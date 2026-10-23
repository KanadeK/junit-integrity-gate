package gate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const healthyXML = `<testsuite name="unit" tests="1" failures="0" errors="0" skipped="0"><testcase classname="demo.C" name="works"/></testsuite>`

func writeFixture(t *testing.T, files map[string]string, manifest string) string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "expected.json")
	if err := os.WriteFile(path, []byte(manifest), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func singleReport(t *testing.T, xml string, opts Options) Report {
	t.Helper()
	return Check(writeFixture(t, map[string]string{"report.xml": xml}, `{"version":1,"files":["report.xml"]}`), opts)
}

func hasCode(report Report, code string) bool {
	for _, d := range report.Diagnostics {
		if d.Code == code {
			return true
		}
	}
	return false
}

func requireCode(t *testing.T, report Report, code string, exit int) {
	t.Helper()
	if report.OK || report.ExitCode != exit || !hasCode(report, code) {
		t.Fatalf("want code=%s exit=%d; got %+v", code, exit, report)
	}
}

func TestCountsAndNestedDeclarations(t *testing.T) {
	tests := []struct {
		name, xml string
		want      Counts
		code      string
	}{
		{"single", healthyXML, Counts{Cases: 1, Passed: 1, NonSkipped: 1}, ""},
		{"wrapper", `<testsuites tests="2"><testsuite tests="1"><testcase name="a"/></testsuite><testsuite tests="1"><testcase name="b"/></testsuite></testsuites>`, Counts{Cases: 2, Passed: 2, NonSkipped: 2}, ""},
		{"mixed nested", `<testsuite tests="3"><testcase name="own"/><testsuite tests="2"><testcase name="child1"/><testcase name="child2"/></testsuite></testsuite>`, Counts{Cases: 3, Passed: 3, NonSkipped: 3}, ""},
		{"inclusive contradiction", `<testsuite tests="1"><testcase name="own"/><testsuite tests="1"><testcase name="child"/></testsuite></testsuite>`, Counts{Cases: 2, Passed: 2, NonSkipped: 2}, "count_mismatch_tests"},
		{"declared zero cannot hide failure", `<testsuite tests="1" failures="0"><testcase name="bad"><failure>secret</failure></testcase></testsuite>`, Counts{Cases: 1, Failures: 1, NonSkipped: 1}, "test_failure"},
		{"failure nodes count case", `<testsuite tests="1" failures="1"><testcase name="bad"><failure/><failure/></testcase></testsuite>`, Counts{Cases: 1, Failures: 1, NonSkipped: 1}, "test_failure"},
		{"failure and error overlap", `<testsuite tests="1" failures="1" errors="1"><testcase name="bad"><failure/><error/></testcase></testsuite>`, Counts{Cases: 1, Failures: 1, Errors: 1, NonSkipped: 1}, "test_error"},
		{"skip plus problem", `<testsuite tests="1"><testcase name="bad"><skipped/><error/></testcase></testsuite>`, Counts{Cases: 1, Errors: 1, NonSkipped: 1}, "conflicting_outcomes"},
		{"partial skips", `<testsuite tests="2" skipped="1"><testcase name="a"/><testcase name="b"><skipped/></testcase></testsuite>`, Counts{Cases: 2, Passed: 1, Skipped: 1, NonSkipped: 1}, ""},
		{"all skipped", `<testsuite tests="1"><testcase name="b"><skipped/></testcase></testsuite>`, Counts{Cases: 1, Skipped: 1}, "all_skipped"},
		{"empty suite", `<testsuite tests="0"/>`, Counts{}, "zero_cases"},
		{"undeclared outcome counters", `<testsuite tests="1"><testcase name="a"/></testsuite>`, Counts{Cases: 1, Passed: 1, NonSkipped: 1}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := singleReport(t, tt.xml, Options{})
			if r.Totals != tt.want {
				t.Fatalf("totals=%+v, want %+v", r.Totals, tt.want)
			}
			if r.FilesExpected != 1 || r.FilesChecked != 1 {
				t.Fatalf("file accounting: %+v", r)
			}
			if tt.code != "" {
				requireCode(t, r, tt.code, 1)
			} else if !r.OK || r.ExitCode != 0 || len(r.Diagnostics) != 0 {
				t.Fatalf("unexpected failure: %+v", r)
			}
		})
	}
}

