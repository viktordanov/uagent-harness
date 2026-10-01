#!/usr/bin/env python3
"""Generate roster/testdata: a roster TSV and the JSON the Go loader must produce."""

import json
import os

MEMBERS = [
    ("Alice", "platform", "lead"),
    ("Bob", "platform", "member"),
    ("Carol", "data", "member"),
    ("Dmitri", "data", "lead"),
    ("Erin", "web", "member"),
    ("Frank", "web", "contractor"),
]

OUT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "roster", "testdata")


def main():
    os.makedirs(OUT, exist_ok=True)
    with open(os.path.join(OUT, "roster.tsv"), "w", encoding="utf-8") as f:
        for name, team, role in MEMBERS:
            f.write(f"{name}\t{team}\t{role}\n")
    expected = [{"name": name, "team": team, "role": role} for name, team, role in MEMBERS]
    with open(os.path.join(OUT, "expected.json"), "w", encoding="utf-8") as f:
        json.dump(expected, f, indent=2)
        f.write("\n")


if __name__ == "__main__":
    main()
