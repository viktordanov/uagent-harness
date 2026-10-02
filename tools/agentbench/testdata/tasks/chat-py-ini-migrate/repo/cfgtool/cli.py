"""python3 -m cfgtool COMMAND ...

Commands:
    check FILE...   validate configs; exit 1 when any has a problem
    show FILE       print the effective config, defaults filled in
    keys            list every key the schema knows
"""

import argparse
import sys

from . import schema
from .parser import ParseError, parse_file
from .report import format_effective, format_keys, format_problems
from .validator import Problem, effective, validate

EXIT_OK = 0
EXIT_PROBLEMS = 1
EXIT_USAGE = 2


def build_parser():
    parser = argparse.ArgumentParser(prog="cfgtool", description="Validate INI-style service configs.")
    sub = parser.add_subparsers(dest="command", required=True)
    check = sub.add_parser("check", help="validate configs")
    check.add_argument("files", nargs="+", metavar="FILE")
    show = sub.add_parser("show", help="print the effective config")
    show.add_argument("file", metavar="FILE")
    sub.add_parser("keys", help="list known keys")
    return parser


def load(path):
    """Parse ``path``; a parse error becomes a list of one Problem."""
    try:
        return parse_file(path), []
    except ParseError as err:
        return None, [Problem(err.path, err.line, err.message)]


def cmd_check(args, out, err):
    problems = []
    for path in args.files:
        doc, parse_problems = load(path)
        problems.extend(parse_problems)
        if doc is not None:
            problems.extend(validate(doc))
    if problems:
        print(format_problems(problems), file=out)
        return EXIT_PROBLEMS
    print(f"{len(args.files)} ok", file=out)
    return EXIT_OK


def cmd_show(args, out, err):
    doc, problems = load(args.file)
    if doc is not None:
        problems = validate(doc)
    if problems:
        print(format_problems(problems), file=err)
        return EXIT_PROBLEMS
    print(format_effective(effective(doc)), file=out)
    return EXIT_OK


def cmd_keys(args, out, err):
    print(format_keys(schema.all_keys()), file=out)
    return EXIT_OK


COMMANDS = {"check": cmd_check, "show": cmd_show, "keys": cmd_keys}


def main(argv=None, out=None, err=None):
    out = out or sys.stdout
    err = err or sys.stderr
    args = build_parser().parse_args(argv)
    try:
        return COMMANDS[args.command](args, out, err)
    except OSError as exc:
        print(f"cfgtool: {exc}", file=err)
        return EXIT_USAGE