func TestCounterValidationAndObservedIndependence(t *testing.T) {
	for _, raw := range []string{"-1", "+1", "1.0", "1e0", " 1", "1 ", "١", "", "9223372036854775808", strings.Repeat("0", 20)} {
		t.Run(fmt.Sprintf("invalid_%q", raw), func(t *testing.T) {
			x := `<testsuite tests="` + raw + `"><testcase name="a"/></testsuite>`
			r := singleReport(t, x, Options{})
			requireCode(t, r, "count_invalid_tests", 1)
			if r.Totals.Cases != 1 || r.Totals.Passed != 1 {
				t.Fatalf("declaration changed observations: %+v", r)
			}
		})
	}
	for _, counter := range []string{"tests", "failures", "errors", "skipped"} {
		t.Run(counter, func(t *testing.T) {
			attrs := ` tests="1"`
			if counter == "tests" {
				attrs = ` tests="999"`
			} else {
				attrs += ` ` + counter + `="999"`
			}
			r := singleReport(t, `<testsuite`+attrs+`><testcase name="a"/></testsuite>`, Options{})
			requireCode(t, r, "count_mismatch_"+counter, 1)
			if r.Totals != (Counts{Cases: 1, Passed: 1, NonSkipped: 1}) {
				t.Fatalf("counter declaration changed facts: %+v", r)
			}
			found := false
			for _, d := range r.Diagnostics {
				if d.Code == "count_mismatch_"+counter {
					found = d.Actual != nil && d.Expected != nil && *d.Expected == 999
				}
			}
			if !found {
				t.Fatal("missing numeric contradiction evidence")
			}
		})
	}
	requireCode(t, singleReport(t, `<testsuite><testcase name="a"/></testsuite>`, Options{}), "count_missing_tests", 1)
}

func TestUnsupportedStructuresAndDirectives(t *testing.T) {
	tests := []struct{ name, xml, code string }{
		{"empty", "", "xml_invalid"},
		{"malformed", `<testsuite tests="1"><testcase name="a"></testsuite>`, "xml_invalid"},
		{"two roots", healthyXML + healthyXML, "xml_structure"},
		{"outside text", healthyXML + "hidden", "xml_structure"},
		{"wrong root", `<testcase name="a"/>`, "xml_structure"},
		{"nested testcase", `<testsuite tests="1"><testcase name="a"><testcase name="hidden"/></testcase></testsuite>`, "xml_structure"},
		{"nested wrapper", `<testsuites><testsuites/></testsuites>`, "xml_structure"},
		{"unknown result", `<testsuite tests="1"><testcase name="a"><not-run/></testcase></testsuite>`, "xml_result_unknown"},
		{"unknown status", `<testsuite tests="1"><testcase name="a" status="notrun"/></testsuite>`, "xml_result_unknown"},
		{"duplicate attr", `<testsuite tests="1" tests="0"/>`, "xml_duplicate_attribute"},
		{"duplicate expanded attr", `<testsuite xmlns:a="same" xmlns:b="same" a:x="1" b:x="2" tests="1"/>`, "xml_duplicate_attribute"},
		{"namespace", `<testsuite xmlns="urn:foreign" tests="1"><testcase name="a"/></testsuite>`, "xml_namespace"},
		{"undeclared prefix", `<p:testsuite tests="1"/>`, "xml_namespace"},
		{"empty prefix binding", `<p:testsuite xmlns:p="" tests="1"><p:testcase name="a"/></p:testsuite>`, "xml_namespace"},
		{"empty name", `<testsuite tests="1"><testcase name=" "/></testsuite>`, "xml_name_invalid"},
		{"DTD", `<!DOCTYPE testsuite><testsuite tests="1"><testcase name="a"/></testsuite>`, "xml_directive"},
		{"internal entity", `<!DOCTYPE testsuite [<!ENTITY secret "payload">]><testsuite tests="1"><testcase name="a">&secret;</testcase></testsuite>`, "xml_directive"},
		{"external entity", `<!DOCTYPE testsuite SYSTEM "file:///definitely-not-opened"><testsuite tests="0"/>`, "xml_directive"},
		{"undefined entity", `<testsuite tests="1"><testcase name="&secret;"/></testsuite>`, "xml_invalid"},
		{"non UTF8", `<?xml version="1.0" encoding="ISO-8859-1"?><testsuite tests="0"/>`, "xml_encoding"},
		{"PI", `<?run command?><testsuite tests="0"/>`, "xml_structure"},
		{"malformed declaration", `<?xml version="1.1"?><testsuite tests="0"/>`, "xml_invalid"},
		{"namespaced counter", `<testsuite xmlns:p="foreign" tests="1" p:tests="9"><testcase name="a"/></testsuite>`, "xml_namespace"},
		{"namespaced name", `<testsuite xmlns:p="foreign" tests="1"><testcase name="a" p:name="hidden"/></testsuite>`, "xml_namespace"},
		{"suite status", `<testsuite tests="1" status="notrun"><testcase name="a"/></testsuite>`, "xml_result_unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireCode(t, singleReport(t, tt.xml, Options{}), tt.code, 1)
		})
	}
	for _, tag := range []string{"flakyFailure", "flakyError", "rerunFailure", "rerunError"} {
		t.Run(tag, func(t *testing.T) {
			requireCode(t, singleReport(t, `<testsuite tests="1"><testcase name="a"><`+tag+`/></testcase></testsuite>`, Options{}), "xml_retry_unsupported", 1)
		})
	}
}

