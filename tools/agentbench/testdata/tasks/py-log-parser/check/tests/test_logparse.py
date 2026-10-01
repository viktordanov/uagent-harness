import unittest

from logparse import parse_lines, summarize
from logparse.durations import format_duration, parse_duration


class DurationTest(unittest.TestCase):
    def test_short(self):
        self.assertEqual(parse_duration("42"), 42)
        self.assertEqual(parse_duration("12:30"), 750)

    def test_format(self):
        self.assertEqual(format_duration(750), "0:12:30")


class ReportTest(unittest.TestCase):
    def test_skips_failed(self):
        recs = parse_lines(["2026-09-01 02:00:01 backup OK 1:00", "2026-09-01 04:00:00 backup FAILED 0:03"])
        self.assertEqual(summarize(recs), "backup       0:01:00")


if __name__ == "__main__":
    unittest.main()
