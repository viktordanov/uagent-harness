"""The Sale record and its line total."""

import datetime
from dataclasses import dataclass
from decimal import Decimal

from .money import HUNDRED, round_cents


def line_total(quantity, unit_price, discount):
    """Total of one line: quantity x unit price, less a percentage discount.

    The result is rounded to the cent (see money.round_cents).
    """
    gross = unit_price * Decimal(quantity)
    net = gross * (HUNDRED - discount) / HUNDRED
    return round_cents(net)


@dataclass(frozen=True)
class Sale:
    """One line of a sales export.

    ``line`` is the line number in the source file (the header is line 1),
    kept so errors and warnings can point at it.
    """

    line: int
    date: datetime.date
    region: str
    product: str
    quantity: int
    unit_price: Decimal
    discount: Decimal = Decimal("0")
    source: str = ""

    @property
    def total(self):
        return line_total(self.quantity, self.unit_price, self.discount)

    @property
    def discounted(self):
        return self.discount > 0
