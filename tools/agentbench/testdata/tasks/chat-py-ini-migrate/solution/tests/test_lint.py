import io
import os
import tempfile
import unittest

from cfgtool import schema
from cfgtool.cli import main
from cfgtool.lint import lint
from cfgtool.parser import parse_text

TEXT = """[service]
name = a
colour = red

[http]
port = 1
timeout = 30s

[cache]
size = 1
"""


class LintTest(unittest.TestCase):
    def test_findings(self):
        findings = [str(f) for f in lint(parse_text(TEXT, "x.ini"))]
        self.assertEqual(
            findings,
            [
                "x.ini:3: unknown key 'colour' in [service]",
                "x.ini:7: warning: 'timeout' in [http] is deprecated, use 'request_timeout'",
            ],
        )

    def test_deprecated_mapping(self):
        self.assertEqual(schema.DEPRECATED[("database", "pool")], "pool_size")

    def test_exit_status(self):
        with tempfile.TemporaryDirectory() as tmp:
            bad = os.path.join(tmp, "bad.ini")
            old = os.path.join(tmp, "old.ini")
            with open(bad, "w") as f:
                f.write(TEXT)
            with open(old, "w") as f:
                f.write("[logging]\nfile = x.log\n")
            out = io.StringIO()
            self.assertEqual(main(["lint", bad], out=out, err=io.StringIO()), 1)
            out = io.StringIO()
            self.assertEqual(main(["lint", old], out=out, err=io.StringIO()), 0)
            self.assertIn("use 'path'", out.getvalue())
            self.assertEqual(main(["lint", "tests/data/good.ini"], out=io.StringIO(), err=io.StringIO()), 0)


if __name__ == "__main__":
    unittest.main()
