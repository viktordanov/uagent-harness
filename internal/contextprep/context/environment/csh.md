---
id: csh
description: The POSIX constructs that fail in csh, and what to write instead
when: {shell: [csh]}
---
csh is not a POSIX shell. These fail in csh:
- x=1, export X=1, X=1 cmd: use set x = 1, setenv X 1, env X=1 cmd.
- $(cmd): use backticks.
- 2>&1: use >& or |&.
- for …; do …; done, if …; then …; fi: use foreach … end, if (…) then … endif.
For a POSIX script, run it as one sh -c '…'.
