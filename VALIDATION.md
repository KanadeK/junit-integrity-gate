# Validation scope for v0.1.0

Local checks were executed on Windows x64 with Go 1.27.3 on 2026-10-24.
Python 3.13.16 was used for developer scripts and report generators only.

- 37 top-level Go test groups: 35 passed, two symlink-creation tests skipped
  because the local Windows process lacked that privilege. 157 named table
  subcases and all eight fuzz seeds passed. The external process suite includes
  17 independent CLI groups, actual exits, JSON counters, privacy sentinels,
  oversized reports and input-byte preservation.
- Combined application statement coverage was **90.8%** with
  `go test -count=1 '-coverpkg=./...' '-coverprofile=coverage.out' ./...`.
  Separately built external CLI processes are exercised but not included in
  that cover profile. The CI floor is 80%; coverage is not a correctness proof.
- A real five-second `FuzzXML` run with two workers completed without failure
  (473,008 executions reported by Go). Its input cap is 4,096 bytes and its
  parser budgets are intentionally small. This is a bounded robustness sample,
  not exhaustive fuzzing or a memory-safety guarantee.
- gofmt, `go vet`, Ruff checks and Python formatting checks passed.
- pytest **9.1.1** and unittest-xml-reporting **4.0.0** actually generated reports
  for ten scenarios: pass/skip/parameters/subtests, call failure, setup,
  collection, teardown and combined call-plus-teardown problems. Generator
  exits and gate exits were independently checked; privacy checks passed.
  The combined pytest phase records are conservatively rejected as duplicates.
- All four CGO-disabled release archives built successfully: Windows x64,
  Linux x64, macOS Intel and macOS Apple Silicon. Archives retain the project
  MIT license, Go BSD notice and Go patent grant. No machine-specific build
  path or credential pattern was found in their contents.
- A fresh native archive extraction verified checksums, version, the complete
  bundled example, changed counts, a missing shard, source hashes and cleanup.
  A separate copied source tree built with `GOPROXY=off` and `GOSUMDB=off`,
  ran the example and removed its installed binary successfully.
- `go list -m all` contained only this project. govulncheck **v1.9.0** reported
  no known vulnerabilities for the source, native binary and all four archive
  binaries. Its reported database update was 2026-10-21. This result is scoped
  to the scanner/database and code scanned, not every developer dependency or
  the operating system.
- Independent review corrected native decoder error classification, a namespace
  prefix edge case, and macOS-style parent-directory aliases. File replacement,
  observed in-place mutation and manifest-change regressions passed.

CI runs native tests, real producers and archive extraction on Ubuntu x64,
Windows x64, macOS Apple Silicon and macOS Intel, plus a separate source/binary
audit job. The development host did not execute other operating systems locally;
consult the [actual Actions runs](https://github.com/KanadeK/junit-integrity-gate/actions)
for remote outcomes. CI never commits code.

Maven/Gradle-style fixtures are original contract inputs; JVM producers were
not executed. This release does not cover every JUnit dialect, merged retries,
freshness, testcase inventory, execution provenance, atomic filesystem
snapshots or a hard wall-clock deadline. Archives are unsigned; macOS binaries
are not notarized. See README.md for the exact supported contract and limits.
