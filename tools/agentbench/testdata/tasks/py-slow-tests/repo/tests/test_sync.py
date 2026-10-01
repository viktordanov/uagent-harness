import unittest

from inventory.sync import fresh


class SyncTest(unittest.TestCase):
    def test_sync_small(self):
        s = fresh([("a", 1)] * 10)
        self.assertEqual(s.quantity("a"), 10)

    def test_sync_mixed(self):
        s = fresh([("a", 2), ("b", 3)] * 15)
        self.assertEqual(s.quantity("a"), 30)
        self.assertEqual(s.quantity("b"), 45)
