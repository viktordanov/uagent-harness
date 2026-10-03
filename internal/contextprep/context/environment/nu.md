---
id: nu
description: The POSIX constructs that fail in nushell, and what to write instead
when: {shell: [nu]}
---
nushell is not a POSIX shell; its commands return structured data. These fail in nushell:
- && and ||: separate commands with ;.
- x=1, export X=1, $X: use $env.X = '1' and $env.X.
- $(cmd), backticks: use (cmd).
- 2>&1, 2>/dev/null: use o+e>| and e> /dev/null.
- Heredocs, for …; do …; done, if …; then …; fi.
ls, rm, cp and others are nushell builtins with other flags: prefix ^ (^ls) for the external command. For a POSIX script, run it as one sh -c '…'.
