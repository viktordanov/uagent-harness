---
id: xonsh
description: The POSIX constructs that fail in xonsh, and what to write instead
when: {shell: [xonsh]}
---
xonsh is Python-based, not a POSIX shell. Simple commands, pipes, && and || work; these fail:
- Heredocs, for …; do …; done, if …; then …; fi: use Python syntax.
- x=1, export X=1: use $X = '1'.
- Backticks are regex globs, not command substitution: use $(cmd).
For a POSIX script, run it as one sh -c '…'.
