"""Cross-platform gofmt check without rewriting sources."""

import subprocess
import sys
from pathlib import Path

root = Path(__file__).resolve().parents[1]
files = sorted(
    str(path.relative_to(root))
    for path in root.rglob("*.go")
    if not {"dist", "build", ".venv", ".git"}.intersection(path.relative_to(root).parts)
)
result = subprocess.run(
    ["gofmt", "-l", *files], cwd=root, capture_output=True, text=True, check=True
)
if result.stdout.strip():
    print(result.stdout)
    sys.exit("Go files need gofmt")
print("gofmt check passed")
