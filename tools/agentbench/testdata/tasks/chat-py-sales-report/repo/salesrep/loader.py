"""Reading sales exports.

A sales export is a CSV file with a header row. Columns:

    date        ISO date, e.g. 2026-01-31
    region      free text, e.g. North
    product     free text, e.g. Widget
    quantity    whole number of units
    unit_price  amount per unit, e.g. 12.50
    discount    optional percentage, e.g. 10 or 12.5%

Extra columns are ignored. Column names are matched case-insensitively.
"""

import csv
import datetime

from .model import Sale
from .money import MoneyError, parse_money, parse_percent

REQUIRED = ("date", "region", "product", "quantity", "unit_price")


class LoadError(Exception):
    """A file that cannot be read as a sales export."""

    def __init__(self, path, line, message):
        super().__init__(f"{path}:{line}: {message}")
        self.path = path
        self.line = line
        self.message = message


def _normalize_header(fieldnames):
    return [(name or "").strip().lower() for name in fieldnames]


def parse_row(row, line, path=""):
    """Turn one CSV row (a dict keyed by lower-case column) into a Sale."""
    try:
        date = datetime.date.fromisoformat(row["date"].strip())
    except (AttributeError, ValueError):
        raise LoadError(path, line, f"bad date: {row.get('date')!r}") from None
    region = (row.get("region") or "").strip()
    product = (row.get("product") or "").strip()
    if not region or not product:
        raise LoadError(path, line, "region and product are required")
    quantity = int(row["quantity"])
    if quantity < 0:
        raise LoadError(path, line, f"negative quantity: {quantity}")
    try:
        unit_price = parse_money(row.get("unit_price"))
        discount = parse_percent(row.get("discount"))
    except MoneyError as err:
        raise LoadError(path, line, str(err)) from None
    return Sale(
        line=line,
        date=date,
        region=region,
        product=product,
        quantity=quantity,
        unit_price=unit_price,
        discount=discount,
        source=path,
    )


def read_sales(handle, path="<input>"):
    """Read sales from an open text file."""
    reader = csv.DictReader(handle)
    if reader.fieldnames is None:
        return []
    reader.fieldnames = _normalize_header(reader.fieldnames)
    missing = [name for name in REQUIRED if name not in reader.fieldnames]
    if missing:
        raise LoadError(path, 1, "missing columns: " + ", ".join(missing))
    sales = []
    for row in reader:
        line = reader.line_num
        if not any((value or "").strip() for value in row.values() if isinstance(value, str)):
            continue  # blank line
        sales.append(parse_row(row, line, path))
    return sales


def load_csv(path):
    """Load one sales export and return its list of Sale."""
    with open(path, newline="", encoding="utf-8") as handle:
        return read_sales(handle, path)


def load_files(paths):
    """Load several exports, in order, into one list."""
    sales = []
    for path in paths:
        sales.extend(load_csv(path))
    return sales
