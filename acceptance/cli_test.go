// Package acceptance tests the installed command boundary with original fixtures.
package acceptance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

var commandBinary string

const private = "PRIVATE_ACCEPTANCE_SENTINEL_7e91"

type counters struct {
	Cases      int64 `json:"cases"`
	Passed     int64 `json:"passed"`
	Failures   int64 `json:"failures"`
	Errors     int64 `json:"errors"`
	Skipped    int64 `json:"skipped"`
	NonSkipped int64 `json:"non_skipped"`
}

type result struct {
	Version        int              `json:"version"`
	OK             bool             `json:"ok"`
	ExitCode       int              `json:"exit_code"`
	DuplicateScope string           `json:"duplicate_scope"`
	FilesExpected  int              `json:"files_expected"`
	FilesChecked   int              `json:"files_checked"`
	Totals         counters         `json:"totals"`
	Diagnostics    []map[string]any `json:"diagnostics"`
}

func TestMain(m *testing.M) {
	directory, err := os.MkdirTemp("", "junit-gate-acceptance-build-")
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot prepare acceptance build")
		os.Exit(1)
	}
	commandBinary = filepath.Join(directory, "junit-integrity-gate")
	if runtime.GOOS == "windows" {
		commandBinary += ".exe"
	}
	build := exec.Command("go", "build", "-o", commandBinary, "./cmd/junit-integrity-gate")
	build.Dir = ".."
	if output, err := build.CombinedOutput(); err != nil {
		fmt.Fprintln(os.Stderr, "acceptance CLI build failed:", err)
		fmt.Fprintln(os.Stderr, string(output))
		_ = os.RemoveAll(directory) // This exact directory was exclusively created above.
		os.Exit(1)
	}
	status := m.Run()
	_ = os.RemoveAll(directory)
	os.Exit(status)
}

func write(t *testing.T, root, name, body string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func manifest(t *testing.T, root string, names ...string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"version": 1, "files": names})
	if err != nil {
		t.Fatal(err)
	}
	return write(t, root, "expected.json", string(raw))
}

func invoke(t *testing.T, directory string, arguments ...string) (int, []byte, []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, commandBinary, arguments...)
	command.Dir = directory
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if ctx.Err() != nil {
		t.Fatal("gate exceeded independent process deadline")
	}
	status := 0
	if err != nil {
		if exited, ok := err.(*exec.ExitError); ok {
			status = exited.ExitCode()
		} else {
			t.Fatal(err)
		}
	}
	encodedDirectory, _ := json.Marshal(directory)
	for _, forbidden := range []string{private, directory, string(encodedDirectory[1 : len(encodedDirectory)-1])} {
		if bytes.Contains(stdout.Bytes(), []byte(forbidden)) || bytes.Contains(stderr.Bytes(), []byte(forbidden)) {
			t.Fatal("gate disclosed private fixture data or its filesystem path")
		}
	}
	return status, stdout.Bytes(), stderr.Bytes()
}

func check(t *testing.T, root, path string, expectedExit int, flags ...string) result {
	t.Helper()
	arguments := append([]string{"check", "--json"}, flags...)
	arguments = append(arguments, path)
	status, stdout, stderr := invoke(t, root, arguments...)
	if status != expectedExit {
		t.Fatalf("exit=%d, want=%d; stdout=%s stderr=%s", status, expectedExit, stdout, stderr)
	}
	var report result
	if err := json.Unmarshal(stdout, &report); err != nil {
		t.Fatalf("check did not return JSON: %v", err)
	}
	if report.Version != 1 || report.ExitCode != status || report.OK != (status == 0) {
		t.Fatalf("inconsistent report envelope: %+v", report)
	}
	if status != 0 && len(report.Diagnostics) == 0 {
		t.Fatal("failed check has no diagnostic reason")
	}
	for _, diagnostic := range report.Diagnostics {
		for field, value := range diagnostic {
			if field == "code" {
				code, ok := value.(string)
				if !ok || code == "" {
					t.Fatal("diagnostic code is not a nonempty string")
				}
			} else if value != nil {
				if _, ok := value.(float64); !ok {
					t.Fatalf("diagnostic field %q is not a numeric ordinal/count", field)
				}
			}
		}
	}
	return report
}

func expectTotals(t *testing.T, report result, expected counters) {
	t.Helper()
	if report.Totals != expected {
		t.Fatalf("observed totals=%+v, want=%+v", report.Totals, expected)
	}
}

func hasCode(report result, code string) bool {
	for _, diagnostic := range report.Diagnostics {
		if diagnostic["code"] == code {
			return true
		}
	}
	return false
}

