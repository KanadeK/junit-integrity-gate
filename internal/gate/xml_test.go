package gate

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

func parserRun(limits Limits) *run {
	resolved, _ := normalizedLimits(limits)
	return &run{report: Report{Version: 1, DuplicateScope: "global", Diagnostics: []Diagnostic{}}, limits: resolved, identities: make(map[identity]struct{})}
}

func parseFixture(input io.Reader, limits Limits) (Counts, Report) {
	r := parserRun(limits)
	c := r.parseXML(input, 1)
	r.report.OK = r.report.ExitCode == 0
	r.report.Totals = c
	return c, r.report
}

type smallChunks struct {
	input io.Reader
	size  int
}

func (r *smallChunks) Read(p []byte) (int, error) {
	if len(p) > r.size {
		p = p[:r.size]
	}
	return r.input.Read(p)
}

type unreadable struct{}

func (unreadable) Read([]byte) (int, error) {
	return 0, errors.New("PRIVATE reader failure must never be serialized")
}

func TestIncrementalUTF8AndReaderErrors(t *testing.T) {
	x := `<?xml version="1.0" encoding="UTF-8"?><testsuite tests="2"><testcase name="参数[一]"/><testcase name="参数[二]"/></testsuite>`
	for _, width := range []int{1, 2, 3, 7} {
		counts, report := parseFixture(&smallChunks{input: strings.NewReader(x), size: width}, Limits{})
		if !report.OK || counts != (Counts{Cases: 2, Passed: 2, NonSkipped: 2}) {
			t.Fatalf("chunk width %d changed observations: %+v", width, report)
		}
	}
	_, report := parseFixture(unreadable{}, Limits{})
	requireCode(t, report, "report_read_error", 1)
	data, _ := json.Marshal(report)
	if bytes.Contains(data, []byte("PRIVATE")) {
		t.Fatal("reader exception escaped into diagnostics")
	}
	_, report = parseFixture(strings.NewReader(`<testsuite tests="1"><`), Limits{})
	requireCode(t, report, "xml_invalid", 1)
}

func TestParserBudgetsAtAndBeyondBoundary(t *testing.T) {
	simple := `<testsuite tests="1"><testcase name="a"/></testsuite>`
	tests := []struct {
		name, xml string
		limits    Limits
		code      string
	}{
		{"file exact", simple, Limits{MaxFileBytes: int64(len(simple))}, ""},
		{"file one too many", simple, Limits{MaxFileBytes: int64(len(simple) - 1)}, "xml_limit_bytes"},
		{"total exact", simple, Limits{MaxTotalBytes: int64(len(simple))}, ""},
		{"total one too many", simple, Limits{MaxTotalBytes: int64(len(simple) - 1)}, "xml_limit_total_bytes"},
		{"elements exact", simple, Limits{MaxElements: 2}, ""},
		{"elements one too many", simple, Limits{MaxElements: 1}, "xml_limit_elements"},
		{"depth exact", simple, Limits{MaxDepth: 2}, ""},
		{"depth one too many", simple, Limits{MaxDepth: 1}, "xml_limit_depth"},
		{"case exact", simple, Limits{MaxCases: 1}, ""},
		{"case one too many", `<testsuite tests="2"><testcase name="a"/><testcase name="b"/></testsuite>`, Limits{MaxCases: 1}, "xml_limit_cases"},
		{"attributes exact", simple, Limits{MaxAttributes: 1}, ""},
		{"attributes one too many", `<testsuite tests="1"><testcase name="a" classname="C"/></testsuite>`, Limits{MaxAttributes: 1}, "xml_limit_attributes"},
		{"identity exact UTF8 bytes", `<testsuite tests="1"><testcase name="é"/></testsuite>`, Limits{MaxIdentityBytes: 2}, ""},
		{"identity one too many UTF8 bytes", `<testsuite tests="1"><testcase name="é"/></testsuite>`, Limits{MaxIdentityBytes: 1}, "xml_limit_identity"},
		{"classname bound", `<testsuite tests="1"><testcase name="a" classname="XX"/></testsuite>`, Limits{MaxIdentityBytes: 1}, "xml_limit_identity"},
		{"opaque subtree elements count", `<testsuite tests="1"><system-out><hidden/></system-out><testcase name="a"/></testsuite>`, Limits{MaxElements: 3}, "xml_limit_elements"},
		{"opaque subtree depth counts", `<testsuite tests="1"><system-out><a><b/></a></system-out><testcase name="a"/></testsuite>`, Limits{MaxDepth: 3}, "xml_limit_depth"},
		{"opaque subtree attributes count", `<testsuite tests="1"><system-out><a x="1" y="2"/></system-out><testcase name="a"/></testsuite>`, Limits{MaxAttributes: 1}, "xml_limit_attributes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, report := parseFixture(strings.NewReader(tt.xml), tt.limits)
			if tt.code != "" {
				requireCode(t, report, tt.code, 1)
			} else if !report.OK || report.Totals.Cases != 1 {
				t.Fatalf("boundary value rejected: %+v", report)
			}
		})
	}
}

