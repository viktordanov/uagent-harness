"""Command line: python3 -m salesrep COMMAND [options] FILE...

Commands:
    summary   totals per group (--by region|product)
    top       best-selling products by total (--n N)
    units     units sold per group (--by region|product)
"""

import argparse
import sys

from . import agg, filters
from .formatting import format_summary, format_top, format_units
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
    summary.add_argument("--by", choices=sorted(agg.GROUP_KEYS), default="region")

    top = sub.add_parser("top", help="best-selling products")
    common(top)
    top.add_argument("--n", type=int, default=5, help="how many products (default 5)")

    units = sub.add_parser("units", help="units sold per group")
    common(units)
    units.add_argument("--by", choices=sorted(agg.GROUP_KEYS), default="product")
    return parser


def load(args):
    rows = load_files(args.files)
    return filters.apply(rows, args.region, args.product, args.start, args.end)


def cmd_summary(args, out):
    rows = load(args)
    pairs = agg.sum_rows(rows, by=args.by)
    print(format_summary(pairs, args.by, agg.grand_total(rows), len(rows)), file=out)


def cmd_top(args, out):
    if args.n < 1:
        raise ValueError("--n must be at least 1")
    rows = load(args)
    print(format_top(agg.top_products(rows, args.n)), file=out)


def cmd_units(args, out):
    rows = load(args)
    print(format_units(agg.units_by(rows, by=args.by), args.by), file=out)


COMMANDS = {"summary": cmd_summary, "top": cmd_top, "units": cmd_units}


def main(argv=None, out=None, err=None):
    out = out or sys.stdout
    err = err or sys.stderr
    args = build_parser().parse_args(argv)
    try:
        COMMANDS[args.command](args, out)
    except LoadError as exc:
        print(f"salesrep: {exc}", file=err)
        return 1
    except (OSError, ValueError) as exc:
        print(f"salesrep: {exc}", file=err)
        return 1
    return 0
