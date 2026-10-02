"""cfgtool: read and validate INI-style service configs.

parser.py turns a file into a Document that keeps every line as written,
schema.py says which sections and keys exist and what their values are,
values.py converts values, validator.py checks a Document against the
schema, and cli.py is ``python3 -m cfgtool``.
"""

__version__ = "1.3.0"