func TestAcceptanceHealthyNestedRollupAndSourcePreservation(t *testing.T) {
	root := t.TempDir()
	xml := `<testsuites tests="3" failures="0" errors="0" skipped="1"><testsuite name="` + private + `" tests="3" failures="0" errors="0" skipped="1"><testcase classname="` + private + `" name="direct"/><testsuite tests="2" skipped="1"><testcase name="parameter[α]"/><testcase name="ignored"><skipped message="` + private + `">` + private + `</skipped></testcase></testsuite></testsuite></testsuites>`
	path := write(t, root, "reports/nested.xml", xml)
	before := sha256.Sum256([]byte(xml))
	report := check(t, root, manifest(t, root, "reports/nested.xml"), 0)
	expectTotals(t, report, counters{Cases: 3, Passed: 2, Skipped: 1, NonSkipped: 2})
	if report.DuplicateScope != "global" || report.FilesExpected != 1 || report.FilesChecked != 1 {
		t.Fatalf("unexpected file/scope summary: %+v", report)
	}
	after, err := os.ReadFile(path)
	if err != nil || sha256.Sum256(after) != before {
		t.Fatal("read-only gate changed source bytes")
	}
}

func TestAcceptanceOptionalCountsAndExactParameterizedIdentity(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.xml", `<testsuite tests="3"><testcase name="case[1]"/><testcase name="case[2]"/><testcase name=" case[1] "/></testsuite>`)
	expectTotals(t, check(t, root, manifest(t, root, "a.xml"), 0), counters{Cases: 3, Passed: 3, NonSkipped: 3})
}

func TestAcceptanceOutcomesCountCasesRatherThanMarkerElements(t *testing.T) {
	for _, fixture := range []struct {
		name, body string
		totals     counters
	}{
		{"repeated-failure", `<testsuite tests="1" failures="1"><testcase name="x"><failure/><failure/></testcase></testsuite>`, counters{Cases: 1, Failures: 1, NonSkipped: 1}},
		{"failure-and-error", `<testsuite tests="1" failures="1" errors="1"><testcase name="x"><failure/><error/></testcase></testsuite>`, counters{Cases: 1, Failures: 1, Errors: 1, NonSkipped: 1}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			root := t.TempDir()
			write(t, root, "a.xml", fixture.body)
			expectTotals(t, check(t, root, manifest(t, root, "a.xml"), 1), fixture.totals)
		})
	}
}

func TestAcceptanceSkipCannotHideAnError(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.xml", `<testsuite tests="1"><testcase name="x"><skipped/><error message="`+private+`"/></testcase></testsuite>`)
	report := check(t, root, manifest(t, root, "a.xml"), 1)
	if !hasCode(report, "conflicting_outcomes") {
		t.Fatal("skip/error conflict was not reported")
	}
}

func TestAcceptanceOpaqueSubtreesCannotInventCases(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a.xml", `<testsuite tests="1"><properties><testcase name="fake"/><anything>`+private+`</anything></properties><system-out><testcase name="fake"/></system-out><system-err><![CDATA[<testcase name="fake"/>]]></system-err><testcase classname="`+private+`" name="real"><system-out><testcase name="fake"/></system-out></testcase></testsuite>`)
	expectTotals(t, check(t, root, manifest(t, root, "a.xml"), 0), counters{Cases: 1, Passed: 1, NonSkipped: 1})
}

func TestAcceptanceCountDeclarationsNeverOverrideObservedCases(t *testing.T) {
	for _, declaration := range []string{`tests="0"`, `tests="2"`, `tests="1" failures="1"`, `tests="1" errors="1"`, `tests="1" skipped="1"`} {
		t.Run(declaration, func(t *testing.T) {
			root := t.TempDir()
			write(t, root, "a.xml", `<testsuite `+declaration+`><testcase name="real"/></testsuite>`)
			expectTotals(t, check(t, root, manifest(t, root, "a.xml"), 1), counters{Cases: 1, Passed: 1, NonSkipped: 1})
		})
	}
}

func TestAcceptanceEachExpectedFileMustHaveNonSkippedCases(t *testing.T) {
	for _, bad := range []string{"", `<testsuite tests="0"/>`, `<testsuite tests="1" skipped="1"><testcase name="ignored"><skipped/></testcase></testsuite>`} {
		t.Run(fmt.Sprintf("bytes-%d", len(bad)), func(t *testing.T) {
			root := t.TempDir()
			write(t, root, "good.xml", `<testsuite tests="1"><testcase name="good"/></testsuite>`)
			write(t, root, "bad.xml", bad)
			check(t, root, manifest(t, root, "good.xml", "bad.xml"), 1)
		})
	}
}

