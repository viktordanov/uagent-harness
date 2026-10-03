---
id: cmd
description: What cmd.exe does instead of POSIX sh
when: {shell: [cmd]}
---
This is cmd.exe, not a POSIX shell: use set X=1 and %X%, double quotes only (single quotes are literal), 2>nul, and no heredocs or POSIX loops. For anything more, run powershell -Command "…".
