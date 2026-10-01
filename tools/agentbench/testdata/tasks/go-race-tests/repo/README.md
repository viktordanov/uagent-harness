# racefix

Small building blocks used by the ingest service:

- `cache`: a string cache with hit counting.
- `collector`: gathers results from concurrent producers.
- `worker`: a polling worker that can be stopped.

Run the tests with `go test -race ./...`.
