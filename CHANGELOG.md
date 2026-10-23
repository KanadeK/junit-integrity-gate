# v0.1.0

Initial release of a standalone classic JUnit report integrity gate.

- Strict explicit-file manifest catches missing reports and physical aliases.
- Reconciles declared suite/wrapper counts with observed descendant cases.
- Detects duplicate identities, empty/all-skipped files and test problems.
- Bounded token parsing rejects DTDs, namespaces, retry extensions and unknown
  structural/result nodes; diagnostics omit names, paths and report contents.
- Global duplicate scope by default, explicit file scope for platform matrices.
- Standalone Windows, Linux and macOS archives with checksums and license notices.

This version supports a conservative unretried classic dialect, with fixed
budgets. It does not prove freshness, testcase completeness or execution
provenance. See README.md and VALIDATION.md for interoperability and limits.
