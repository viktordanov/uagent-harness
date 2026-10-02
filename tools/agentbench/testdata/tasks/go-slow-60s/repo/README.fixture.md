# billing

Money arithmetic (`money/`), monthly reports (`report/`), and end-to-end
integration tests (`integration/`, slow: about a minute).

`go test ./...` runs everything; `go test ./money ./report` is fast.
