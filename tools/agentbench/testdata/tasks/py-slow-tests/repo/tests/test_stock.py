import unittest

from inventory.stock import Stock


class StockTest(unittest.TestCase):
    def test_add_remove(self):
        s = Stock()
        s.add("a", 3)
        s.remove("a", 2)
        self.assertEqual(s.quantity("a"), 1)
