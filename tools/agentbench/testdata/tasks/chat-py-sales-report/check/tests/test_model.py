import unittest
from decimal import Decimal

from salesrep.model import line_total
from tests.helpers import sale


class LineTotalTest(unittest.TestCase):
    def test_no_discount(self):
        self.assertEqual(line_total(10, Decimal("12.50"), Decimal("0")), Decimal("125.00"))

    def test_discount_rounds_half_up(self):
        self.assertEqual(line_total(1, Decimal("10.05"), Decimal("50")), Decimal("5.03"))
        self.assertEqual(line_total(5, Decimal("2.99"), Decimal("10")), Decimal("13.46"))

    def test_sale_total(self):
        s = sale(quantity=4, unit_price="4.15", discount="12.5")
        self.assertEqual(s.total, Decimal("14.53"))
        self.assertTrue(s.discounted)
        self.assertFalse(sale().discounted)


if __name__ == "__main__":
    unittest.main()
