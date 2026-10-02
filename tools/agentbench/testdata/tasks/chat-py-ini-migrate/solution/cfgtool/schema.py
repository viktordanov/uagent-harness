"""What a service config may contain.

SCHEMA maps a section to its keys; each key has a Field that gives its
kind (see values.py), whether it is required, and its default.
"""

from dataclasses import dataclass


@dataclass(frozen=True)
class Field:
    kind: str
    required: bool = False
    default: str = None
    choices: tuple = ()
    help: str = ""


SCHEMA = {
    "service": {
        "name": Field("str", required=True, help="service name, used in logs and metrics"),
        "environment": Field("enum", default="dev", choices=("dev", "staging", "prod")),
        "workers": Field("int", default="4", help="worker processes"),
    },
    "http": {
        "host": Field("str", default="0.0.0.0"),
        "port": Field("port", required=True),
        "request_timeout": Field("duration", default="30s", help="per-request deadline"),
        "tls": Field("bool", default="false"),
        "allowed_origins": Field("list", default="", help="CORS origins, comma-separated"),
    },
    "database": {
        "url": Field("str", required=True),
        "pool_size": Field("int", default="10"),
        "connect_timeout": Field("duration", default="5s"),
        "read_only": Field("bool", default="false"),
    },
    "logging": {
        "level": Field("enum", default="info", choices=("debug", "info", "warn", "error")),
        "path": Field("str", default="-", help="log file, - for stderr"),
        "json": Field("bool", default="false"),
    },
}

REQUIRED_SECTIONS = ("service", "http", "database")

# Keys that were renamed: (section, old key) -> new key. `lint` warns about
# them and `migrate` rewrites them.
DEPRECATED = {
    ("http", "timeout"): "request_timeout",
    ("database", "pool"): "pool_size",
    ("logging", "file"): "path",
}


def replacement_for(section, key):
    """The new name of a deprecated key, or None."""
    return DEPRECATED.get((section, key))


def is_known_section(name):
    return name in SCHEMA


def field_for(section, key):
    """The Field for ``section.key``, or None when the schema does not know it."""
    return SCHEMA.get(section, {}).get(key)


def all_keys():
    """Every (section, key, field), in schema order."""
    for section, keys in SCHEMA.items():
        for key, field in keys.items():
            yield section, key, field
