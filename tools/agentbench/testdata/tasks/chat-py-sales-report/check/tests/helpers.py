import datetime
import os
from decimal import Decimal

from salesrep.model import Sale

DATA = os.path.join(os.path.dirname(__file__), "data")
Q1 = os.path.join(DATA, "q1.csv")


def sale(region="North", product="Widget", quantity=1, unit_price="1.00", discount="0", date="2026-01-01", line=2):
    return Sale(
        line=line,
        date=datetime.date.fromisoformat(date),
        region=region,
        product=product,
        quantity=quantity,
        unit_price=Decimal(unit_price),
        discount=Decimal(discount),
    )
