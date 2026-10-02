"""Money helpers.

All amounts are Decimal. A line total is rounded to the cent, half up,
before it is added to anything: that is what the finance team's
spreadsheet does, and the reports must match it to the cent.
"""

from decimal import ROUND_DOWN, Decimal, InvalidOperation

CENT = Decimal("0.01")
ZERO = Decimal("0")
HUNDRED = Decimal("100")


class MoneyError(ValueError):
    """A value that is not a valid amount or percentage."""


def parse_money(text):
    """Parse an amount such as "12.5" or "1,204.00" into a Decimal."""
    if text is None:
        raise MoneyError("missing amount")
    cleaned = text.strip().replace(",", "")
    if cleaned.startswith("$"):
        cleaned = cleaned[1:]
    if not cleaned:
        raise MoneyError("empty amount")
    try:
        value = Decimal(cleaned)
    except InvalidOperation:
        raise MoneyError(f"not an amount: {text!r}") from None
    if value < ZERO:
        raise MoneyError(f"negative amount: {text!r}")
    return value


def parse_percent(text):
    """Parse a discount such as "15" or "12.5%" into a Decimal in [0, 100].

    An empty or missing value is no discount.
    """
    if text is None:
        return ZERO
    cleaned = text.strip().rstrip("%").strip()
    if not cleaned:
        return ZERO
    try:
        value = Decimal(cleaned)
    except InvalidOperation:
        raise MoneyError(f"not a percentage: {text!r}") from None
    if value < ZERO or value > HUNDRED:
        raise MoneyError(f"percentage out of range: {text!r}")
    return value


def round_cents(value):
    """Round an amount to the cent."""
    return value.quantize(CENT, rounding=ROUND_DOWN)


def format_money(value):
    """Format an amount with exactly two decimals, e.g. 1204.5 -> "1204.50"."""
    return f"{value.quantize(CENT):.2f}"
