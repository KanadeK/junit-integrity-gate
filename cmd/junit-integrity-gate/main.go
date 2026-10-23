// Command junit-integrity-gate checks explicitly expected classic JUnit reports.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/KanadeK/junit-integrity-gate/internal/gate"
)

const version = "0.1.0"

const usage = `junit-integrity-gate checks classic JUnit XML against an explicit file manifest.

Usage:
  junit-integrity-gate check [--json] [--duplicate-scope global|file] MANIFEST
  junit-integrity-gate --version
  junit-integrity-gate --help

Place flags before MANIFEST. Paths in the version-1 JSON manifest are relative
to its directory. Every listed report must contain a non-skipped testcase.
Output contains only fixed reason codes, ordinals and numeric counts.

Exit status: 0 healthy; 1 report/file/policy failure; 2 invalid CLI or manifest.
See README.md for the supported dialect, fixed budgets and trust boundaries.
`

func run(args []string, stdout, stderr io.Writer) int {
	badArgs := func() int {
		fmt.Fprintln(stderr, "junit-integrity-gate: invalid arguments (use --help)")
		return 2
	}
	if len(args) == 1 {
		switch args[0] {
		case "--version", "-version":
			if _, err := fmt.Fprintln(stdout, version); err != nil {
				return 2
			}
			return 0
		case "--help", "-h", "help":
			fmt.Fprint(stdout, usage)
			return 0
		}
	}
	if len(args) == 0 || args[0] != "check" {
		return badArgs()
	}
	flags := flag.NewFlagSet("check", flag.ContinueOnError)
	flags.SetOutput(io.Discard) // flag errors may echo private arguments.
	asJSON := flags.Bool("json", false, "write a versioned JSON report")
	scope := flags.String("duplicate-scope", "global", "global or file")
	if err := flags.Parse(args[1:]); err != nil {
		if err == flag.ErrHelp {
			fmt.Fprint(stdout, usage)
			return 0
		}
		return badArgs()
	}
	if flags.NArg() != 1 || (*scope != "global" && *scope != "file") {
		return badArgs()
	}
	report := gate.Check(flags.Arg(0), gate.Options{DuplicateScope: *scope})
	if *asJSON {
		encoder := json.NewEncoder(stdout)
		encoder.SetIndent("", "  ")
		if err := encoder.Encode(report); err != nil {
			fmt.Fprintln(stderr, "junit-integrity-gate: output_write_failed")
			return 2
		}
	} else {
		status := "FAIL"
		if report.OK {
			status = "PASS"
		}
		if _, err := fmt.Fprintf(stdout,
			"%s files=%d/%d cases=%d passed=%d failures=%d errors=%d skipped=%d non_skipped=%d duplicate_scope=%s\n",
			status, report.FilesChecked, report.FilesExpected, report.Totals.Cases,
			report.Totals.Passed, report.Totals.Failures, report.Totals.Errors,
			report.Totals.Skipped, report.Totals.NonSkipped, report.DuplicateScope); err != nil {
			return 2
		}
		for _, diagnostic := range report.Diagnostics {
			if _, err := fmt.Fprintf(stdout, "%s file=%d suite=%d case=%d\n",
				diagnostic.Code, diagnostic.File, diagnostic.Suite, diagnostic.Case); err != nil {
				return 2
			}
		}
		if report.DiagnosticsTruncated {
			fmt.Fprintln(stdout, "diagnostics_truncated")
		}
	}
	return report.ExitCode
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
