"""The agentbench check of go-large-repo-guide: docs/ARCHITECTURE.md covers
every package, the middleware steps, the encoders, and real gotchas."""
import os
import re
import subprocess
import sys

try:
    doc = open("docs/ARCHITECTURE.md").read()
except FileNotFoundError:
    sys.exit("no docs/ARCHITECTURE.md")
problems = []
if not 6_000 <= len(doc.encode()) <= 120_000:
    problems.append(f"size {len(doc.encode())} bytes, want 6 to 120 KB")
pkgs = sorted({os.path.dirname(p)[2:] for p in subprocess.run(["find", ".", "-name", "*.go", "-not", "-path", "./.github/*"], capture_output=True, text=True).stdout.split() if os.path.dirname(p) != "."})
missing = [p for p in pkgs if not re.search(r"(?<![\w/])" + re.escape(p) + r"(?![\w])", doc)]
if len(missing) > 3:
    problems.append(f"{len(missing)} of {len(pkgs)} packages not mentioned: {missing}")
low = doc.lower()
for step in ["initialize", "serialize", "build", "finalize", "deserialize"]:
    if step not in low:
        problems.append(f"middleware step {step} not described")
for typ in ["InitializeInput", "SerializeInput", "DeserializeOutput", "RawResponse", "Before", "After"]:
    if typ not in doc:
        problems.append(f"{typ} not mentioned")
for head in ["middleware stack", "encoding", "transport", "documents", "waiters", "gotchas"]:
    if not re.search(r"^#+ .*" + head, low, re.M):
        problems.append(f"no section heading with {head!r}")
gotchas = re.split(r"^#+ .*gotchas.*$", doc, flags=re.M | re.I)
cited = {f for f in re.findall(r"[\w./-]+\.go", gotchas[-1]) if os.path.isfile(f.lstrip("./"))} if len(gotchas) > 1 else set()
if len(cited) < 4:
    problems.append(f"the gotchas cite {len(cited)} existing files, want at least 4: {sorted(cited)}")
for p in problems:
    print(p)
sys.exit(1 if problems else 0)
