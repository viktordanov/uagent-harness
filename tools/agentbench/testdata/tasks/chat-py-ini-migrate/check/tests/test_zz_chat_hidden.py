import os
import re
import subprocess
import sys
import tempfile
import unittest

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))


def cfgtool(*argv):
    proc = subprocess.run(
        [sys.executable, "-m", "cfgtool", *argv],
        capture_output=True,
        text=True,
        cwd=ROOT,
        check=False,
        timeout=60,
    )
    return proc.returncode, proc.stdout, proc.stderr


class TempFiles:
    def __init__(self, **files):
        self.files = files

    def __enter__(self):
        self.dir = tempfile.TemporaryDirectory()
        paths = {}
        for name, text in self.files.items():
            path = os.path.join(self.dir.name, name + ".ini")
            with open(path, "w", encoding="utf-8") as f:
                f.write(text)
            paths[name] = path
        return paths

    def __exit__(self, *exc):
        self.dir.cleanup()


def read(path):
    with open(path, encoding="utf-8") as f:
        return f.read()


def line_of(output, path, number):
    """The output lines that point at path:number."""
    prefix = f"{path}:{number}:"
    return [line for line in output.splitlines() if prefix in line]


VALID = """[service]
name = a

[http]
port = 8080

[database]
url = postgres://x/y
"""

UNKNOWN = """[service]
name = a
colour = red

[http]
port = 8080
request_timeout = 1s
gzip = on

[database]
url = postgres://x/y
"""

DEPRECATED_ONLY = """[service]
name = a

[http]
port = 8080
timeout = 30s

[database]
url = postgres://x/y
pool = 3

[logging]
file = /tmp/x.log
"""

LEGACY = """; legacy config
[service]
name = billing
timeout = 9s

[http]
port = 9100 ; public port
timeout = 45s            ; slow webhooks
keepalive = on

# database
[database]
url = postgres://b@db/b
pool=5
read_only = no

[logging]
level = info
file = /var/log/b.log # main log
"""

MIGRATED = """; legacy config
[service]
name = billing
timeout = 9s

[http]
port = 9100 ; public port
request_timeout = 45s            ; slow webhooks
keepalive = on

# database
[database]
url = postgres://b@db/b
pool_size=5
read_only = no

[logging]
level = info
path = /var/log/b.log # main log
"""


class LintHidden(unittest.TestCase):
    def test_unknown_keys_fail(self):
        with TempFiles(c=UNKNOWN) as p:
            code, out, err = cfgtool("lint", p["c"])
        both = out + err
        self.assertEqual(code, 1, both)
        self.assertTrue(any("colour" in l and "service" in l for l in line_of(both, p["c"], 3)), both)
        self.assertTrue(any("gzip" in l and "http" in l for l in line_of(both, p["c"], 8)), both)
        self.assertFalse(line_of(both, p["c"], 7), both)

    def test_clean_passes(self):
        with TempFiles(c=VALID) as p:
            code, out, err = cfgtool("lint", p["c"])
        self.assertEqual(code, 0, out + err)
        self.assertNotIn("unknown", (out + err).lower())

    def test_several_files(self):
        with TempFiles(a=VALID, b=UNKNOWN) as p:
            code, out, err = cfgtool("lint", p["a"], p["b"])
        self.assertEqual(code, 1)
        self.assertTrue(line_of(out + err, p["b"], 3))


class DeprecatedHidden(unittest.TestCase):
    def test_mapping(self):
        from cfgtool import schema

        self.assertEqual(schema.DEPRECATED[("http", "timeout")], "request_timeout")
        self.assertEqual(schema.DEPRECATED[("database", "pool")], "pool_size")
        self.assertEqual(schema.DEPRECATED[("logging", "file")], "path")

    def test_warns_without_failing(self):
        with TempFiles(c=DEPRECATED_ONLY) as p:
            code, out, err = cfgtool("lint", p["c"])
        both = out + err
        self.assertEqual(code, 0, both)
        for number, old, new in ((6, "timeout", "request_timeout"), (10, "pool", "pool_size"), (13, "file", "path")):
            lines = line_of(both, p["c"], number)
            self.assertTrue(lines, both)
            text = " ".join(lines)
            self.assertIn("deprecat", text.lower())
            self.assertIn(old, text)
            self.assertRegex(text, rf"\b{new}\b")
            self.assertNotIn("unknown", text.lower())

    def test_deprecated_and_unknown(self):
        with TempFiles(c=LEGACY) as p:
            code, out, err = cfgtool("lint", p["c"])
        both = out + err
        self.assertEqual(code, 1, both)
        self.assertIn("keepalive", " ".join(line_of(both, p["c"], 9)))
        self.assertIn("request_timeout", " ".join(line_of(both, p["c"], 8)))


class MigrateHidden(unittest.TestCase):
    def test_in_place(self):
        with TempFiles(c=LEGACY, d=VALID) as p:
            code, out, err = cfgtool("migrate", p["c"], p["d"])
            self.assertEqual(code, 0, out + err)
            self.assertEqual(read(p["c"]), MIGRATED)
            self.assertEqual(read(p["d"]), VALID)
            code, out, err = cfgtool("lint", p["c"])
        self.assertNotIn("deprecat", (out + err).lower())

    def test_idempotent(self):
        with TempFiles(c=MIGRATED) as p:
            code, out, err = cfgtool("migrate", p["c"])
            self.assertEqual(code, 0, out + err)
            self.assertEqual(read(p["c"]), MIGRATED)

    def test_dry_run(self):
        with TempFiles(c=LEGACY) as p:
            code, out, err = cfgtool("migrate", "--dry-run", p["c"])
            self.assertEqual(code, 0, out + err)
            self.assertEqual(read(p["c"]), LEGACY)
        lines = out.splitlines()
        self.assertTrue(any(l.startswith("---") for l in lines), out)
        self.assertTrue(any(l.startswith("+++") for l in lines), out)
        self.assertTrue(any(l.startswith("@@") for l in lines), out)
        self.assertIn("-timeout = 45s            ; slow webhooks", lines)
        self.assertIn("+request_timeout = 45s            ; slow webhooks", lines)
        self.assertIn("-pool=5", lines)
        self.assertIn("+pool_size=5", lines)
        self.assertIn("+path = /var/log/b.log # main log", lines)
        self.assertFalse(any(l.startswith("+") and not l.startswith("+++") and "9s" in l for l in lines), out)


class InlineSemicolonHidden(unittest.TestCase):
    def test_parser(self):
        from cfgtool.parser import parse_text

        doc = parse_text("[http]\nport = 8080 ; public port\nhost = a;b\nrequest_timeout = 5s\t; tab\n")
        self.assertEqual(doc.get("http", "port"), "8080")
        self.assertEqual(doc.get("http", "host"), "a;b")
        self.assertEqual(doc.get("http", "request_timeout"), "5s")

    def test_check_and_show(self):
        text = VALID.replace("port = 8080", "port = 8080 ; public port").replace(
            "name = a", "name = a ; the name"
        )
        with TempFiles(c=text) as p:
            code, out, err = cfgtool("check", p["c"])
            self.assertEqual(code, 0, out + err)
            code, out, err = cfgtool("show", p["c"])
        self.assertEqual(code, 0, out + err)
        self.assertIn("http.port = 8080\n", out)
        self.assertIn("service.name = a\n", out)


if __name__ == "__main__":
    unittest.main()
