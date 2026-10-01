"""Parse job logs and summarise how long each job ran."""

from .records import parse_line, parse_lines
from .report import summarize

__all__ = ["parse_line", "parse_lines", "summarize"]
