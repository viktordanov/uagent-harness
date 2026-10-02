"""Compares the agent's snapshot with the API's responses."""
import json
import os
import sys

here = os.path.dirname(os.path.abspath(__file__))
want = json.load(open(os.path.join(here, "snapshot.expected.json")))
path = os.path.join("data", "snapshot.json")
try:
    got = json.load(open(path))
except (OSError, ValueError) as e:
    print(f"{path}: {e}")
    sys.exit(1)
failures = []
if not isinstance(got, dict):
    failures.append(f"{path}: not a JSON object")
else:
    for key in want:
        if got.get(key) != want[key]:
            failures.append(f"{path}: {key} differs from the API's response")
    for key in sorted(set(got) - set(want)):
        failures.append(f"{path}: unexpected key {key}")
for f in failures:
    print(f)
sys.exit(1 if failures else 0)
