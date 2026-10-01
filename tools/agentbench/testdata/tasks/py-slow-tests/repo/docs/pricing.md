# Pricing

`bulk_price(unit_cents, qty)` returns the total in cents for `qty` items:

- 1 to 9 items: no discount.
- 10 to 49 items: 5% off the total.
- 50 items or more: 10% off the total.

Discounts round down to whole cents in the customer's favour, i.e. the total
is rounded down (floor). `qty` below 1 raises `ValueError`.

Example: `bulk_price(199, 10)` is `1890` (1990 minus 5% = 1890.5, floored).
