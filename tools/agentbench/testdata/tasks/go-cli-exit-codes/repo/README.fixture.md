# cnt

`cnt [-max N] FILE...` counts the lines of each file.

## Output

For each file, in order, stdout gets `<lines> <file>`. With more than one
file, a last line `<sum> total` follows (the sum of the files that could be
read). Nothing else is ever written to stdout, except the help text for `-h`.

## Errors and exit status

| Status | When | stderr |
| --- | --- | --- |
| 0 | every file was counted and no limit was exceeded | nothing |
| 1 | a file could not be read; the other files are still counted | `cnt: <error>` per file |
| 2 | usage error: unknown flag, bad `-max` value, a negative `-max`, or no files | `cnt: <message>` then the usage line |
| 3 | `-max N` was given and some file has more than N lines (its count is still printed) | `cnt: <file>: <lines> lines exceeds -max <N>` per such file |

If a file could not be read, the status is 1 even when a limit was also
exceeded. `-h` or `-help` prints the usage line to stdout and exits 0.

The usage line is: `usage: cnt [-max N] FILE...`