func TestAcceptanceMissingShardFailsDespiteHealthyOtherShard(t *testing.T) {
	root := t.TempDir()
	write(t, root, "good.xml", `<testsuite tests="1"><testcase name="good"/></testsuite>`)
	report := check(t, root, manifest(t, root, "good.xml", "missing.xml"), 1)
	if report.FilesExpected != 2 {
		t.Fatal("missing expected shard was removed from the denominator")
	}
}

func TestAcceptanceDuplicateScopeIsExplicitAndNarrow(t *testing.T) {
	root := t.TempDir()
	xml := `<testsuite tests="1"><testcase classname="suite" name="same"/></testsuite>`
	write(t, root, "a.xml", xml)
	write(t, root, "b.xml", xml)
	path := manifest(t, root, "a.xml", "b.xml")
	check(t, root, path, 1)
	report := check(t, root, path, 0, "--duplicate-scope", "file")
	if report.DuplicateScope != "file" {
		t.Fatal("identity escape hatch missing from machine output")
	}
	write(t, root, "a.xml", `<testsuite tests="2"><testcase name="same"/><testcase name="same"/></testsuite>`)
	check(t, root, manifest(t, root, "a.xml"), 1, "--duplicate-scope", "file")
}

func TestAcceptancePhysicalAliasesNeverPassInFileScope(t *testing.T) {
	root := t.TempDir()
	source := write(t, root, "a.xml", `<testsuite tests="1"><testcase name="same"/></testsuite>`)
	if err := os.Link(source, filepath.Join(root, "b.xml")); err != nil {
		t.Skip("filesystem cannot create hard-link alias")
	}
	status, _, _ := invoke(t, root, "check", "--json", "--duplicate-scope", "file", manifest(t, root, "a.xml", "b.xml"))
	if status == 0 {
		t.Fatal("two physical aliases bypassed duplicate protection")
	}
}

func TestAcceptanceUnsupportedStructureFailsClosed(t *testing.T) {
	cases := map[string]string{
		"nested-case":         `<testsuite tests="1"><testcase name="a"><testcase name="b"/></testcase></testsuite>`,
		"missing-name":        `<testsuite tests="1"><testcase/></testsuite>`,
		"blank-name":          `<testsuite tests="1"><testcase name=" "/></testsuite>`,
		"unknown-result":      `<testsuite tests="1"><testcase name="a"><unmodeledResult/></testcase></testsuite>`,
		"namespaced":          `<testsuite xmlns="urn:foreign" tests="1"><testcase name="a"/></testsuite>`,
		"undeclared-prefix":   `<p:testsuite tests="1"><p:testcase name="a"/></p:testsuite>`,
		"nested-wrapper":      `<testsuites><testsuites><testsuite tests="1"><testcase name="a"/></testsuite></testsuites></testsuites>`,
		"multiple-roots":      `<testsuite tests="1"><testcase name="a"/></testsuite><testsuite tests="0"/>`,
		"external-text":       `PRIVATE_TEXT<testsuite tests="1"><testcase name="a"/></testsuite>`,
		"duplicate-attribute": `<testsuite tests="1" tests="1"><testcase name="a"/></testsuite>`,
		"missing-tests":       `<testsuite><testcase name="a"/></testsuite>`,
		"truncated":           `<testsuite tests="1"><testcase name="a"/>`,
	}
	for name, xml := range cases {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			write(t, root, "a.xml", xml)
			check(t, root, manifest(t, root, "a.xml"), 1)
		})
	}
}

func TestAcceptanceRetriesAreNotSilentlyTreatedAsPasses(t *testing.T) {
	for _, tag := range []string{"flakyFailure", "flakyError", "rerunFailure", "rerunError"} {
		t.Run(tag, func(t *testing.T) {
			root := t.TempDir()
			write(t, root, "a.xml", `<testsuite tests="1"><testcase name="a"><`+tag+`>`+private+`</`+tag+`></testcase></testsuite>`)
			report := check(t, root, manifest(t, root, "a.xml"), 1)
			if !hasCode(report, "xml_retry_unsupported") {
				t.Fatal("unsupported retry was not identified")
			}
		})
	}
}

