import unittest

from logparse import parse_lines, summarize
from logparse.durations import parse_duration


class HoursTest(unittest.TestCase):
    def test_hours(self):
        self.assertEqual(parse_duration("1:05:00"), 3900)
        self.assertEqual(parse_duration("2:00:07"), 7207)
        self.assertEqual(parse_duration("10:00:00"), 36000)

    def test_report(self):
        with open("sample.log", encoding="utf-8") as f:
            out = summarize(parse_lines(f))
        self.assertIn("reindex      3:05:07", out)
        self.assertIn("backup       0:24:29", out)


if __name__ == "__main__":
    unittest.main()
