---
id: elvish
description: The POSIX constructs that fail in elvish, and what to write instead
when: {shell: [elvish]}
---
elvish is not a POSIX shell. These fail in elvish:
- && and ||: separate commands with ; (a failing command throws and stops the rest).
- x=1, export X=1, $X: use var x = 1, set E:X = 1, $E:X.
- $(cmd), backticks: use (cmd).
- Heredocs, for …; do …; done, if …; then …; fi.
For a POSIX script, run it as one sh -c '…'.
