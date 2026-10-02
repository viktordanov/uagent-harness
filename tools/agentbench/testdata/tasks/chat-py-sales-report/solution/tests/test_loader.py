import datetime
import io
import unittest
from decimal import Decimal

from salesrep.loader import LoadError, load_csv, load_files, read_sales
from tests.helpers import Q1


class LoaderTest(unittest.TestCase):
    def test_load_q1(self):
        rows = load_csv(Q1)
        self.assertEqual(len(rows), 14)
        first = rows[0]
        self.assertEqual(first.line, 2)
        self.assertEqual(first.date, datetime.date(2026, 1, 5))
        self.assertEqual(first.region, "North")
        self.assertEqual(first.quantity, 10)
        self.assertEqual(first.unit_price, Decimal("12.50"))
        self.assertEqual(first.discount, Decimal("0"))
        self.assertEqual(rows[-1].line, 15)

    def test_load_files_concatenates(self):
        self.assertEqual(len(load_files([Q1, Q1])), 28)

    def test_header_case_and_extra_columns(self):
        text = "Date,Region,Product,Quantity,Unit_Price,Note\n2026-01-01,North,Widget,2,3.00,hi\n"
        rows = read_sales(io.StringIO(text))
        self.assertEqual(len(rows), 1)
        self.assertEqual(rows[0].total, Decimal("6.00"))

    def test_blank_lines_are_ignored(self):
        text = "date,region,product,quantity,unit_price\n\n2026-01-01,North,Widget,2,3.00\n,,,,\n"
        self.assertEqual(len(read_sales(io.StringIO(text))), 1)

    def test_missing_column(self):
        with self.assertRaises(LoadError) as ctx:
            read_sales(io.StringIO("date,region,product\n"), "x.csv")
        self.assertIn("quantity", str(ctx.exception))
        self.assertIn("x.csv:1", str(ctx.exception))

    def test_bad_date_names_line(self):
        text = "date,region,product,quantity,unit_price\n2026-01-01,North,Widget,1,1\nsoon,North,Widget,1,1\n"
        with self.assertRaises(LoadError) as ctx:
            read_sales(io.StringIO(text), "x.csv")
        self.assertEqual(ctx.exception.line, 3)

    def test_empty_quantity_is_skipped(self):
        text = "date,region,product,quantity,unit_price\n2026-01-01,North,Widget,,1\n2026-01-02,North,Widget,1,1\n"
        skipped = []
        rows = read_sales(io.StringIO(text), "x.csv", on_skip=lambda path, line, reason: skipped.append(line))
        self.assertEqual(len(rows), 1)
        self.assertEqual(skipped, [2])

    def test_bad_price(self):
        text = "date,region,product,quantity,unit_price\n2026-01-01,North,Widget,1,cheap\n"
        with self.assertRaises(LoadError):
            read_sales(io.StringIO(text))


if __name__ == "__main__":
    unittest.main()
