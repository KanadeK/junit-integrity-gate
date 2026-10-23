// Package gate checks an explicit set of existing JUnit-style XML reports.
// It neither runs tests nor establishes report freshness or provenance.
package gate

import (
	"os"
)

// Limits bound input and retained state. Zero fields use their defaults.
type Limits struct {
	MaxFiles         int
	MaxFileBytes     int64
	MaxTotalBytes    int64
	MaxElements      int
	MaxDepth         int
	MaxCases         int
	MaxAttributes    int
	MaxIdentityBytes int
	MaxManifestBytes int64
	MaxDiagnostics   int
}

func DefaultLimits() Limits {
	return Limits{256, 16 << 20, 64 << 20, 500000, 64, 100000, 64, 8192, 1 << 20, 100}
}

type Options struct {
	DuplicateScope string
	Limits         Limits
}

type Counts struct {
	Cases      int `json:"cases"`
	Passed     int `json:"passed"`
	Failures   int `json:"failures"`
	Errors     int `json:"errors"`
	Skipped    int `json:"skipped"`
	NonSkipped int `json:"non_skipped"`
}

func (c *Counts) add(other Counts) {
	c.Cases += other.Cases
	c.Passed += other.Passed
	c.Failures += other.Failures
	c.Errors += other.Errors
	c.Skipped += other.Skipped
	c.NonSkipped += other.NonSkipped
}

// Diagnostic deliberately has no string fields except a fixed public code.
type Diagnostic struct {
	Code     string `json:"code"`
	File     int    `json:"file,omitempty"`
	Suite    int    `json:"suite,omitempty"`
	Case     int    `json:"case,omitempty"`
	Actual   *int64 `json:"actual,omitempty"`
	Expected *int64 `json:"expected,omitempty"`
}

type Report struct {
	Version              int          `json:"version"`
	OK                   bool         `json:"ok"`
	ExitCode             int          `json:"exit_code"`
	DuplicateScope       string       `json:"duplicate_scope"`
	FilesExpected        int          `json:"files_expected"`
	FilesChecked         int          `json:"files_checked"`
	Totals               Counts       `json:"totals"`
	Diagnostics          []Diagnostic `json:"diagnostics"`
	DiagnosticsTruncated bool         `json:"diagnostics_truncated,omitempty"`
}

type run struct {
	report     Report
	limits     Limits
	identities map[identity]struct{}
	elements   int
	cases      int
	bytes      int64
}

func (r *run) diagnostic(code string, file, suite, test int) {
	r.addDiagnostic(Diagnostic{Code: code, File: file, Suite: suite, Case: test}, 1)
}

func (r *run) addDiagnostic(d Diagnostic, exit int) {
	if r.report.ExitCode < exit {
		r.report.ExitCode = exit
	}
	if len(r.report.Diagnostics) < r.limits.MaxDiagnostics {
		r.report.Diagnostics = append(r.report.Diagnostics, d)
	} else {
		r.report.DiagnosticsTruncated = true
	}
}

func (r *run) invalid(code string, file int) {
	r.addDiagnostic(Diagnostic{Code: code, File: file}, 2)
}

func normalizedLimits(l Limits) (Limits, bool) {
	d := DefaultLimits()
	ints := []*int{&l.MaxFiles, &l.MaxElements, &l.MaxDepth, &l.MaxCases, &l.MaxAttributes, &l.MaxIdentityBytes, &l.MaxDiagnostics}
	defaults := []int{d.MaxFiles, d.MaxElements, d.MaxDepth, d.MaxCases, d.MaxAttributes, d.MaxIdentityBytes, d.MaxDiagnostics}
	for i, n := range ints {
		if *n < 0 {
			return d, false
		}
		if *n == 0 {
			*n = defaults[i]
		}
	}
	longs := []*int64{&l.MaxFileBytes, &l.MaxTotalBytes, &l.MaxManifestBytes}
	longDefaults := []int64{d.MaxFileBytes, d.MaxTotalBytes, d.MaxManifestBytes}
	for i, n := range longs {
		if *n < 0 {
			return d, false
		}
		if *n == 0 {
			*n = longDefaults[i]
		}
	}
	return l, true
}

// Check examines only the files named in a version-1 JSON manifest.
// FilesChecked counts safe regular files whose XML parsing was attempted.
// On failure, totals may represent only the cases fully observed before an error.
func Check(manifestPath string, opts Options) Report {
	limits, valid := normalizedLimits(opts.Limits)
	scope := opts.DuplicateScope
	if scope == "" {
		scope = "global"
	}
	r := &run{report: Report{Version: 1, DuplicateScope: scope, Diagnostics: []Diagnostic{}}, limits: limits, identities: make(map[identity]struct{})}
	if !valid || (scope != "global" && scope != "file") {
		// Do not echo an invalid caller-supplied option into public JSON.
		if scope != "global" && scope != "file" {
			r.report.DuplicateScope = "global"
		}
		r.invalid("invalid_options", 0)
		return r.report
	}
	manifest, base, guard, code := openManifest(manifestPath)
	if code != "" {
		r.invalid(code, 0)
		return r.report
	}
	defer base.Close()
	files, code := readManifest(manifest, r.limits)
	manifest.Close()
	if code == "" && !guard.unchanged() {
		code = "manifest_changed"
	}
	if code != "" {
		r.invalid(code, 0)
		return r.report
	}
	r.report.FilesExpected = len(files)
	seenPaths := make(map[string]struct{}, len(files))
	for i, name := range files {
		if !validReportPath(name) {
			r.invalid("manifest_path_invalid", i+1)
		}
		if _, exists := seenPaths[name]; exists {
			r.invalid("manifest_duplicate_path", i+1)
		}
		seenPaths[name] = struct{}{}
	}
	if r.report.ExitCode != 0 {
		return r.report
	}
	physical := make([]os.FileInfo, 0, len(files))
	for i, name := range files {
		ordinal := i + 1
		f, info, fileGuard, code := openReport(base, name)
		if code != "" {
			r.diagnostic(code, ordinal, 0, 0)
			continue
		}
		alias := false
		for _, previous := range physical {
			if os.SameFile(previous, info) {
				alias = true
				break
			}
		}
		if alias {
			r.diagnostic("report_alias", ordinal, 0, 0)
			f.Close()
			continue
		}
		physical = append(physical, info)
		if info.Size() > r.limits.MaxFileBytes {
			r.diagnostic("xml_limit_bytes", ordinal, 0, 0)
			f.Close()
			continue
		}
		if scope == "file" {
			r.identities = make(map[identity]struct{})
		}
		r.report.FilesChecked++
		counts := r.parseXML(f, ordinal)
		r.report.Totals.add(counts)
		after, err := f.Stat()
		if err != nil || !sameSnapshot(info, after) || !fileGuard.unchanged() {
			r.diagnostic("report_changed", ordinal, 0, 0)
		}
		f.Close()
		if counts.Cases == 0 {
			r.diagnostic("zero_cases", ordinal, 0, 0)
		} else if counts.NonSkipped == 0 {
			r.diagnostic("all_skipped", ordinal, 0, 0)
		}
	}
	if !guard.unchanged() {
		r.diagnostic("manifest_changed", 0, 0, 0)
	}
	r.report.OK = r.report.ExitCode == 0
	return r.report
}
