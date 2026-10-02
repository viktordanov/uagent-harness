"""Row filters used by the CLI options --region, --product, --from, --to."""

import datetime


def parse_day(text):
    """Parse a --from/--to value (YYYY-MM-DD)."""
    try:
        return datetime.date.fromisoformat(text)
    except ValueError:
        raise ValueError(f"not a date (YYYY-MM-DD): {text!r}") from None


def by_region(rows, region):
    want = region.strip().lower()
    return [row for row in rows if row.region.lower() == want]


def by_product(rows, product):
    want = product.strip().lower()
    return [row for row in rows if row.product.lower() == want]


def between(rows, start=None, end=None):
    """Keep rows dated from ``start`` to ``end``, both inclusive; None is open."""
    kept = []
    for row in rows:
        if start is not None and row.date < start:
            continue
        if end is not None and row.date > end:
            continue
        kept.append(row)
    return kept


def apply(rows, region=None, product=None, start=None, end=None):
    """Apply every filter that is set."""
    if region:
        rows = by_region(rows, region)
    if product:
        rows = by_product(rows, product)
    return between(rows, start, end)
