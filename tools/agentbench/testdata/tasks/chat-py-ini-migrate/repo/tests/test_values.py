import unittest

from cfgtool.schema import Field
from cfgtool.values import ValueError_, convert, parse_bool, parse_duration, parse_list, parse_port


class ValuesTest(unittest.TestCase):
    def test_bool(self):
        for text in ["true", "Yes", "on", "1"]:
            self.assertIs(parse_bool(text), True)
        for text in ["false", "NO", "off", "0"]:
            self.assertIs(parse_bool(text), False)
        with self.assertRaises(ValueError_):
            parse_bool("maybe")

    def test_duration(self):
        self.assertEqual(parse_duration("250ms"), 0.25)
        self.assertEqual(parse_duration("30s"), 30)
        self.assertEqual(parse_duration("5m"), 300)
        self.assertEqual(parse_duration("1.5h"), 5400)
        with self.assertRaises(ValueError_):
            parse_duration("30")

    def test_port(self):
        self.assertEqual(parse_port("8080"), 8080)
        for bad in ["0", "70000", "http"]:
            with self.assertRaises(ValueError_):
                parse_port(bad)

    def test_list(self):
        self.assertEqual(parse_list("a, b,,c "), ["a", "b", "c"])
        self.assertEqual(parse_list(""), [])

    def test_enum_via_convert(self):
        field = Field("enum", choices=("a", "b"))
        self.assertEqual(convert("a", field), "a")
        with self.assertRaises(ValueError_):
            convert("c", field)


if __name__ == "__main__":
    unittest.main()
