import unittest

from inventory.stock import OutOfStock, Stock


class StockTest(unittest.TestCase):
    def test_add_remove(self):
        s = Stock()
        s.add("a", 3)
        s.remove("a", 2)
        self.assertEqual(s.quantity("a"), 1)

    def test_out_of_stock(self):
        s = Stock()
        s.add("a", 3)
        with self.assertRaises(OutOfStock):
            s.remove("a", 4)
        self.assertEqual(s.quantity("a"), 3)
        with self.assertRaises(OutOfStock):
            s.remove("missing", 1)
        self.assertEqual(s.quantity("missing"), 0)
        s.remove("a", 3)
        self.assertEqual(s.quantity("a"), 0)
