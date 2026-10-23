"""Exercise an extracted native archive from a new directory, then remove it."""

import argparse
import hashlib
import json
import os
import platform
import shutil
import subprocess
import tempfile
import zipfile
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def smoke(archive):
    checksums = dict(
        row.split("  ", 1)[::-1]
        for row in (archive.parent / "SHA256SUMS.txt").read_text().splitlines()
    )
    assert hashlib.sha256(archive.read_bytes()).hexdigest() == checksums[archive.name]
    with tempfile.TemporaryDirectory(prefix="junit-gate-fresh-") as temporary:
        fresh = Path(temporary)
        install = fresh / "installed"
        install.mkdir()
        with zipfile.ZipFile(archive) as bundle:
            for item in bundle.infolist():
                relative = Path(item.filename)
                assert (
                    not relative.is_absolute()
                    and ".." not in relative.parts
                    and "\\" not in item.filename
                )
            bundle.extractall(install)
        assert (install / "LICENSE").is_file() and (
            install / "third_party" / "Go-LICENSE.txt"
        ).is_file()
        command = install / (
            "junit-integrity-gate.exe" if os.name == "nt" else "junit-integrity-gate"
        )
        command.chmod(0o755)

        def run(*args, expected=0):
            process = subprocess.run(
                [str(command), *map(str, args)],
                cwd=fresh,
                capture_output=True,
                text=True,
                encoding="utf-8",
                timeout=30,
                check=False,
            )
            assert process.returncode == expected, (
                process.returncode,
                process.stdout,
                process.stderr,
            )
            return process.stdout

        assert run("--version").strip() == "0.1.0"
        manifest = install / "examples" / "expected.json"
        before = {
            path: hashlib.sha256(path.read_bytes()).hexdigest()
            for path in (install / "examples").rglob("*")
            if path.is_file()
        }
        result = json.loads(run("check", "--json", manifest))
        assert result["ok"] and result["totals"] == {
            "cases": 3,
            "passed": 2,
            "failures": 0,
            "errors": 0,
            "skipped": 1,
            "non_skipped": 2,
        }
        assert before == {path: hashlib.sha256(path.read_bytes()).hexdigest() for path in before}
        broken = fresh / "broken"
        shutil.copytree(install / "examples", broken)
        report = broken / "reports" / "shard-1.xml"
        report.write_text(
            report.read_text(encoding="utf-8").replace('tests="2"', 'tests="3"'), encoding="utf-8"
        )
        assert not json.loads(run("check", "--json", broken / "expected.json", expected=1))["ok"]
        (broken / "reports" / "shard-2.xml").unlink()
        assert not json.loads(run("check", "--json", broken / "expected.json", expected=1))["ok"]
        shutil.rmtree(install)
        assert not command.exists()
    print(
        "Archive checksum, fresh extraction, version, complete example, rejection, input hashes and cleanup passed."
    )


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--archive", type=Path)
    arguments = parser.parse_args()
    operating_system = {"Windows": "windows", "Linux": "linux", "Darwin": "darwin"}[
        platform.system()
    ]
    architecture = "arm64" if platform.machine().lower() in {"arm64", "aarch64"} else "amd64"
    smoke(
        arguments.archive
        or ROOT / "dist" / f"junit-integrity-gate_0.1.0_{operating_system}_{architecture}.zip"
    )
