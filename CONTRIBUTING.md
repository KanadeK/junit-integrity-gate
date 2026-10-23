# Contributing

Report the version, platform, command and a minimal **synthetic** reproducer.
Do not upload production XML, private names, paths or failure logs. Security
reports use [SECURITY.md](SECURITY.md). Discuss dialect/policy changes before
loosening fail-closed behavior; valid producer XML can be outside this contract.

Use Go 1.27.3 or a newer maintained patch, and Python 3.11+ for development
scripts. The application itself needs no Python or external modules.

```sh
python scripts/check_format.py
go vet ./...
go test -count=1 '-coverpkg=./...' '-coverprofile=coverage.out' ./...
python scripts/check_coverage.py
python scripts/build_release.py --host-only
python scripts/installed_smoke.py
```

The coverage floor is 80% of statements. Coverage complements independently
specified fixtures and complete CLI workflows; it is not a correctness proof.
Build all four binary archives with `python scripts/build_release.py`.
To reproduce the bounded local fuzz session:

```sh
go test ./internal/gate -run='^$' -fuzz=FuzzXML -fuzztime=5s -parallel=2
```

The repository enforces LF source line endings through `.gitattributes`,
including on Windows checkouts with `core.autocrlf=true`, so gofmt sees the
same input on every tested platform.
Keep quotes around flags containing `=` and a dotted value: PowerShell can
otherwise split them into separate native arguments.

For real producer tests, install the test-only requirements in a virtual
environment and run against a built binary:

```sh
python -m venv .venv
# Activate .venv using your platform's usual command.
python -m pip install -r requirements-dev.txt
python -m ruff check .
python -m ruff format --check .
go build -trimpath -buildvcs=false -o build/ ./cmd/junit-integrity-gate
python acceptance/producer_smoke.py --binary build/junit-integrity-gate
```

On Windows append `.exe` to the binary. The script creates and removes synthetic
test projects in a temporary directory; its producers actually run tests.

Audit the application and the binary with the official vulnerability database:

```sh
go install golang.org/x/vuln/cmd/govulncheck@v1.9.0
govulncheck ./...
govulncheck -mode=binary build/junit-integrity-gate
```

These developer commands use the network for package/database downloads.
Application runtime does not. Preserve Go's BSD notice in binary archives and
all relevant third-party notices if new material is introduced. CI runs only
checks and builds; it never commits code. No automated dependency-update or
auto-merge workflow is enabled.
