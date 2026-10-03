---
id: go
description: Go toolchain notes, caches in the sandbox and running one test
files: [go.mod, go.work]
check: [go, version]
enabled: false
---
Go: when the build cache cannot be written in the sandbox, set GOCACHE=$TMPDIR/go-build (and GOTMPDIR=$TMPDIR). Run one test with go test ./pkg -run '^TestName$' instead of the whole suite, and go vet ./... before you finish.
