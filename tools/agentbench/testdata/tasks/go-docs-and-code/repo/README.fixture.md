# fetcher

A small HTTP fetcher used by the nightly import jobs. It reads a config
file, applies environment overrides, and downloads each URL in its list.

## Usage

```sh
fetcher -config fetcher.conf urls.txt
```

## Configuration

The config file has one `key = value` per line; `#` starts a comment.
Every key can be overridden with an environment variable.

| Key | Environment variable | Default | Meaning |
| --- | --- | --- | --- |
| `timeout` | `FETCHER_TIMEOUT` | `30s` | How long one attempt may take (a Go duration). |
| `user_agent` | `FETCHER_USER_AGENT` | `fetcher/1.0` | The User-Agent header sent with each request. |

## Development

Run `go test ./...`.
