# shorty

A small URL shortener: `POST /shorten` with `{"url": "https://..."}` returns `{"code": "abc123"}`, and `GET /abc123` redirects (302) to the URL. `GET /healthz` returns 200.

```sh
go run . -addr :8080
```

| Flag | Default | Meaning |
| --- | --- | --- |
| `-addr` | `:8080` | the address to listen on |
| `-data` | none | keep the links in this file, so they survive a restart |

Codes are six characters from `[a-z0-9]`. Without `-data` the links live in memory, so a restart forgets them.

## Development

```sh
go test ./...
```
