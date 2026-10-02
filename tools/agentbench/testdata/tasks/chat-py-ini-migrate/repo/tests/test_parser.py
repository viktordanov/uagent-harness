import unittest

from cfgtool.parser import COMMENT, ENTRY, SECTION, ParseError, parse_file, parse_text

SAMPLE = """; header comment
[service]
name = demo

# another comment
[http]
port=8080
host =  127.0.0.1   # loopback only
"""


class ParserTest(unittest.TestCase):
    def test_kinds_and_numbers(self):
        doc = parse_text(SAMPLE)
        kinds = [(line.number, line.kind) for line in doc.lines]
        self.assertEqual(kinds[0], (1, COMMENT))
        self.assertEqual(kinds[1], (2, SECTION))
        self.assertEqual(kinds[2], (3, ENTRY))
        self.assertEqual(kinds[6], (7, ENTRY))
        self.assertEqual(len(doc.lines), 8)

    def test_values(self):
        doc = parse_text(SAMPLE)
        self.assertEqual(doc.get("service", "name"), "demo")
        self.assertEqual(doc.get("http", "port"), "8080")
        self.assertEqual(doc.get("http", "missing", "x"), "x")

    def test_inline_hash_comment(self):
        doc = parse_text(SAMPLE)
        self.assertEqual(doc.get("http", "host"), "127.0.0.1")

    def test_hash_inside_value(self):
        doc = parse_text("[s]\ncolor = #fff\nurl = http://x/#top\n")
        self.assertEqual(doc.get("s", "color"), "#fff")
        self.assertEqual(doc.get("s", "url"), "http://x/#top")

    def test_round_trip(self):
        doc = parse_text(SAMPLE)
        self.assertEqual(doc.text(), SAMPLE)

    def test_sections_and_dict(self):
        doc = parse_text(SAMPLE)
        self.assertEqual(doc.sections(), ["service", "http"])
        self.assertEqual(doc.section_line("http"), 6)
        self.assertEqual(doc.as_dict()["http"]["port"], "8080")

    def test_key_outside_section(self):
        with self.assertRaises(ParseError) as ctx:
            parse_text("name = x\n", "a.ini")
        self.assertEqual(ctx.exception.line, 1)

    def test_garbage_line(self):
        with self.assertRaises(ParseError) as ctx:
            parse_text("[s]\nthis is not ini\n", "a.ini")
        self.assertIn("a.ini:2", str(ctx.exception))

    def test_parse_file(self):
        doc = parse_file("tests/data/good.ini")
        self.assertEqual(doc.get("database", "read_only"), "true")


if __name__ == "__main__":
    unittest.main()
