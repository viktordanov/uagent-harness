# tasks

A tiny task tracker: a Go API (`api/`, served by `cmd/server`) and a plain
ES-module front end (`web/`, no build step).

- `GET /api/tasks` lists tasks, newest first.
- `POST /api/tasks` creates one from `{"title": "..."}`; it starts as `todo`.

Run the tests with `go test ./...` and `node --test web/*.test.js`.
