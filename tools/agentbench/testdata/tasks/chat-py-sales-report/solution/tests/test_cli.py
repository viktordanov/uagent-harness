import io
import json
import os
import subprocess
import tempfile
import sys
import unittest

from salesrep.cli import main
from tests.helpers import Q1


def run(*argv):
    out, err = io.StringIO(), io.StringIO()
    code = main(list(argv), out=out, err=err)
    return code, out.getvalue(), err.getvalue()


class SummaryTest(unittest.TestCase):
    def test_by_region(self):
        code, out, _ = run("summary", Q1)
        self.assertEqual(code, 0)
        lines = out.splitlines()
        self.assertEqual(lines[0].split(), ["region", "total"])
        self.assertEqual(lines[2].split(), ["East", "122.47"])
        self.assertEqual(lines[3].split(), ["North", "259.20"])
        self.assertEqual(lines[4].split(), ["South", "68.48"])
        self.assertEqual(lines[5].split(), ["TOTAL", "450.15"])
        self.assertIn("14 rows", out)

    def test_by_product_with_filter(self):
        code, out, _ = run("summary", "--by", "product", "--region", "North", Q1)
        self.assertEqual(code, 0)
        self.assertIn("Widget", out)
        self.assertIn("220.00", out)
        self.assertIn("5 rows", out)

    def test_missing_file(self):
        code, _, err = run("summary", "nope.csv")
        self.assertEqual(code, 1)
        self.assertIn("salesrep:", err)


class MonthTest(unittest.TestCase):
    def test_by_month(self):
        code, out, _ = run("summary", "--by", "month", Q1)
        self.assertEqual(code, 0)
        lines = out.splitlines()
        self.assertEqual(lines[0].split(), ["month", "total"])
        self.assertEqual(lines[2].split(), ["2026-01", "199.47"])
        self.assertEqual(lines[3].split(), ["2026-02", "85.83"])
        self.assertEqual(lines[4].split(), ["2026-03", "164.85"])


class JsonTest(unittest.TestCase):
    def test_json_by_region(self):
        code, out, _ = run("summary", "--format", "json", Q1)
        self.assertEqual(code, 0)
        self.assertEqual(
            json.loads(out),
            [
                {"group": "East", "total": "122.47"},
                {"group": "North", "total": "259.20"},
                {"group": "South", "total": "68.48"},
            ],
        )

    def test_json_by_month(self):
        code, out, _ = run("summary", "--by", "month", "--format", "json", Q1)
        self.assertEqual(code, 0)
        self.assertEqual([item["group"] for item in json.loads(out)], ["2026-01", "2026-02", "2026-03"])


class SkippedRowsTest(unittest.TestCase):
    def test_missing_quantity_is_skipped(self):
        text = (
            "date,region,product,quantity,unit_price\n"
            "2026-01-01,North,Widget,2,3.00\n"
            "2026-01-02,North,Widget,,3.00\n"
            "2026-01-03,North,Widget,1,3.00\n"
        )
        with tempfile.TemporaryDirectory() as tmp:
            path = os.path.join(tmp, "s.csv")
            with open(path, "w", encoding="utf-8") as f:
                f.write(text)
            code, out, err = run("summary", path)
        self.assertEqual(code, 0)
        self.assertIn("line 3", err)
        self.assertIn("9.00", out)
        self.assertIn("2 rows, 1 skipped", out)


class TopTest(unittest.TestCase):
    def test_top_two(self):
        code, out, _ = run("top", "--n", "2", Q1)
        self.assertEqual(code, 0)
        lines = out.splitlines()
        self.assertEqual(lines[2].split(), ["1", "Widget", "315.73"])
        self.assertEqual(lines[3].split(), ["2", "Gadget", "76.29"])
        self.assertEqual(len(lines), 4)


class ModuleTest(unittest.TestCase):
    def test_python_m(self):
        proc = subprocess.run(
            [sys.executable, "-m", "salesrep", "units", Q1],
            capture_output=True,
            text=True,
            check=False,
        )
        self.assertEqual(proc.returncode, 0, proc.stderr)
        self.assertIn("Widget", proc.stdout)
        self.assertIn("32", proc.stdout)


if __name__ == "__main__":
    unittest.main()
