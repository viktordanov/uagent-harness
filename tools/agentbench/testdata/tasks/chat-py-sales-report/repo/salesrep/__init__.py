"""salesrep: reports over CSV sales exports.

The package reads one or more CSV files (see loader.py for the columns),
turns each line into a Sale (model.py), groups and sums them (agg.py),
and prints plain-text tables (formatting.py). cli.py wires it together
behind ``python3 -m salesrep``.
"""

__version__ = "0.4.1"
