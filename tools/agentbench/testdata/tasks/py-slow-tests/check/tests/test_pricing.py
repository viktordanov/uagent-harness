import unittest

from inventory.pricing import bulk_price


class BulkPriceTest(unittest.TestCase):
    def test_tiers(self):
        self.assertEqual(bulk_price(100, 1), 100)
        self.assertEqual(bulk_price(100, 9), 900)
        self.assertEqual(bulk_price(199, 10), 1890)
        self.assertEqual(bulk_price(100, 49), 4655)
        self.assertEqual(bulk_price(333, 50), 14985)
        self.assertEqual(bulk_price(7, 51), 321)

    def test_bad_qty(self):
        for q in (0, -1):
            with self.assertRaises(ValueError):
                bulk_price(100, q)