func TestOpaqueContentsAndDeclarations(t *testing.T) {
	x := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><testsuites tests="1"><testsuite tests="1"><properties><testcase name="hidden"/></properties><system-out><testsuite tests="987"><testcase name="hidden"/></testsuite></system-out><testcase name="a"><system-err><![CDATA[<error/><testcase/>]]></system-err><properties><p:foreign xmlns:p="foreign"><failure/></p:foreign></properties></testcase></testsuite></testsuites>`
	r := singleReport(t, x, Options{})
	if !r.OK || r.Totals != (Counts{Cases: 1, Passed: 1, NonSkipped: 1}) {
		t.Fatalf("opaque content became results: %+v", r)
	}
	for _, prefix := range []string{"", "\xef\xbb\xbf"} {
		r = singleReport(t, prefix+`<?xml version='1.0' encoding='utf-8'?>`+healthyXML, Options{})
		if !r.OK {
			t.Fatalf("normal declaration/BOM rejected: %+v", r)
		}
	}
}

func TestManifestSchemaAndPaths(t *testing.T) {
	invalid := []struct{ body, code string }{
		{`{`, "manifest_invalid_json"},
		{`[]`, "manifest_schema"},
		{`{"version":1,"version":1,"files":["report.xml"]}`, "manifest_schema"},
		{`{"version":true,"files":["report.xml"]}`, "manifest_version"},
		{`{"version":1.0,"files":["report.xml"]}`, "manifest_version"},
		{`{"version":2,"files":["report.xml"]}`, "manifest_version"},
		{`{"version":1,"files":null}`, "manifest_schema"},
		{`{"version":1,"files":[42]}`, "manifest_schema"},
		{`{"version":1,"files":[]}`, "manifest_empty"},
		{`{"version":1,"files":["report.xml"],"command":"private-secret"}`, "manifest_schema"},
		{`{"version":1}`, "manifest_schema"},
		{`{"files":["report.xml"]}`, "manifest_schema"},
		{`{"version":1,"files":["report.xml"]} {}`, "manifest_invalid_json"},
		{`{"version":1,"files":["report.xml","report.xml"]}`, "manifest_duplicate_path"},
	}
	for i, tt := range invalid {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			r := Check(writeFixture(t, map[string]string{"report.xml": healthyXML}, tt.body), Options{})
			requireCode(t, r, tt.code, 2)
		})
	}
	for _, path := range []string{"", "../outside.xml", "/absolute.xml", "C:/outside.xml", `a\b.xml`, "a//b.xml", "./a.xml", "a/../b.xml", "a/", "a.xml.", "a.xml ", "CON", "NUL.xml", "a/*.xml", "a/?.xml", "bad\x00.xml"} {
		t.Run(fmt.Sprintf("path_%q", path), func(t *testing.T) {
			data, err := json.Marshal(map[string]any{"version": 1, "files": []string{path}})
			if err != nil {
				t.Fatal(err)
			}
			r := Check(writeFixture(t, nil, string(data)), Options{})
			requireCode(t, r, "manifest_path_invalid", 2)
		})
	}
	r := Check(writeFixture(t, map[string]string{"report.xml": healthyXML}, `{"version":1,"files":["report.xml","missing.xml"]}`), Options{})
	requireCode(t, r, "report_missing", 1)
	if r.FilesExpected != 2 || r.FilesChecked != 1 || r.Totals.Cases != 1 {
		t.Fatalf("expected set was silently reduced: %+v", r)
	}
}

func TestDuplicatePolicyAndPhysicalAliases(t *testing.T) {
	manifest := writeFixture(t, map[string]string{"one.xml": healthyXML, "two.xml": healthyXML}, `{"version":1,"files":["one.xml","two.xml"]}`)
	requireCode(t, Check(manifest, Options{}), "duplicate_case", 1)
	r := Check(manifest, Options{DuplicateScope: "file"})
	if !r.OK || r.Totals.Cases != 2 || r.DuplicateScope != "file" {
		t.Fatalf("explicit platform scope rejected: %+v", r)
	}
	duplicate := `<testsuite tests="2"><testcase name="same"/><testcase name="same"/></testsuite>`
	requireCode(t, singleReport(t, duplicate, Options{DuplicateScope: "file"}), "duplicate_case", 1)
	parameterized := `<testsuite tests="2"><testcase name="same[a]"/><testcase name="same[b]"/></testsuite>`
	if r := singleReport(t, parameterized, Options{}); !r.OK || r.Totals.Cases != 2 {
		t.Fatalf("distinct parameters conflated: %+v", r)
	}
	dir := filepath.Dir(manifest)
	if err := os.Remove(filepath.Join(dir, "two.xml")); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(filepath.Join(dir, "one.xml"), filepath.Join(dir, "two.xml")); err != nil {
		t.Skipf("hard links unavailable: %v", err)
	}
	requireCode(t, Check(manifest, Options{DuplicateScope: "file"}), "report_alias", 1)
}

func TestPrivacyAndInvalidOptions(t *testing.T) {
	secret := "PRIVATE_SENTINEL_DO_NOT_EMIT"
	x := `<testsuite name="` + secret + `" tests="1" hostname="` + secret + `"><testcase classname="` + secret + `" name="` + secret + `"><failure message="` + secret + `">` + secret + `</failure></testcase></testsuite>`
	r := singleReport(t, x, Options{})
	data, err := json.Marshal(r)
	if err != nil || strings.Contains(string(data), secret) {
		t.Fatalf("public diagnostics exposed input: %s, %v", data, err)
	}
	for _, opts := range []Options{{DuplicateScope: secret}, {Limits: Limits{MaxCases: -1}}} {
		r := Check("unused-private-path", opts)
		requireCode(t, r, "invalid_options", 2)
		data, _ := json.Marshal(r)
		if strings.Contains(string(data), secret) || strings.Contains(string(data), "unused-private-path") {
			t.Fatal("invalid option/path was echoed")
		}
	}
	r = singleReport(t, `<testsuite tests="2"><testcase name="a"><failure/><error/><skipped/></testcase><testcase name="b"><failure/><error/></testcase></testsuite>`, Options{Limits: Limits{MaxDiagnostics: 2}})
	if !r.DiagnosticsTruncated || len(r.Diagnostics) != 2 || r.ExitCode != 1 {
		t.Fatalf("diagnostic limit lost failure: %+v", r)
	}
}
