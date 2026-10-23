"""Build reproducible-layout archives; runtime application has no Python dependency."""

import argparse
import hashlib
import os
import subprocess
import tempfile
import zipfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
VERSION = "0.1.0"
TARGETS = (("windows", "amd64"), ("linux", "amd64"), ("darwin", "amd64"), ("darwin", "arm64"))


def build(targets=TARGETS):
    destination = ROOT / "dist"
    destination.mkdir(exist_ok=True)
    archives = []
    for system, architecture in targets:
        archive = destination / f"junit-integrity-gate_{VERSION}_{system}_{architecture}.zip"
        with tempfile.TemporaryDirectory(prefix="junit-gate-build-") as staging:
            name = "junit-integrity-gate" + (".exe" if system == "windows" else "")
            binary = Path(staging) / name
            subprocess.run(
                [
                    "go",
                    "build",
                    "-trimpath",
                    "-buildvcs=false",
                    "-ldflags=-s -w",
                    "-o",
                    str(binary),
                    "./cmd/junit-integrity-gate",
                ],
                cwd=ROOT,
                env={**os.environ, "GOOS": system, "GOARCH": architecture, "CGO_ENABLED": "0"},
                check=True,
            )
            members = [(name, binary.read_bytes(), 0o755)]
            for relative in (
                "LICENSE",
                "README.md",
                "README.zh-CN.md",
                "SECURITY.md",
                "THIRD_PARTY.md",
                "third_party/Go-LICENSE.txt",
                "third_party/Go-PATENTS.txt",
                "examples/expected.json",
                "examples/reports/shard-1.xml",
                "examples/reports/shard-2.xml",
            ):
                members.append((relative, (ROOT / relative).read_bytes(), 0o644))
            with zipfile.ZipFile(
                archive, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9
            ) as output:
                for relative, data, permissions in members:
                    info = zipfile.ZipInfo(relative, (2026, 1, 1, 0, 0, 0))
                    info.create_system = 3
                    info.external_attr = (0o100000 | permissions) << 16
                    info.compress_type = zipfile.ZIP_DEFLATED
                    output.writestr(info, data)
        archives.append(archive)
        print(archive.name)
    checksums = "".join(
        hashlib.sha256(path.read_bytes()).hexdigest() + "  " + path.name + "\n" for path in archives
    )
    (destination / "SHA256SUMS.txt").write_text(checksums, encoding="ascii", newline="\n")
    return archives


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--host-only", action="store_true", help="build only the native target for a smoke test"
    )
    arguments = parser.parse_args()
    selected = TARGETS
    if arguments.host_only:
        system = subprocess.check_output(["go", "env", "GOOS"], text=True).strip()
        architecture = subprocess.check_output(["go", "env", "GOARCH"], text=True).strip()
        selected = ((system, architecture),)
    build(selected)
