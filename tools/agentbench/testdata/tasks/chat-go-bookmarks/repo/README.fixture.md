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
  "title": "The Go Programming Language",
  "tags": ["go", "lang"],
  "created_at": "2026-03-01T12:00:00Z",
  "updated_at": "2026-03-01T12:00:00Z"
}
```

| Method and path | Does | Success |
| --- | --- | --- |
| `GET /bookmarks` | list bookmarks, ordered by id | 200, a JSON array |
| `POST /bookmarks` | create a bookmark from `url`, `title`, `tags` | 201, the bookmark, and a `Location` header |
| `GET /bookmarks/{id}` | one bookmark | 200 |
| `PUT /bookmarks/{id}` | replace a bookmark's `url`, `title` and `tags` | 200, the bookmark |
| `GET /tags` | every tag in use with its count, most used first | 200 |
| `GET /healthz` | liveness | 200 |

`GET /bookmarks` takes one optional query parameter:

| Parameter | Meaning |
| --- | --- |
| `q` | keep bookmarks whose title or URL contains this text, ignoring case |

Rules:

- `url` is required and must be `http` or `https`; two bookmarks cannot have
  the same URL (compared ignoring case and a trailing slash).
- `title` is required, at most 200 bytes.
- Tags are letters, digits, `-` and `_`, at most 32 bytes each. Duplicates
  that differ only in case are dropped, keeping the first spelling.
- A body with an unknown field is rejected.

### Errors

Every error is a JSON object with one field, and the matching status code:

```json
{"error": "title is required"}
```

| Status | When |
| --- | --- |
| 400 | a bad body, a bad id, or a bookmark that fails validation |
| 404 | no bookmark has the id |
| 409 | another bookmark already has the URL |
| 500 | a bug; the details go to the server log |

## Development

```sh
go test ./...
```
