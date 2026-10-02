import unittest
from decimal import Decimal

from salesrep.money import MoneyError, format_money, parse_money, parse_percent, round_cents


class ParseMoneyTest(unittest.TestCase):
    def test_plain(self):
        self.assertEqual(parse_money("12.5"), Decimal("12.5"))

    def test_thousands_and_dollar(self):
        self.assertEqual(parse_money("$1,204.00"), Decimal("1204.00"))

    def test_rejects(self):
        for bad in ["", "  ", "abc", "-3", None]:
            with self.subTest(bad=bad):
                with self.assertRaises(MoneyError):
                    parse_money(bad)


class ParsePercentTest(unittest.TestCase):
    def test_values(self):
        self.assertEqual(parse_percent("15"), Decimal("15"))
        self.assertEqual(parse_percent("12.5%"), Decimal("12.5"))
        self.assertEqual(parse_percent(""), Decimal("0"))
        self.assertEqual(parse_percent(None), Decimal("0"))

    def test_range(self):
        with self.assertRaises(MoneyError):
            parse_percent("101")


class RoundingTest(unittest.TestCase):
    def test_half_up(self):
        self.assertEqual(round_cents(Decimal("5.025")), Decimal("5.03"))
        self.assertEqual(round_cents(Decimal("11.205")), Decimal("11.21"))

    def test_below_half(self):
        self.assertEqual(round_cents(Decimal("7.4925")), Decimal("7.49"))
        self.assertEqual(round_cents(Decimal("17.8245")), Decimal("17.82"))

    def test_format(self):
        self.assertEqual(format_money(Decimal("1204.5")), "1204.50")
        self.assertEqual(format_money(Decimal("0")), "0.00")


if __name__ == "__main__":
    unittest.main()
