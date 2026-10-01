class OutOfStock(Exception):
    """Raised when removing more items than are in stock."""


class Stock:
    def __init__(self):
        self._qty = {}

    def add(self, sku, n):
        if n <= 0:
            raise ValueError("n must be positive")
        self._qty[sku] = self._qty.get(sku, 0) + n

    def remove(self, sku, n):
        if n <= 0:
            raise ValueError("n must be positive")
        have = self._qty.get(sku, 0)
        if n > have:
            raise OutOfStock(f"{sku}: have {have}, want {n}")
        self._qty[sku] = have - n

    def quantity(self, sku):
        return self._qty.get(sku, 0)