func TestAcceptanceCounterSyntaxIsStrictASCIIUnsignedDecimal(t *testing.T) {
	for _, count := range []string{"-1", "+1", "1.0", " 1", "1 ", "١", strings.Repeat("9", 100)} {
		t.Run(fmt.Sprintf("counter-%x", []byte(count)), func(t *testing.T) {
			root := t.TempDir()
			write(t, root, "a.xml", `<testsuite tests="`+count+`"><testcase name="a"/></testsuite>`)
			check(t, root, manifest(t, root, "a.xml"), 1)
		})
	}
}

func TestAcceptanceDirectivesAndForeignEncodingsAreRejected(t *testing.T) {
	for name, xml := range map[string]string{
		"doctype":          `<!DOCTYPE testsuite><testsuite tests="1"><testcase name="a"/></testsuite>`,
		"external-entity":  `<!DOCTYPE testsuite [<!ENTITY p SYSTEM "file:///PRIVATE_DO_NOT_READ">]><testsuite tests="1"><testcase name="a"><system-out>&p;</system-out></testcase></testsuite>`,
		"foreign-encoding": `<?xml version="1.0" encoding="ISO-8859-1"?><testsuite tests="1"><testcase name="a"/></testsuite>`,
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			write(t, root, "a.xml", xml)
			check(t, root, manifest(t, root, "a.xml"), 1)
		})
	}
}

func TestAcceptanceIgnoredContentStillConsumesResourceBudgets(t *testing.T) {
	attributes := `tests="1"`
	for index := 0; index < 64; index++ {
		attributes += fmt.Sprintf(` metadata%d="value"`, index)
	}
	fixtures := []struct {
		name, xml, code string
	}{
		{"opaque-depth", `<testsuite tests="1"><system-out>` + strings.Repeat(`<x>`, 64) + strings.Repeat(`</x>`, 64) + `</system-out><testcase name="a"/></testsuite>`, "xml_limit_depth"},
		{"attribute-budget", `<testsuite ` + attributes + `><testcase name="a"/></testsuite>`, "xml_limit_attributes"},
		{"identity-budget", `<testsuite tests="1"><testcase name="` + strings.Repeat("x", 8193) + `"/></testsuite>`, "xml_limit_identity"},
		{"file-byte-budget", `<testsuite tests="1"><system-out>` + strings.Repeat("x", 16*1024*1024) + `</system-out><testcase name="a"/></testsuite>`, "xml_limit_bytes"},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			root := t.TempDir()
			write(t, root, "a.xml", fixture.xml)
			report := check(t, root, manifest(t, root, "a.xml"), 1)
			if !hasCode(report, fixture.code) {
				t.Fatalf("required budget diagnostic %q missing", fixture.code)
			}
		})
	}
}

func TestAcceptanceManifestConfigurationIsFailClosed(t *testing.T) {
	documents := []string{
		`{"version":1,"version":1,"files":["a.xml"]}`,
		`{"version":1,"files":["a.xml"],"extra":false}`,
		`{"version":true,"files":["a.xml"]}`,
		`{"version":1.0,"files":["a.xml"]}`,
		`{"version":1,"files":[]}`,
		`{"version":1,"files":null}`,
		`{"version":1,"files":[true]}`,
		`{"version":1,"files":["a.xml","a.xml"]}`,
		`{"version":1,"files":["../outside.xml"]}`,
		`{"version":1,"files":["./a.xml"]}`,
		`{"version":1,"files":["/a.xml"]}`,
		`{"version":1,"files":["C:/a.xml"]}`,
		`{"version":1,"files":["reports\\a.xml"]}`,
		`{"version":1,"files":[""]}`,
	}
	for index, document := range documents {
		t.Run(fmt.Sprintf("case-%d", index), func(t *testing.T) {
			root := t.TempDir()
			write(t, root, "a.xml", `<testsuite tests="1"><testcase name="a"/></testsuite>`)
			path := write(t, root, "invalid.json", document)
			status, _, _ := invoke(t, root, "check", "--json", path)
			if status != 2 {
				t.Fatalf("invalid manifest exit=%d, want=2", status)
			}
		})
	}
}

func TestAcceptanceHelpVersionAndInvalidCLI(t *testing.T) {
	root := t.TempDir()
	for _, option := range []string{"--help", "--version"} {
		status, stdout, _ := invoke(t, root, option)
		if status != 0 || len(bytes.TrimSpace(stdout)) == 0 {
			t.Fatalf("%s unavailable", option)
		}
	}
	for _, args := range [][]string{{}, {"unknown"}, {"check"}, {"check", "--duplicate-scope", "anything", "not-present.json"}} {
		status, _, _ := invoke(t, root, args...)
		if status != 2 {
			t.Fatalf("invalid CLI exit=%d, want=2", status)
		}
	}
}
