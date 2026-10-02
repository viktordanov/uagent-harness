import datetime
import unittest

from salesrep import filters
from salesrep.loader import load_csv
from tests.helpers import Q1


class FiltersTest(unittest.TestCase):
    def setUp(self):
        self.rows = load_csv(Q1)

    def test_region_is_case_insensitive(self):
        self.assertEqual(len(filters.by_region(self.rows, "north")), 5)

    def test_product(self):
        self.assertEqual(len(filters.by_product(self.rows, "Gizmo")), 4)

    def test_between_inclusive(self):
        rows = filters.between(self.rows, datetime.date(2026, 2, 2), datetime.date(2026, 2, 23))
        self.assertEqual(len(rows), 4)

    def test_apply_combines(self):
        rows = filters.apply(self.rows, region="South", start=datetime.date(2026, 2, 1))
        self.assertEqual([r.product for r in rows], ["Gizmo", "Gadget", "Widget"])

    def test_parse_day(self):
        self.assertEqual(filters.parse_day("2026-03-01"), datetime.date(2026, 3, 1))
        with self.assertRaises(ValueError):
            filters.parse_day("March")


if __name__ == "__main__":
    unittest.main()
