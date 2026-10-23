"""Run pinned real report producers, then independently check the external CLI.

Install acceptance/requirements.txt into a disposable test environment first.
Generated XML, runner output and test identities stay in private temporary files;
this script prints only producer versions, statuses and aggregate gate results.
"""

import argparse
import importlib.metadata
import json
import os
import subprocess
import sys
import tempfile
import textwrap
from pathlib import Path

VERSIONS = {"pytest": "9.1.1", "unittest-xml-reporting": "4.0.0"}
PRIVATE = "PRIVATE_PRODUCER_SENTINEL_a719e3"


def run(arguments, directory):
    environment = os.environ.copy()
    environment.pop("PYTHONPATH", None)
    environment["PYTEST_DISABLE_PLUGIN_AUTOLOAD"] = "1"
    environment["PYTHONIOENCODING"] = "utf-8"
    return subprocess.run(
        list(map(str, arguments)),
        cwd=directory,
        env=environment,
        capture_output=True,
        text=True,
        encoding="utf-8",
        timeout=45,
        check=False,
    )


def require_versions():
    for package, expected in VERSIONS.items():
        try:
            installed = importlib.metadata.version(package)
        except importlib.metadata.PackageNotFoundError:
            raise SystemExit("Install the pinned test-only acceptance requirements first") from None
        if installed != expected:
            raise SystemExit("Pinned report-producer version mismatch")


def expected(cases, passed=0, failures=0, errors=0, skipped=0):
    return {
        "cases": cases,
        "passed": passed,
        "failures": failures,
        "errors": errors,
        "skipped": skipped,
        "non_skipped": cases - skipped,
    }


def check_report(binary, directory, report, label, expected_totals, gate_exit, producer_exit):
    manifest = directory / "expected.json"
    manifest.write_text(json.dumps({"version": 1, "files": [report.name]}), encoding="utf-8")
    result = run([binary, "check", "--json", manifest], directory)
    if PRIVATE in result.stdout + result.stderr or str(directory) in result.stdout + result.stderr:
        raise RuntimeError(f"{label}: gate disclosed private producer data")
    if result.returncode != gate_exit:
        raise RuntimeError(f"{label}: unexpected gate exit code {result.returncode}")
    try:
        summary = json.loads(result.stdout)
    except ValueError:
        raise RuntimeError(f"{label}: gate did not return a JSON report") from None
    if (
        summary.get("version") != 1
        or summary.get("exit_code") != gate_exit
        or summary.get("ok") is not (gate_exit == 0)
    ):
        raise RuntimeError(f"{label}: inconsistent gate report envelope")
    totals = summary.get("totals", {})
    if totals != expected_totals:
        raise RuntimeError(f"{label}: aggregate counters differ from independent expectations")
    if summary.get("files_expected") != 1 or summary.get("files_checked") != 1:
        raise RuntimeError(f"{label}: producer report was not checked")
    codes = sorted({entry["code"] for entry in summary.get("diagnostics", [])})
    if gate_exit and not codes:
        raise RuntimeError(f"{label}: failure has no diagnostic reason")
    return {
        "scenario": label,
        "producer_exit_code": producer_exit,
        "gate_exit_code": gate_exit,
        "totals": totals,
        "diagnostic_codes": codes,
    }


def pytest_report(root, binary, label, source, producer_exit, totals, gate_exit):
    directory = root / label
    directory.mkdir()
    test = directory / "test_synthetic.py"
    test.write_text(textwrap.dedent(source).replace("PRIVATE_SENTINEL", PRIVATE), encoding="utf-8")
    report = directory / (PRIVATE + ".xml")
    generated = run(
        [
            sys.executable,
            "-m",
            "pytest",
            "-q",
            "-p",
            "no:cacheprovider",
            "--confcutdir=.",
            "-o",
            "junit_family=xunit2",
            "-o",
            "junit_logging=all",
            "--junitxml=" + str(report),
            test.name,
        ],
        directory,
    )
    if generated.returncode != producer_exit:
        raise RuntimeError(f"{label}: unexpected pytest status {generated.returncode}")
    if not report.is_file():
        raise RuntimeError(f"{label}: pytest did not produce XML")
    return check_report(binary, directory, report, label, totals, gate_exit, producer_exit)


