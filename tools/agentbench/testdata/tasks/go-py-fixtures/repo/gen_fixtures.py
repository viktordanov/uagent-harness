#!/usr/bin/env python3
"""Generate roster/testdata: a roster TSV and the JSON the Go loader must produce."""

import json
import os

MEMBERS = [
    ("Alice", "platform"),
    ("Bob", "platform"),
    ("Carol", "data"),
    ("Dmitri", "data"),
    ("Erin", "web"),
    ("Frank", "web"),
]

OUT = os.path.join(os.path.dirname(os.path.abspath(__file__)), "roster", "testdata")


def main():
    os.makedirs(OUT, exist_ok=True)
    with open(os.path.join(OUT, "roster.tsv"), "w", encoding="utf-8") as f:
        for name, team in MEMBERS:
            f.write(f"{name}\t{team}\n")
    expected = [{"name": name, "team": team} for name, team in MEMBERS]
    with open(os.path.join(OUT, "expected.json"), "w", encoding="utf-8") as f:
        json.dump(expected, f, indent=2)
        f.write("\n")


if __name__ == "__main__":
    main()
