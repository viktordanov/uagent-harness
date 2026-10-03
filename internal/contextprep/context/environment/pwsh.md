---
id: pwsh
description: The POSIX constructs that fail or differ in PowerShell
when: {shell: [pwsh]}
---
This is PowerShell, not a POSIX shell. These fail or differ:
- export X=1, X=1 cmd, $X: use $env:X = '1' and $env:X.
- Heredocs: use a here-string, @' and '@ on their own lines.
- The backtick is the escape character; $(…) is a subexpression.
- 2>/dev/null: use 2>$null. && and || need PowerShell 7.
- ls, rm, cp, cat (and curl in Windows PowerShell) are aliases of cmdlets with other flags: rm -rf is Remove-Item -Recurse -Force.
