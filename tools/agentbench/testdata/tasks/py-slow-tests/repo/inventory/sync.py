"""Pretend synchronisation with a warehouse system (slow on purpose)."""

import time

from .stock import Stock


def sync(stock, feed, delay=0.5):
    for sku, n in feed:
        time.sleep(delay)
        stock.add(sku, n)
    return stock


def fresh(feed, delay=0.5):
    return sync(Stock(), feed, delay)
