"""Prices are integer cents."""


def line_total(unit_cents, qty):
    return unit_cents * qty


def bulk_price(unit_cents, qty):
    if qty < 1:
        raise ValueError("qty must be at least 1")
    total = unit_cents * qty
    if qty >= 50:
        return total * 90 // 100
    if qty >= 10:
        return total * 95 // 100
    return total