def xmlrunner_report(root, binary, label, methods, producer_exit, totals, gate_exit):
    directory = root / label
    directory.mkdir()
    source = """\
import sys
import unittest
import xmlrunner

class Synthetic(unittest.TestCase):
METHODS

with open(REPORT, "wb") as output:
    result = xmlrunner.XMLTestRunner(output=output, verbosity=0).run(
        unittest.defaultTestLoader.loadTestsFromTestCase(Synthetic))
raise SystemExit(0 if result.wasSuccessful() else 1)
"""
    report = directory / (PRIVATE + ".xml")
    methods = textwrap.indent(textwrap.dedent(methods).strip(), "    ")
    source = source.replace("METHODS", methods).replace("REPORT", repr(report.name))
    source = source.replace("PRIVATE_SENTINEL", PRIVATE)
    script = directory / "produce.py"
    script.write_text(source, encoding="utf-8")
    generated = run([sys.executable, script.name], directory)
    if generated.returncode != producer_exit:
        raise RuntimeError(f"{label}: unexpected xmlrunner status {generated.returncode}")
    if not report.is_file():
        raise RuntimeError(f"{label}: xmlrunner did not produce XML")
    return check_report(binary, directory, report, label, totals, gate_exit, producer_exit)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--binary", required=True, type=Path)
    args = parser.parse_args()
    binary = args.binary.resolve(strict=True)
    require_versions()
    with tempfile.TemporaryDirectory(prefix="junit-producer-acceptance-") as temporary:
        root = Path(temporary)
        results = []
        results.append(
            pytest_report(
                root,
                binary,
                "pytest-pass-skip-parameters",
                """
            import pytest
            def test_pass():
                print("PRIVATE_SENTINEL")
                assert True
            @pytest.mark.skip(reason="PRIVATE_SENTINEL")
            def test_skipped():
                raise AssertionError("must not execute")
            @pytest.mark.parametrize("value", [1, 2])
            def test_parameter(value):
                assert value > 0
        """,
                0,
                expected(4, passed=3, skipped=1),
                0,
            )
        )
        results.append(
            pytest_report(
                root,
                binary,
                "pytest-call-failure",
                """
            def test_failure():
                assert False, "PRIVATE_SENTINEL"
        """,
                1,
                expected(1, failures=1),
                1,
            )
        )
        results.append(
            pytest_report(
                root,
                binary,
                "pytest-setup-error",
                """
            import pytest
            @pytest.fixture
            def resource():
                raise RuntimeError("PRIVATE_SENTINEL")
            def test_body(resource):
                raise AssertionError("body must not execute")
        """,
                1,
                expected(1, errors=1),
                1,
            )
        )
        results.append(
            pytest_report(
                root,
                binary,
                "pytest-collection-error",
                """
            raise RuntimeError("PRIVATE_SENTINEL")
        """,
                2,
                expected(1, errors=1),
                1,
            )
        )
        results.append(
            pytest_report(
                root,
                binary,
                "pytest-teardown-error",
                """
            import pytest
            @pytest.fixture
            def resource():
                yield object()
                raise RuntimeError("PRIVATE_SENTINEL")
            def test_body(resource):
                assert resource is not None
        """,
                1,
                expected(1, errors=1),
                1,
            )
        )
        results.append(
            pytest_report(
                root,
                binary,
                "pytest-call-plus-teardown",
                """
            import pytest
            @pytest.fixture
            def resource():
                yield object()
                raise RuntimeError("PRIVATE_SENTINEL")
            def test_body(resource):
                assert False, "PRIVATE_SENTINEL"
        """,
                1,
                expected(2, failures=1, errors=1),
                1,
            )
        )
        results.append(
            xmlrunner_report(
                root,
                binary,
                "xmlrunner-pass-skip-subtests",
                """
            def test_pass(self):
                print("PRIVATE_SENTINEL")
                self.assertTrue(True)
            @unittest.skip("PRIVATE_SENTINEL")
            def test_skipped(self):
                self.fail("must not execute")
            def test_subtests(self):
                for value in (1, 2):
                    with self.subTest(value=value):
                        self.assertGreater(value, 0)
        """,
                0,
                expected(3, passed=2, skipped=1),
                0,
            )
        )
        results.append(
            xmlrunner_report(
                root,
                binary,
                "xmlrunner-failing-subtest",
                """
            def test_pass(self):
                self.assertTrue(True)
            def test_subtests(self):
                for value in (0, 1):
                    with self.subTest(value=value):
                        self.assertEqual(value, 0, "PRIVATE_SENTINEL")
        """,
                1,
                expected(2, passed=1, failures=1),
                1,
            )
        )
        results.append(
            xmlrunner_report(
                root,
                binary,
                "xmlrunner-setup-error",
                """
            def setUp(self):
                raise RuntimeError("PRIVATE_SENTINEL")
            def test_body(self):
                self.fail("must not execute")
        """,
                1,
                expected(1, errors=1),
                1,
            )
        )
        results.append(
            xmlrunner_report(
                root,
                binary,
                "xmlrunner-teardown-error",
                """
            def test_body(self):
                self.assertTrue(True)
            def tearDown(self):
                raise RuntimeError("PRIVATE_SENTINEL")
        """,
                1,
                expected(1, errors=1),
                1,
            )
        )
    print(json.dumps({"producer_versions": VERSIONS, "scenarios": results}, sort_keys=True))


if __name__ == "__main__":
    main()
