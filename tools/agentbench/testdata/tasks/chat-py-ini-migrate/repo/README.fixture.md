# cfgtool

Reads and validates the INI-style configs our services start from. Python
3.10+, no dependencies.

```
python3 -m cfgtool check examples/*.ini
python3 -m cfgtool show examples/api.ini
python3 -m cfgtool keys
```

## Config format

```ini
; a comment line (# works too)
[http]
port = 8080
request_timeout = 30s   ; an inline comment
```

- Section and key names are lower-case letters, digits, `_`, `-` and `.`.
- Whitespace around names and values is ignored.
- An inline comment starts at a `;` or `#` that follows whitespace, so
  `color = #fff` and `path = a;b` are plain values.
- A key must be inside a section; anything else that is not a section, a
  key, a comment, or blank is a parse error.

## Sections and keys

`python3 -m cfgtool keys` prints the whole schema (`cfgtool/schema.py`).
In short:

| Section | Keys |
| --- | --- |
| `[service]` (required) | `name` (required), `environment` (dev, staging, prod), `workers` |
| `[http]` (required) | `host`, `port` (required), `request_timeout`, `tls`, `allowed_origins` |
| `[database]` (required) | `url` (required), `pool_size`, `connect_timeout`, `read_only` |
| `[logging]` | `level` (debug, info, warn, error), `path`, `json` |

Kinds: booleans are `true/false`, `yes/no`, `on/off` or `1/0`; durations
are a number and a unit, `250ms`, `30s`, `5m`, `1h`; lists are
comma-separated.

## Commands

### check

```
python3 -m cfgtool check FILE...
```

Prints one line per problem, `FILE:LINE: message`, then a count, or
`N ok` when every file is valid. It reports unknown sections, missing
required sections and keys, duplicate keys, and values of the wrong kind.
Keys it does not know inside a known section are ignored.

### show

Prints the effective config of one valid file, `section.key = value`, with
defaults filled in.

### keys

Lists every key the schema knows, with its kind and default.

## Exit status

| Status | Meaning |
| --- | --- |
| 0 | every file is valid |
| 1 | at least one problem |
| 2 | usage error, or a file that cannot be read |

## Development

```
python3 -m unittest discover -s tests
```
