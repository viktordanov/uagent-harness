import io
import subprocess
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
