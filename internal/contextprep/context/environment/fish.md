---
id: fish
description: The POSIX constructs that fail in fish, and what to write instead
when: {shell: [fish]}
---
fish is not a POSIX shell. These fail in fish:
- Heredocs (<<EOF, <<<): pipe printf '%s\n' … into the command, or write the file with your file tool.
- for …; do …; done, while …; do …; done, if …; then …; fi, case: fish uses for x in …; …; end, while …; …; end, if …; …; end, switch.
- x=1 on its own: use set x 1 (set -x to export). VAR=1 cmd works.
- $?, ${x}, $((1+2)), backticks, <(cmd): use $status, {$x}, math 1+2, (cmd), (cmd | psub).
- An unmatched glob is an error: quote patterns (find -name '*.go').
For a POSIX script, run it as one sh -c '…' (inside fish single quotes, \' and \\ are escapes).
