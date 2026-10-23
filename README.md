# junit-integrity-gate

Catch missing shards, copied reports and contradictory counts before a JUnit
artifact is accepted by CI. A local CLI with an explicit expected-file manifest,
fixed resource budgets and output that omits test names and failure logs.

[中文说明](README.zh-CN.md) · [Validation](VALIDATION.md) · [Security](SECURITY.md)

## Why use it?

A report viewer can display the XML it receives while a missing shard goes
unnoticed. A copied shard can also inflate a green total. This gate checks a
list you define **before the test jobs run**, then compares declared counts with
the testcase records actually present. Missing, empty, all-skipped, repeated,
contradictory or failing reports produce a nonzero exit status.

It does not run tests or prove that a self-consistent report is fresh, genuine,
complete against a testcase inventory, or produced from the intended commit.
Keep the runner's exit status and bind artifacts to the correct CI run separately.

## Install

Download the archive matching your machine from
[v0.1.0 Releases](https://github.com/KanadeK/junit-integrity-gate/releases/tag/v0.1.0).
Available targets: Windows x64, Linux x64, macOS Intel and macOS Apple Silicon.
Compare its SHA-256 digest with `SHA256SUMS.txt` on that release. The binaries
include the Go runtime; no Go, Python, paid API or account is needed to run them.
Archives are unsigned and macOS builds are not notarized. Build from source
where your local policy requires it.

Windows PowerShell, after downloading the Windows archive and checksum file:

```powershell
Get-FileHash .\junit-integrity-gate_0.1.0_windows_amd64.zip -Algorithm SHA256
Get-Content .\SHA256SUMS.txt
Expand-Archive .\junit-integrity-gate_0.1.0_windows_amd64.zip .\junit-gate
.\junit-gate\junit-integrity-gate.exe --version
```

Linux x64, after downloading the Linux archive and checksum file:

```sh
sha256sum junit-integrity-gate_0.1.0_linux_amd64.zip
cat SHA256SUMS.txt
unzip junit-integrity-gate_0.1.0_linux_amd64.zip -d junit-gate
chmod +x junit-gate/junit-integrity-gate
./junit-gate/junit-integrity-gate --version
```

On macOS select the corresponding `darwin` archive and use `shasum -a 256`.
Put the extracted binary on your PATH or use its full path. Retain the bundled
license notices when redistributing it. Checksums detect corruption; they are
not an independent signature of the publisher.

To build from source, use Go **1.27.3 or a later maintained security patch**:

```sh
git clone --branch v0.1.0 https://github.com/KanadeK/junit-integrity-gate.git
cd junit-integrity-gate
go build -trimpath -buildvcs=false -o build/ ./cmd/junit-integrity-gate
```

Alternatively, `go install github.com/KanadeK/junit-integrity-gate/cmd/junit-integrity-gate@v0.1.0`
installs into your Go bin directory. Only the Go standard library is imported.
After a source build, use `./build/junit-integrity-gate` or
`.\build\junit-integrity-gate.exe` for the commands below.

## Try the complete workflow

From a source checkout or an extracted release directory:

```sh
junit-integrity-gate check --json examples/expected.json
```

Use `./junit-integrity-gate` (POSIX) or `.\junit-integrity-gate.exe` (PowerShell)
when running directly from the extracted directory. The bundled example has
two files, three cases, two passes and one skip; the command exits **0**.

Create a manifest for your own reports:

```json
{"version":1,"files":["reports/shard-1.xml","reports/shard-2.xml"]}
```

Paths are relative to the **manifest directory**, independently of the current
working directory. Do not generate this list from a glob of the reports that
happen to exist: an already missing shard would disappear from the expectation.
List exactly the reports your job matrix should produce.

```sh
junit-integrity-gate check expected.json
junit-integrity-gate check --json expected.json
junit-integrity-gate check --duplicate-scope file --json expected.json
```

Place flags before the manifest. Default `global` duplicate detection uses the
exact `(classname or empty string, name)` across all listed files. Parameter
suffixes, case and Unicode remain significant. `file` scope permits the same
identity across separate platform reports, while still rejecting repeats within
a file. Supplying the same physical file twice is rejected in either mode.

To observe a failure safely, make a temporary copy of `examples`, change
`tests="2"` to `tests="3"` in the first report and rerun against the copied
manifest. Expect exit **1** and a count-mismatch diagnostic. Removing a listed
report also fails; neither operation changes the original bundled example.

## Supported report contract

JUnit XML has several incompatible dialects. Version 0.1.0 supports a strict
**classic, unretried, unnamespaced** subset:

- One root `testsuite`, or a root `testsuites` wrapper containing suites.
- Suites can contain direct testcase records and nested suites. Every suite
  must declare `tests`; its count includes **all descendant cases**. Wrapper
  counters and `failures`, `errors`, `skipped` are optional, but checked whenever
  present. Counters must be nonnegative ASCII decimal integers.
- Each testcase needs a nonblank `name`; `classname` is optional. A testcase is
  counted once. Multiple failure nodes count one failed case. Failure and error
  on one case increment both counters. A skip combined with a problem is invalid.
- Any failure or error fails the gate. Every expected file needs at least one
  non-skipped case. `non_skipped` does not establish that a test body ran: setup
  and collection errors can also produce such records.
- `properties`, `system-out`, `system-err` and outcome contents are opaque.
  Their contents cannot create testcases, but still consume byte/depth/node
  budgets. Unknown structural or result children fail closed.
- Retry extensions `flakyFailure`, `flakyError`, `rerunFailure`, `rerunError`
  are unsupported. Structural elements and semantic attributes must be
  unnamespaced. Nonstandard `status`, `result`, `outcome`, `disabled` attributes
  on suites/cases are rejected rather than guessed. DTDs and non-UTF-8 encodings
  are rejected.
  Standard predefined XML escapes and UTF-8 text are supported.

Ordinary pytest xunit2 and unittest-xml-reporting reports are exercised by the
producer acceptance script. Maven/Gradle-style **unretried** fixtures are covered
by contract tests, not a claim of every producer configuration being supported.
Merged reruns and pytest call-failure-plus-teardown-error duplicate phase records
can be valid producer output and are intentionally rejected by this policy.

## Machine output and exit statuses

`--json` writes a version-1 object with `ok`, `exit_code`, `duplicate_scope`,
`files_expected`, `files_checked`, `totals` and `diagnostics`. Counts are observed
records; when parsing fails they can be partial. A failed report must never be
used as a complete inventory. Diagnostic ordinals are one-based and map files
to their order in the manifest. A zero or absent ordinal means not applicable.

Output omits paths, suite/class/case names, logs, properties and raw parser
exceptions. It contains fixed reason codes and numeric evidence. Preserve the
manifest privately to map ordinals back to the source. No arbitrary input is
echoed in CLI errors.

| Exit | Meaning |
| --- | --- |
| 0 | Every listed file meets this policy. |
| 1 | Missing/unreadable/invalid report, unsupported dialect, exhausted budget, duplicate, count mismatch, skipped-only report or test problem. |
| 2 | Invalid CLI, manifest or configuration; or an output write failure. |

## Limits and filesystem boundary

The CLI has fixed limits: 1 MiB manifest, 256 files, 16 MiB per XML file,
64 MiB total XML, 500,000 XML elements, depth 64, 100,000 cases, 64 attributes
per element, 8,192 bytes per identity component and 100 diagnostics. These
include opaque logs. They bound accepted input, not all operating-system or
parser allocations. No strict wall-clock timeout or security sandbox is claimed.
Use the CI job's timeout for a hard external deadline.

Only explicit regular report files below the manifest directory are opened.
Absolute, drive, UNC, backslash, empty, `.` and `..` report path components,
symlink traversal and physical aliases are rejected. The caller-selected
manifest directory is canonicalized once, permitting OS directory aliases
such as macOS `/var`; the manifest file itself and report components below
that root cannot be links. Rooted filesystem access constrains report opens
to that directory. Use stable finished artifacts in a directory you
control; pre/post checks are not an atomic snapshot of a concurrently modified
filesystem. The tool does not inspect unlisted XML or read external entities.
It writes no input files and makes no network requests or telemetry calls.

## Compared with existing tools

[junitparser](https://github.com/weiwei/junitparser) is an XML manipulation and
merge library. [dorny/test-reporter](https://github.com/dorny/test-reporter)
publishes CI test summaries. [Harness-Gate](https://github.com/musutrade/Harness-Gate)
is a broader quality-gate project. This CLI provides a smaller offline check:
an explicit shard manifest, observed/declaration reconciliation, privacy-limited
diagnostics and conservative duplicate/retry handling. It complements test
runners and report viewers; it cannot replace their execution evidence.

## Uninstall and cleanup

Delete the extracted directory and any binary you copied onto PATH. For
`go install`, delete `junit-integrity-gate` (`.exe` on Windows) from your Go bin
directory. Remove only the temporary example/report copies you created. The
program creates no configuration files, caches, services or background jobs.

## Development and license

See [CONTRIBUTING.md](CONTRIBUTING.md) for test, producer, fuzz, build and audit
commands. See [VALIDATION.md](VALIDATION.md) for executed checks and limits.
Original project code is [MIT](LICENSE). Distributed binaries also include the
Go runtime under its [BSD license](third_party/Go-LICENSE.txt);
[THIRD_PARTY.md](THIRD_PARTY.md) records this distinction.
