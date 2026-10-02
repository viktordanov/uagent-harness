import unittest
from decimal import Decimal

from salesrep.aggregate import grand_total, top_products, total_by, units_by
from salesrep.loader import load_csv
from tests.helpers import Q1, sale


class AggTest(unittest.TestCase):
    def setUp(self):
        self.rows = load_csv(Q1)

    def test_sum_by_region(self):
        self.assertEqual(
            total_by(self.rows, "region"),
            [("East", Decimal("122.47")), ("North", Decimal("259.20")), ("South", Decimal("68.48"))],
        )

    def test_sum_by_product(self):
        self.assertEqual(
            total_by(self.rows, by="product"),
            [("Gadget", Decimal("76.29")), ("Gizmo", Decimal("58.13")), ("Widget", Decimal("315.73"))],
        )

    def test_grand_total(self):
        self.assertEqual(grand_total(self.rows), Decimal("450.15"))

    def test_unknown_group(self):
        with self.assertRaises(ValueError):
            total_by(self.rows, by="colour")

    def test_units(self):
        self.assertEqual(units_by(self.rows), [("Gadget", 11), ("Gizmo", 14), ("Widget", 32)])

    def test_top_ties_by_name(self):
        rows = [
            sale(product="B", unit_price="5.00"),
            sale(product="A", unit_price="5.00"),
            sale(product="C", unit_price="9.00"),
        ]
        self.assertEqual([name for name, _ in top_products(rows, 3)], ["C", "A", "B"])
        self.assertEqual(len(top_products(rows, 1)), 1)


if __name__ == "__main__":
    unittest.main()