func TestGlobalBudgetsDoNotResetBetweenFiles(t *testing.T) {
	first := `<testsuite tests="1"><testcase name="a"/></testsuite>`
	second := `<testsuite tests="1"><testcase name="b"/></testsuite>`
	for _, limits := range []Limits{
		{MaxTotalBytes: int64(len(first) + len(second) - 1)},
		{MaxElements: 3},
		{MaxCases: 1},
	} {
		r := parserRun(limits)
		if c := r.parseXML(strings.NewReader(first), 1); c.Cases != 1 || r.report.ExitCode != 0 {
			t.Fatalf("first input should fit: %+v", r.report)
		}
		r.parseXML(strings.NewReader(second), 2)
		if r.report.ExitCode == 0 {
			t.Fatalf("global limit reset: %+v", limits)
		}
		for _, d := range r.report.Diagnostics {
			if d.File != 2 {
				t.Fatalf("wrong file ordinal: %+v", d)
			}
		}
	}
	r := parserRun(Limits{MaxTotalBytes: 1})
	r.parseXML(strings.NewReader(first), 1)
	r.parseXML(strings.NewReader(second), 2)
	if !hasCode(r.report, "xml_limit_total_bytes") || hasCode(r.report, "report_read_error") {
		t.Fatalf("exhausted global budget misclassified: %+v", r.report)
	}
}

// FuzzXML exercises the real bounded token state machine without filesystem work.
// The small caps keep every fuzz input bounded; assertions protect fail-closed
// handling of directives, explicit retry markup, and diagnostic privacy.
func FuzzXML(f *testing.F) {
	for _, seed := range []string{
		healthyXML,
		`<!DOCTYPE testsuite [<!ENTITY x "large">]><testsuite tests="0"/>`,
		`<testsuite tests="1"><testcase name="x"><flakyFailure/></testcase></testsuite>`,
		`<testsuite tests="1"><testcase name="x"><error/><skipped/></testcase></testsuite>`,
		`<testsuite tests="1"><system-out><testcase name="hidden"/></system-out><testcase name="x"/></testsuite>`,
		`<testsuite tests="1" tests="0"/>`,
		`<testsuite><testcase`,
		"\xff\x00",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 4096 {
			return
		}
		limits := Limits{MaxFileBytes: 4096, MaxTotalBytes: 4096, MaxElements: 128, MaxDepth: 16, MaxCases: 32, MaxAttributes: 8, MaxIdentityBytes: 64, MaxDiagnostics: 16}
		counts, report := parseFixture(bytes.NewReader(data), limits)
		if counts.Cases < 0 || counts.Passed < 0 || counts.Errors < 0 || counts.Failures < 0 || counts.Skipped < 0 || counts.NonSkipped < 0 || counts.Cases > 32 ||
			counts.Passed+counts.Skipped > counts.Cases || counts.Errors > counts.Cases || counts.Failures > counts.Cases || counts.NonSkipped+counts.Skipped != counts.Cases {
			t.Fatalf("impossible observed counts: %+v", counts)
		}
		if bytes.HasPrefix(data, []byte("<!DOCTYPE")) && report.ExitCode == 0 {
			t.Fatal("DOCTYPE accepted")
		}
		if bytes.Equal(data, []byte(`<testsuite tests="1"><testcase name="x"><flakyFailure/></testcase></testsuite>`)) && !hasCode(report, "xml_retry_unsupported") {
			t.Fatal("retry extension became a pass")
		}
		for _, d := range report.Diagnostics {
			if len(d.Code) > 40 || strings.ContainsAny(d.Code, "<>\"' \r\n\t") || d.File != 1 {
				t.Fatalf("non-fixed diagnostic: %+v", d)
			}
		}
	})
}
