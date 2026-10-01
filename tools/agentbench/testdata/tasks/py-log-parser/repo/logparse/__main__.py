import sys

from . import parse_lines, summarize


def main(argv):
    if len(argv) != 2:
        print("usage: python3 -m logparse FILE", file=sys.stderr)
        return 2
    with open(argv[1], encoding="utf-8") as f:
        print(summarize(parse_lines(f)))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
