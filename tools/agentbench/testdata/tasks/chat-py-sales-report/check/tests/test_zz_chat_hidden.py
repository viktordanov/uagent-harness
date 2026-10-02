import json
import os
import re
import subprocess
import sys
import tempfile
import unittest
from decimal import Decimal

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
Q1 = os.path.join(ROOT, "tests", "data", "q1.csv")

HEADER = "date,region,product,quantity,unit_price,discount\n"


def salesrep(*argv):
    proc = subprocess.run(
        [sys.executable, "-m", "salesrep", *argv],
        capture_output=True,
        text=True,
        cwd=ROOT,
        check=False,
        timeout=60,
    )
    return proc.returncode, proc.stdout, proc.stderr


def table_rows(out):
    """Data lines of a text table: (first cell, last cell)."""
    rows = []
    for line in out.splitlines():
        parts = line.split()
        if len(parts) >= 2 and not set(line.strip()) <= set("- "):
            rows.append((parts[0], parts[-1]))
    return rows


class Temp:
    def __init__(self, text):
        self.text = text

    def __enter__(self):
        self.dir = tempfile.TemporaryDirectory()
        path = os.path.join(self.dir.name, "sales.csv")
        with open(path, "w", encoding="utf-8") as f:
            f.write(self.text)
        return path

    def __exit__(self, *exc):
        self.dir.cleanup()


class MonthHidden(unittest.TestCase):
    def test_q1_by_month(self):
        code, out, err = salesrep("summary", "--by", "month", Q1)
        self.assertEqual(code, 0, err)
        rows = dict(table_rows(out))
        self.assertEqual(rows.get("2026-01"), "199.47", out)
        self.assertEqual(rows.get("2026-02"), "85.83", out)
        self.assertEqual(rows.get("2026-03"), "164.85", out)
        self.assertEqual(rows.get("TOTAL"), "450.15", out)

    def test_months_sorted_across_years(self):
        text = HEADER + (
            "2026-02-03,North,Widget,1,10.00,\n"
            "2025-12-30,North,Widget,2,10.00,\n"
            "2026-01-15,South,Widget,3,10.00,\n"
            "2025-12-01,South,Widget,1,1.00,\n"
            "2026-01-31,South,Widget,1,0.50,\n"
        )
        with Temp(text) as path:
            code, out, err = salesrep("summary", "--by", "month", path)
        self.assertEqual(code, 0, err)
        months = [first for first, _ in table_rows(out) if re.fullmatch(r"\d{4}-\d{2}", first)]
        self.assertEqual(months, ["2025-12", "2026-01", "2026-02"], out)
        rows = dict(table_rows(out))
        self.assertEqual(rows["2025-12"], "21.00")
        self.assertEqual(rows["2026-01"], "30.50")


class JsonHidden(unittest.TestCase):
    def test_region_json(self):
        code, out, err = salesrep("summary", "--format", "json", Q1)
        self.assertEqual(code, 0, err)
        self.assertEqual(
            json.loads(out),
            [
                {"group": "East", "total": "122.47"},
                {"group": "North", "total": "259.20"},
                {"group": "South", "total": "68.48"},
            ],
        )

    def test_product_json_with_filter(self):
        code, out, err = salesrep("summary", "--by", "product", "--region", "North", "--format", "json", Q1)
        self.assertEqual(code, 0, err)
        data = json.loads(out)
        self.assertEqual([d["group"] for d in data], ["Gadget", "Gizmo", "Widget"])
        self.assertEqual(data[2], {"group": "Widget", "total": "220.00"})

    def test_month_json(self):
        code, out, err = salesrep("summary", "--by", "month", "--format", "json", Q1)
        self.assertEqual(code, 0, err)
        self.assertEqual(
            json.loads(out),
            [
                {"group": "2026-01", "total": "199.47"},
                {"group": "2026-02", "total": "85.83"},
                {"group": "2026-03", "total": "164.85"},
            ],
        )

    def test_text_is_default(self):
        code, out, err = salesrep("summary", Q1)
        self.assertEqual(code, 0, err)
        self.assertIn("TOTAL", out)


SKIPPY = HEADER + (
    "2026-01-01,North,Widget,2,3.00,\n"
    "2026-01-02,North,Widget,,3.00,\n"
    "2026-01-03,South,Widget,1,3.00,\n"
    "2026-01-04,South,Widget,  ,3.00,\n"
    "2026-01-05,South,Gadget\n"
    "2026-02-06,East,Gadget,4,2.50,10\n"
)


class SkippedHidden(unittest.TestCase):
    def test_text_summary_skips_and_counts(self):
        with Temp(SKIPPY) as path:
            code, out, err = salesrep("summary", path)
        self.assertEqual(code, 0, err)
        for line in (3, 5, 6):
            self.assertRegex(err, rf"\bline {line}\b")
        self.assertNotRegex(err, r"\bline (2|4|7)\b")
        self.assertNotIn("Traceback", err)
        rows = dict(table_rows(out))
        self.assertEqual(rows.get("North"), "6.00", out)
        self.assertEqual(rows.get("South"), "3.00", out)
        self.assertEqual(rows.get("East"), "9.00", out)
        self.assertEqual(rows.get("TOTAL"), "18.00", out)
        self.assertRegex(out, r"\b3 rows\b")
        self.assertRegex(out, r"\b3 skipped\b")

    def test_json_stays_clean(self):
        with Temp(SKIPPY) as path:
            code, out, err = salesrep("summary", "--format", "json", "--by", "month", path)
        self.assertEqual(code, 0, err)
        self.assertEqual(
            json.loads(out),
            [{"group": "2026-01", "total": "9.00"}, {"group": "2026-02", "total": "9.00"}],
        )
        self.assertRegex(err, r"\bline 3\b")

    def test_load_csv_still_returns_list(self):
        from salesrep.loader import load_csv

        with Temp(SKIPPY) as path:
            rows = load_csv(path)
        self.assertIsInstance(rows, list)
        self.assertEqual(len(rows), 3)


class RenameHidden(unittest.TestCase):
    def test_new_names(self):
        from salesrep.aggregate import total_by
        from salesrep.loader import load_csv

        self.assertEqual(
            total_by(load_csv(Q1), "region"),
            [("East", Decimal("122.47")), ("North", Decimal("259.20")), ("South", Decimal("68.48"))],
        )

    def test_old_names_gone(self):
        self.assertFalse(os.path.exists(os.path.join(ROOT, "salesrep", "agg.py")))
        with self.assertRaises(ImportError):
            __import__("salesrep.agg")
        for name in os.listdir(os.path.join(ROOT, "salesrep")):
            if name.endswith(".py"):
                with open(os.path.join(ROOT, "salesrep", name), encoding="utf-8") as f:
                    src = f.read()
                self.assertNotIn("sum_rows", src, name)
                self.assertNotRegex(src, r"\bagg\.(?!py)", name)
                self.assertNotRegex(src, r"import agg\b", name)


if __name__ == "__main__":
    unittest.main()
