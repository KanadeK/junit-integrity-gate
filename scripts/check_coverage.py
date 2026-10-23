"""Apply a statement coverage floor to the actual Go cover profile."""

import re
import subprocess
import sys

output = subprocess.check_output(["go", "tool", "cover", "-func=coverage.out"], text=True)
print(output, end="")
match = re.search(r"^total:\s+\(statements\)\s+(\d+(?:\.\d+)?)%\s*$", output, re.MULTILINE)
if match is None or float(match.group(1)) < 80:
    sys.exit("Statement coverage is below the documented 80% floor")
