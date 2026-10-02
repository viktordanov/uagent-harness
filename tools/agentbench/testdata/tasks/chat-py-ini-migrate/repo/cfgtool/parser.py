"""Parsing INI-style configs.

The format:

    ; a comment line (# works too)
    [section]
    key = value
    other_key = value ; an inline comment

Section and key names are lower-case letters, digits, ``_`` and ``-``.
Whitespace around names and values is ignored. An inline comment starts
at a ``;`` or ``#`` that follows whitespace, so ``a;b`` is a plain value.

A Document keeps every line of the file, comments and blank lines too, so
it can be written back unchanged (``Document.text()``).
"""

import re
from dataclasses import dataclass, field

NAME = r"[a-z0-9_][a-z0-9_.-]*"
SECTION_RE = re.compile(r"^\s*\[\s*(" + NAME + r")\s*\]\s*(?:[;#].*)?$")
ENTRY_RE = re.compile(r"^\s*(" + NAME + r")\s*=\s*(.*?)\s*$")

BLANK = "blank"
COMMENT = "comment"
SECTION = "section"
ENTRY = "entry"


class ParseError(Exception):
    def __init__(self, path, line, message):
        super().__init__(f"{path}:{line}: {message}")
        self.path = path
        self.line = line
        self.message = message


@dataclass
class Line:
    """One physical line. ``number`` counts from 1."""

    number: int
    raw: str
    kind: str
    section: str = None
    key: str = None
    value: str = None


@dataclass
class Document:
    path: str
    lines: list = field(default_factory=list)

    def sections(self):
        """Section names in file order, each once."""
        seen = []
        for line in self.lines:
            if line.kind == SECTION and line.section not in seen:
                seen.append(line.section)
        return seen

    def section_line(self, name):
        for line in self.lines:
            if line.kind == SECTION and line.section == name:
                return line.number
        return None

    def entries(self, section=None):
        """Entry lines, optionally only those of one section."""
        return [
            line
            for line in self.lines
            if line.kind == ENTRY and (section is None or line.section == section)
        ]

    def get(self, section, key, default=None):
        """The value of the last ``key`` in ``section``, or ``default``."""
        found = default
        for line in self.entries(section):
            if line.key == key:
                found = line.value
        return found

    def as_dict(self):
        result = {}
        for line in self.entries():
            result.setdefault(line.section, {})[line.key] = line.value
        return result

    def text(self):
        """The document as text, exactly as parsed (plus any edits to raw)."""
        return "".join(line.raw for line in self.lines)


def _strip_inline_comment(value):
    """Drop an inline comment: a '#' that follows whitespace, and the rest."""
    for i, ch in enumerate(value):
        if ch == "#" and i > 0 and value[i - 1] in " \t":
            return value[:i].rstrip()
    return value


def parse_text(text, path="<string>"):
    doc = Document(path)
    section = None
    for number, raw in enumerate(text.splitlines(keepends=True), 1):
        stripped = raw.strip()
        if not stripped:
            doc.lines.append(Line(number, raw, BLANK, section))
            continue
        if stripped[0] in ";#":
            doc.lines.append(Line(number, raw, COMMENT, section))
            continue
        match = SECTION_RE.match(stripped)
        if match:
            section = match.group(1)
            doc.lines.append(Line(number, raw, SECTION, section))
            continue
        match = ENTRY_RE.match(stripped)
        if match:
            if section is None:
                raise ParseError(path, number, "key outside of any section")
            key, value = match.group(1), _strip_inline_comment(match.group(2))
            doc.lines.append(Line(number, raw, ENTRY, section, key, value))
            continue
        raise ParseError(path, number, f"cannot parse line: {stripped!r}")
    return doc


def parse_file(path):
    with open(path, encoding="utf-8") as handle:
        return parse_text(handle.read(), path)
