"""Command line: python3 -m salesrep COMMAND [options] FILE...

Commands:
    summary   totals per group (--by region|product|month, --format text|json)
    top       best-selling products by total (--n N)
    units     units sold per group (--by region|product)
"""

import argparse
import sys

from . import aggregate, filters
from .formatting import format_summary, format_summary_json, format_top, format_units
from .loader import LoadError, load_files


def build_parser():
    parser = argparse.ArgumentParser(prog="salesrep", description="Reports over CSV sales exports.")
    sub = parser.add_subparsers(dest="command", required=True)

    def common(p):
        p.add_argument("files", nargs="+", metavar="FILE", help="sales export (CSV)")
        p.add_argument("--region", help="only this region")
        p.add_argument("--product", help="only this product")
        p.add_argument("--from", dest="start", type=filters.parse_day, help="first day, YYYY-MM-DD")
        p.add_argument("--to", dest="end", type=filters.parse_day, help="last day, YYYY-MM-DD")

    summary = sub.add_parser("summary", help="totals per group")
    common(summary)
    summary.add_argument("--by", choices=sorted(aggregate.GROUP_KEYS), default="region")
    summary.add_argument("--format", choices=["text", "json"], default="text")

    top = sub.add_parser("top", help="best-selling products")
    common(top)
    top.add_argument("--n", type=int, default=5, help="how many products (default 5)")

    units = sub.add_parser("units", help="units sold per group")
    common(units)
    units.add_argument("--by", choices=sorted(aggregate.GROUP_KEYS), default="product")
    return parser


class Loaded:
    """Rows after the filters, and how many rows the loader skipped."""

    def __init__(self, rows, skipped):
        self.rows = rows
        self.skipped = skipped


def load(args, err):
    skipped = []

    def on_skip(path, line, reason):
        skipped.append((path, line))
        print(f"warning: {path}: line {line}: {reason}, skipped", file=err)

    rows = load_files(args.files, on_skip)
    rows = filters.apply(rows, args.region, args.product, args.start, args.end)
    return Loaded(rows, len(skipped))


def cmd_summary(args, out, err):
    loaded = load(args, err)
    pairs = aggregate.total_by(loaded.rows, by=args.by)
    if args.format == "json":
        print(format_summary_json(pairs), file=out)
        return
    grand = aggregate.grand_total(loaded.rows)
    print(format_summary(pairs, args.by, grand, len(loaded.rows), loaded.skipped), file=out)


def cmd_top(args, out, err):
    if args.n < 1:
        raise ValueError("--n must be at least 1")
    rows = load(args, err).rows
    print(format_top(aggregate.top_products(rows, args.n)), file=out)


def cmd_units(args, out, err):
    rows = load(args, err).rows
    print(format_units(aggregate.units_by(rows, by=args.by), args.by), file=out)


COMMANDS = {"summary": cmd_summary, "top": cmd_top, "units": cmd_units}


def main(argv=None, out=None, err=None):
    out = out or sys.stdout
    err = err or sys.stderr
    args = build_parser().parse_args(argv)
    try:
        COMMANDS[args.command](args, out, err)
    except LoadError as exc:
        print(f"salesrep: {exc}", file=err)
        return 1
    except (OSError, ValueError) as exc:
        print(f"salesrep: {exc}", file=err)
        return 1
    return 0
