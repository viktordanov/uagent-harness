"""Migrate: rename deprecated keys in place.

Only the key name changes; the value, spacing, inline comment, and every
other line stay exactly as they were.
"""

import difflib

from . import schema
from .parser import rename_key


def migrate(doc):
    """Rename deprecated keys in ``doc``; return the number renamed."""
    changed = 0
    for line in doc.entries():
        new = schema.replacement_for(line.section, line.key)
        if new is not None:
            rename_key(line, new)
            changed += 1
    return changed


def diff(path, before, after):
    """A unified diff from ``before`` to ``after`` (text), headed by ``path``."""
    return "".join(
        difflib.unified_diff(
            before.splitlines(keepends=True),
            after.splitlines(keepends=True),
            fromfile=f"a/{path}",
            tofile=f"b/{path}",
        )
    )
