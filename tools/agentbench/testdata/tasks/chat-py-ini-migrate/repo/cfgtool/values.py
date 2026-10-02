"""Converting raw string values to Python values, one function per kind."""

import re

TRUE = {"true", "yes", "on", "1"}
FALSE = {"false", "no", "off", "0"}
DURATION_RE = re.compile(r"^(\d+(?:\.\d+)?)(ms|s|m|h)$")
UNITS = {"ms": 0.001, "s": 1, "m": 60, "h": 3600}


class ValueError_(ValueError):
    """A value that does not fit its kind."""


def parse_str(text, field=None):
    if text == "":
        raise ValueError_("must not be empty")
    return text


def parse_int(text, field=None):
    try:
        return int(text, 10)
    except ValueError:
        raise ValueError_(f"not an integer: {text!r}") from None


def parse_port(text, field=None):
    port = parse_int(text)
    if not 1 <= port <= 65535:
        raise ValueError_(f"port out of range: {port}")
    return port


def parse_bool(text, field=None):
    lowered = text.lower()
    if lowered in TRUE:
        return True
    if lowered in FALSE:
        return False
    raise ValueError_(f"not a boolean: {text!r}")


def parse_duration(text, field=None):
    """'250ms', '30s', '5m', '1.5h' -> seconds (float)."""
    match = DURATION_RE.match(text)
    if not match:
        raise ValueError_(f"not a duration: {text!r} (use e.g. 250ms, 30s, 5m, 1h)")
    return float(match.group(1)) * UNITS[match.group(2)]


def parse_list(text, field=None):
    return [item.strip() for item in text.split(",") if item.strip()]


def parse_enum(text, field=None):
    choices = field.choices if field is not None else ()
    if text not in choices:
        raise ValueError_(f"must be one of {', '.join(choices)}: {text!r}")
    return text


PARSERS = {
    "str": parse_str,
    "int": parse_int,
    "port": parse_port,
    "bool": parse_bool,
    "duration": parse_duration,
    "list": parse_list,
    "enum": parse_enum,
}


def convert(text, field):
    """Convert ``text`` according to ``field.kind``."""
    return PARSERS[field.kind](text, field)
