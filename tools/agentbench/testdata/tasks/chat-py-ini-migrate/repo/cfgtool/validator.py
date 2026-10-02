"""Checking a Document against the schema.

validate() reports:
  - sections the schema does not know,
  - required sections and keys that are missing,
  - keys given twice in one section,
  - values that do not fit their kind.

Keys the schema does not know inside a known section are not reported
here: older configs carry many of them, and the service ignores them.
"""

from dataclasses import dataclass

from . import schema
from .values import ValueError_, convert


@dataclass(frozen=True)
class Problem:
    path: str
    line: int
    message: str

    def __str__(self):
        return f"{self.path}:{self.line}: {self.message}"


def _check_sections(doc):
    problems = []
    for line in doc.lines:
        if line.kind == "section" and not schema.is_known_section(line.section):
            problems.append(Problem(doc.path, line.number, f"unknown section [{line.section}]"))
    present = doc.sections()
    for name in schema.REQUIRED_SECTIONS:
        if name not in present:
            problems.append(Problem(doc.path, 1, f"missing section [{name}]"))
    return problems


def _check_entries(doc):
    problems = []
    seen = {}
    for line in doc.entries():
        where = (line.section, line.key)
        if where in seen:
            problems.append(
                Problem(doc.path, line.number, f"duplicate key '{line.key}' in [{line.section}] (first on line {seen[where]})")
            )
            continue
        seen[where] = line.number
        field = schema.field_for(line.section, line.key)
        if field is None:
            continue
        try:
            convert(line.value, field)
        except ValueError_ as err:
            problems.append(Problem(doc.path, line.number, f"{line.section}.{line.key}: {err}"))
    return problems


def _check_required(doc):
    problems = []
    for section, key, field in schema.all_keys():
        if not field.required or section not in doc.sections():
            continue
        if doc.get(section, key) is None:
            problems.append(Problem(doc.path, doc.section_line(section), f"missing required key '{key}' in [{section}]"))
    return problems


def validate(doc):
    """All problems in ``doc``, sorted by line."""
    problems = _check_sections(doc) + _check_entries(doc) + _check_required(doc)
    return sorted(problems, key=lambda p: (p.line, p.message))


def effective(doc):
    """The config with defaults filled in: {section: {key: converted value}}.

    Only call this on a document without problems.
    """
    result = {}
    for section, key, field in schema.all_keys():
        raw = doc.get(section, key, field.default)
        if raw is None:
            continue
        result.setdefault(section, {})[key] = convert(raw, field) if raw != "" else raw
    return result
