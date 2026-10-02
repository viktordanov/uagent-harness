# Handoff: links that survive a restart

Add a file-backed store behind a `-data FILE` flag; without the flag, shorty keeps links in memory as now.

- `internal/store/file.go`: `File`, a `Store` that keeps a `Memory` and appends each new link to the file as a JSON line (`{"code":...,"url":...}`), syncing after each write; `OpenFile` replays the file on start.
- `main.go`: `-data` picks `OpenFile` over `NewMemory`.
- Tests: a restart test that saves a link, reopens the file, and reads it back.
