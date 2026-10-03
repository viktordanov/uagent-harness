---
id: tmpdir
description: The session's private $TMPDIR and what it is for
when: {sandbox: [read-only, workspace-write]}
---
$TMPDIR ({{tmpdir}}) is this session's private scratch directory, writable in every mode: put temporary files there, and point a tool cache that cannot be written elsewhere at it, such as GOCACHE=$TMPDIR/go-build.
