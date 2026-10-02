import io
import subprocess
import sys
import unittest

from cfgtool.cli import main


def run(*argv):
    out, err = io.StringIO(), io.StringIO()
    code = main(list(argv), out=out, err=err)
    return code, out.getvalue(), err.getvalue()


class CheckTest(unittest.TestCase):
    def test_good(self):
        code, out, _ = run("check", "tests/data/good.ini", "examples/api.ini")
        self.assertEqual(code, 0)
        self.assertEqual(out.strip(), "2 ok")

    def test_bad(self):
        code, out, _ = run("check", "tests/data/bad.ini")
        self.assertEqual(code, 1)
        self.assertIn("tests/data/bad.ini:8: unknown section [cache]", out)
        self.assertTrue(out.rstrip().endswith("problems"))

    def test_missing_file(self):
        code, _, err = run("check", "nope.ini")
        self.assertEqual(code, 2)
        self.assertIn("cfgtool:", err)


class ShowTest(unittest.TestCase):
    def test_show(self):
        code, out, _ = run("show", "tests/data/good.ini")
        self.assertEqual(code, 0)
        self.assertIn("http.port = 8080", out)
        self.assertIn("http.request_timeout = 0.5s", out)
        self.assertIn("database.read_only = true", out)
        self.assertIn("http.allowed_origins = https://a.example, https://b.example", out)

    def test_show_bad(self):
        code, _, err = run("show", "tests/data/bad.ini")
        self.assertEqual(code, 1)
        self.assertIn("unknown section", err)


class KeysTest(unittest.TestCase):
    def test_keys(self):
        code, out, _ = run("keys")
        self.assertEqual(code, 0)
        self.assertIn("http.port", out)
        self.assertIn("required", out)


class ModuleTest(unittest.TestCase):
    def test_python_m(self):
        proc = subprocess.run(
            [sys.executable, "-m", "cfgtool", "check", "examples/api.ini"],
            capture_output=True,
            text=True,
            check=False,
        )
        self.assertEqual(proc.returncode, 0, proc.stdout + proc.stderr)


if __name__ == "__main__":
    unittest.main()
