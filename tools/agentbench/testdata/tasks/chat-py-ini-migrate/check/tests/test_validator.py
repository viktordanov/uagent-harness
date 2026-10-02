import unittest

from cfgtool.parser import parse_file, parse_text
from cfgtool.validator import effective, validate


class ValidatorTest(unittest.TestCase):
    def test_good(self):
        self.assertEqual(validate(parse_file("tests/data/good.ini")), [])

    def test_bad(self):
        problems = [str(p) for p in validate(parse_file("tests/data/bad.ini"))]
        self.assertIn("tests/data/bad.ini:1: missing section [database]", problems)
        self.assertIn("tests/data/bad.ini:1: missing required key 'name' in [service]", problems)
        self.assertTrue(any(p.startswith("tests/data/bad.ini:2: service.environment") for p in problems))
        self.assertTrue(any(p.startswith("tests/data/bad.ini:5: http.port: port out of range") for p in problems))
        self.assertTrue(any(p.startswith("tests/data/bad.ini:6: http.tls") for p in problems))
        self.assertIn("tests/data/bad.ini:8: unknown section [cache]", problems)

    def test_sorted_by_line(self):
        lines = [p.line for p in validate(parse_file("tests/data/bad.ini"))]
        self.assertEqual(lines, sorted(lines))

    def test_duplicate_key(self):
        doc = parse_text("[service]\nname = a\nname = b\n[http]\nport = 1\n[database]\nurl = x\n", "d.ini")
        self.assertEqual([str(p) for p in validate(doc)], ["d.ini:3: duplicate key 'name' in [service] (first on line 2)"])

    def test_unknown_keys_are_not_problems(self):
        doc = parse_text("[service]\nname = a\ncolour = red\n[http]\nport = 1\n[database]\nurl = x\n")
        self.assertEqual(validate(doc), [])

    def test_effective_fills_defaults(self):
        config = effective(parse_file("tests/data/good.ini"))
        self.assertEqual(config["service"]["workers"], 4)
        self.assertEqual(config["http"]["request_timeout"], 0.5)
        self.assertEqual(config["http"]["host"], "0.0.0.0")
        self.assertEqual(config["database"]["pool_size"], 10)
        self.assertIs(config["database"]["read_only"], True)
        self.assertEqual(config["logging"]["level"], "info")


if __name__ == "__main__":
    unittest.main()
