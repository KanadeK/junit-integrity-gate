# Security policy

The latest tagged release is supported. Security fixes can require rebuilding
with a maintained Go toolchain patch. The distributed program makes no network
requests, executes no subprocesses and has no telemetry code.

Use GitHub's [private vulnerability reporting](https://github.com/KanadeK/junit-integrity-gate/security/advisories/new).
Include the binary version, platform, impact and a minimal synthetic reproducer.
If private reporting is unavailable, open an issue requesting a private contact
channel without publishing the vulnerability. Never upload production reports,
credentials, private test names or failure logs.

The bounded token decoder and rooted file opens are defenses in depth. The Go
runtime, operating system and a stable artifact directory remain trusted.
Checks do not establish report authenticity or execution provenance. File
checks do not make a hostile concurrently modified filesystem a snapshot.
Use an external CI timeout for a strict wall-clock deadline.
