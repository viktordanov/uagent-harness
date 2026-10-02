# bookmarks

A small HTTP JSON API that keeps bookmarks in memory. Nothing is saved to
disk: restarting the server starts with an empty list (or the demo list with
`-seed`).

```sh
go run ./cmd/bookmarks -addr localhost:8080 -seed
```

## Layout

| Package | Holds |
| --- | --- |
| `internal/model` | the `Bookmark` type, validation, tag normalization |
| `internal/store` | the in-memory store |
| `internal/handlers` | the HTTP handlers, error mapping, middleware |
| `internal/seed` | demo data for `-seed` |
| `cmd/bookmarks` | the server binary |

## API

All bodies are JSON. A bookmark looks like this:

```json
{
  "id": 1,
  "url": "https://go.dev",
  "name": "The Go Programming Language",
  "tags": ["go", "lang"],
  "created_at": "2026-03-01T12:00:00Z",
  "updated_at": "2026-03-01T12:00:00Z"
}
```

| Method and path | Does | Success |
| --- | --- | --- |
| `GET /bookmarks` | list bookmarks, ordered by id | 200, a JSON array |
| `POST /bookmarks` | create a bookmark from `url`, `name`, `tags` | 201, the bookmark, and a `Location` header |
| `GET /bookmarks/{id}` | one bookmark | 200 |
| `PUT /bookmarks/{id}` | replace a bookmark's `url`, `name` and `tags` | 200, the bookmark |
| `DELETE /bookmarks/{id}` | delete a bookmark | 204, no body |
| `GET /tags` | every tag in use with its count, most used first | 200 |
| `GET /healthz` | liveness | 200 |

`GET /bookmarks` takes these optional query parameters:

| Parameter | Meaning |
| --- | --- |
| `q` | keep bookmarks whose name or URL contains this text, ignoring case |
| `tag` | keep bookmarks with this tag, ignoring case (`?tag=go` matches `Go`) |
| `limit` | return at most this many bookmarks (default: all) |
| `offset` | skip this many bookmarks first (default 0) |

`limit` and `offset` apply after `q` and `tag`, in id order, so
`?tag=go&limit=10&offset=10` is the second page of ten Go bookmarks. Both
must be non-negative integers.

Rules:

- `url` is required and must be `http` or `https`; two bookmarks cannot have
  the same URL (compared ignoring case and a trailing slash).
- `name` is required, at most 200 bytes. It was called `title` before; a
  request body may still send `title` instead, and `name` wins if both are
  set. Responses only have `name`.
- Tags are letters, digits, `-` and `_`, at most 32 bytes each. Duplicates
  that differ only in case are dropped, keeping the first spelling.
- A body with an unknown field is rejected.

### Errors

Every error is a JSON object with one field, and the matching status code:

```json
{"error": "invalid bookmark: name is required"}
```

| Status | When |
| --- | --- |
| 400 | a bad body, a bad id, a bad `limit` or `offset`, or a bookmark that fails validation |
| 404 | no bookmark has the id (`GET`, `PUT`, `DELETE`) |
| 409 | another bookmark already has the URL |
| 500 | a bug; the details go to the server log |

The store is safe for concurrent requests.

## Development

```sh
go test -race ./...
```
