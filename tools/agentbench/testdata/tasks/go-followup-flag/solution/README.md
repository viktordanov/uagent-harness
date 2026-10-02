# logq

`logq` filters and prints JSON-lines logs, one object per line:

```json
{"time":"2026-03-01T10:00:00Z","level":"info","msg":"server started","fields":{"port":8080}}
```

```sh
logq [flags] [file ...]    # no file: read stdin
```

| Flag | Meaning |
| --- | --- |
| `--level L` | keep entries at level L or above: `debug`, `info`, `warn`, `error` |
| `--grep S` | keep entries whose message contains S |
| `--format F` | `text` (the default) or `json` |
| `--after T` | keep entries at or after T: an RFC 3339 time (`2026-03-01T10:00:00Z`), or a duration (`90m`, `2h`) counted back from the newest entry in the input |
| `--since T` | deprecated: the same as `--after`, with a warning on stderr |

Entries keep their input order. A line that is not a JSON object is skipped with a warning on stderr.

Exit status: 0 on success, 1 when a file cannot be read, 2 on a usage error (a bad flag or value).

## Development

```sh
go test ./...
```
