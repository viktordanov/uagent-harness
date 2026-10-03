---
id: output
description: How to size the Bash tool's max_output_length
---
Bash output: max_output_length caps each output field at that many characters (default {{max_output_length}}); cut output keeps its head and tail around a marker, with the path of the file that holds all of it.
- Leave it unset when you need the whole output: a file you will edit, a failing test's full report.
- Set it only for noisy commands (builds, logs, broad searches), sized to what you will actually read.
- When output was cut, narrow the command (a pattern, a line range, one test) or read the saved file in parts; do not run it again with a bigger limit.
