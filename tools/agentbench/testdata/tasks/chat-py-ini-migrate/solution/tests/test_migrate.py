import io
import os
import tempfile
import unittest

from cfgtool.cli import main

BEFORE = """; legacy
[http]
port = 9100
timeout = 45s   ; webhooks are slow
[database]
url = x
pool=5
"""

AFTER = """; legacy
[http]
port = 9100
request_timeout = 45s   ; webhooks are slow
[database]
url = x
pool_size=5
"""


class MigrateTest(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.path = os.path.join(self.tmp.name, "c.ini")
        with open(self.path, "w") as f:
            f.write(BEFORE)

    def tearDown(self):
        self.tmp.cleanup()

    def read(self):
        with open(self.path) as f:
            return f.read()

    def test_in_place(self):
        out = io.StringIO()
        self.assertEqual(main(["migrate", self.path], out=out, err=io.StringIO()), 0)
        self.assertEqual(self.read(), AFTER)
        self.assertIn("2 keys migrated", out.getvalue())

    def test_dry_run(self):
        out = io.StringIO()
        self.assertEqual(main(["migrate", "--dry-run", self.path], out=out, err=io.StringIO()), 0)
        self.assertEqual(self.read(), BEFORE)
        text = out.getvalue()
        self.assertIn("-timeout = 45s   ; webhooks are slow\n", text)
        self.assertIn("+request_timeout = 45s   ; webhooks are slow\n", text)
        self.assertIn("+pool_size=5\n", text)


if __name__ == "__main__":
    unittest.main()
