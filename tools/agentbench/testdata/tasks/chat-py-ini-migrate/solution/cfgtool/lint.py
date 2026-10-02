"""Lint: keys the schema does not know, and deprecated keys.

Unknown keys inside a known section are errors; deprecated keys are
warnings that name the replacement. Keys in unknown sections are left to
``check``.
"""

from dataclasses import dataclass

from . import schema


@dataclass(frozen=True)
class Finding:
    path: str
    line: int
    message: str
    warning: bool = False

    def __str__(self):
        prefix = "warning: " if self.warning else ""
        return f"{self.path}:{self.line}: {prefix}{self.message}"


def lint(doc):
    findings = []
    for line in doc.entries():
        if not schema.is_known_section(line.section):
            continue
        if schema.field_for(line.section, line.key) is not None:
            continue
        new = schema.replacement_for(line.section, line.key)
        if new is not None:
            message = f"'{line.key}' in [{line.section}] is deprecated, use '{new}'"
            findings.append(Finding(doc.path, line.number, message, warning=True))
        else:
            message = f"unknown key '{line.key}' in [{line.section}]"
            findings.append(Finding(doc.path, line.number, message))
    return findings
