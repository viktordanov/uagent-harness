"""Printing problems and configs."""


def format_problems(problems):
    lines = [str(problem) for problem in problems]
    if problems:
        noun = "problem" if len(problems) == 1 else "problems"
        lines.append(f"{len(problems)} {noun}")
    return "\n".join(lines)


def format_value(value):
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, float):
        return f"{value:g}s"
    if isinstance(value, list):
        return ", ".join(value)
    return str(value)


def format_effective(config):
    lines = []
    for section, keys in config.items():
        for key, value in keys.items():
            lines.append(f"{section}.{key} = {format_value(value)}")
    return "\n".join(lines)


def format_keys(rows):
    """rows: (section, key, field) -> a table of the schema."""
    lines = []
    for section, key, field in rows:
        flags = "required" if field.required else f"default {field.default!r}"
        line = f"{section}.{key:<18} {field.kind:<9} {flags}"
        if field.help:
            line += f"  # {field.help}"
        lines.append(line.rstrip())
    return "\n".join(lines)
