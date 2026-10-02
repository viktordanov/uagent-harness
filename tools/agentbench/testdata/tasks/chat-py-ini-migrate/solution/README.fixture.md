# cfgtool

Reads and validates the INI-style configs our services start from. Python
3.10+, no dependencies.

```
python3 -m cfgtool check examples/*.ini
python3 -m cfgtool show examples/api.ini
python3 -m cfgtool keys
python3 -m cfgtool lint examples/*.ini
python3 -m cfgtool migrate --dry-run examples/billing.ini
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

### lint

```
python3 -m cfgtool lint FILE...
```

Reports keys the schema does not know inside a known section, one line
each, `FILE:LINE: unknown key 'KEY' in [SECTION]`, and deprecated keys as
warnings that name the replacement,
`FILE:LINE: warning: 'timeout' in [http] is deprecated, use 'request_timeout'`.
Deprecated keys are listed in `DEPRECATED` in `cfgtool/schema.py`:

| Section | Old key | New key |
| --- | --- | --- |
| `[http]` | `timeout` | `request_timeout` |
| `[database]` | `pool` | `pool_size` |
| `[logging]` | `file` | `path` |

### migrate

```
python3 -m cfgtool migrate [--dry-run] FILE...
```

Renames deprecated keys to their replacements in place and prints
`FILE: N keys migrated`. Only key names change: values, spacing, inline
comments, other lines, and key order stay as they were. With `--dry-run`
it prints a unified diff of the change instead and writes nothing.

## Exit status

| Command | 0 | 1 | 2 |
| --- | --- | --- | --- |
| `check` | every file is valid | at least one problem | usage error, or a file that cannot be read |
| `show` | printed | the file has problems | usage error, or a file that cannot be read |
| `keys` | printed | - | usage error |
| `lint` | no unknown keys (warnings only, or nothing) | an unknown key or a parse error | usage error, or a file that cannot be read |
| `migrate` | migrated, or diff printed with `--dry-run` | a file that cannot be parsed | usage error, or a file that cannot be read |

## Development

```
python3 -m unittest discover -s tests
```
