"""Compares the agent's snapshots with the API's data."""
import json
import os
import sys

here = os.path.dirname(os.path.abspath(__file__))
failures = []
for name in ("items", "audit"):
    want = json.load(open(os.path.join(here, name + ".expected.json")))
    path = os.path.join("data", name + ".json")
    try:
        got = json.load(open(path))
    except (OSError, ValueError) as e:
        failures.append(f"{path}: {e}")
        continue
    if got != want:
        ids = [x.get("id") if isinstance(x, dict) else x for x in got] if isinstance(got, list) else got
        failures.append(f"{path}: differs from the API's data (got ids {ids})")
for f in failures:
    print(f)
sys.exit(1 if failures else 0)
