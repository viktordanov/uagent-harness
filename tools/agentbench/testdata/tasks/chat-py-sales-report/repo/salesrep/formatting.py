"""Plain-text tables."""

from .money import format_money


def format_table(header, rows, footer=None):
    """Lay out rows of strings in columns.

    The first column is left-aligned, the others right-aligned. ``footer``
    is a list of lines printed under the table, after a blank line.
    """
    widths = [len(cell) for cell in header]
    for row in rows:
        for i, cell in enumerate(row):
            widths[i] = max(widths[i], len(cell))

    def render(cells):
        parts = []
        for i, cell in enumerate(cells):
            if i == 0:
                parts.append(cell.ljust(widths[i]))
            else:
                parts.append(cell.rjust(widths[i]))
        return "  ".join(parts).rstrip()

    lines = [render(header), render(["-" * w for w in widths])]
    lines.extend(render(row) for row in rows)
    if footer:
        lines.append("")
        lines.extend(footer)
    return "\n".join(lines)


def row_count(count):
    return f"{count} row" if count == 1 else f"{count} rows"


def format_summary(pairs, by, grand, count):
    """The summary table: one line per group, then TOTAL, then the footer."""
    body = [[group, format_money(total)] for group, total in pairs]
    body.append(["TOTAL", format_money(grand)])
    return format_table([by, "total"], body, footer=[row_count(count)])


def format_top(pairs):
    body = [[str(i), name, format_money(total)] for i, (name, total) in enumerate(pairs, 1)]
    return format_table(["#", "product", "total"], body)


def format_units(pairs, by):
    body = [[group, str(units)] for group, units in pairs]
    return format_table([by, "units"], body)
