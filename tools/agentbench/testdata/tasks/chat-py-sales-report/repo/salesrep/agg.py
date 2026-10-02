"""Grouping and summing sales."""

from .money import ZERO

GROUP_KEYS = {
    "region": lambda sale: sale.region,
    "product": lambda sale: sale.product,
}


def group_key(by):
    """Return the function that gives a sale's group for ``by``."""
    try:
        return GROUP_KEYS[by]
    except KeyError:
        raise ValueError(f"cannot group by {by!r}") from None


def sum_rows(rows, by="region"):
    """Sum line totals per group.

    Returns a list of (group, total) pairs sorted by group name.
    """
    key = group_key(by)
    totals = {}
    for row in rows:
        group = key(row)
        totals[group] = totals.get(group, ZERO) + row.total
    return sorted(totals.items())


def grand_total(rows):
    """Sum of every line total."""
    total = ZERO
    for row in rows:
        total += row.total
    return total


def units_by(rows, by="product"):
    """Sum quantities per group, as (group, units) pairs sorted by group."""
    key = group_key(by)
    units = {}
    for row in rows:
        group = key(row)
        units[group] = units.get(group, 0) + row.quantity
    return sorted(units.items())


def top_products(rows, n=5):
    """The ``n`` products with the highest totals.

    Sorted by total, highest first; products with equal totals are listed
    by name.
    """
    pairs = sum_rows(rows, by="product")
    pairs.sort(key=lambda pair: (-pair[1], pair[0]))
    return pairs[:n]
