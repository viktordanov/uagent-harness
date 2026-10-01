# Size strings

## ParseSize(s string) (int64, error)

1. Leading and trailing white space is ignored.
2. The rest is a number, then optionally one space, then optionally a unit.
3. The number is one or more ASCII digits, optionally followed by `.` and one
   or more digits. There is no sign: `-1`, `+1`, `.5`, and `5.` are syntax
   errors.
4. Units are case-sensitive:

   | Unit | Bytes |
   | --- | --- |
   | (none), `B` | 1 |
   | `kB`, `MB`, `GB`, `TB` | 1000, 1000², 1000³, 1000⁴ |
   | `KiB`, `MiB`, `GiB`, `TiB` | 1024, 1024², 1024³, 1024⁴ |

5. At most one ASCII space may separate the number and the unit. Any other
   white space there, or more than one space, is a syntax error. Anything
   after the number that does not start with a letter is a syntax error too.
6. The result is the exact value rounded **down** to a whole number of bytes,
   so `1.5 B` is 1 and `0.001 kB` is 1. Compute it exactly: no floating point.
7. A result above the largest int64 is a range error.

Errors: every error wraps one of the package's sentinels, so `errors.Is` works:

- `ErrSyntax` for a malformed number or spacing (including the empty string),
- `ErrUnit` for a well-formed number followed by an unknown unit,
- `ErrRange` for a value that does not fit in int64.

## FormatSize(n int64) string

- Below 1024: `"<n> B"`, e.g. `"0 B"`, `"1023 B"`.
- Otherwise the largest binary unit (`KiB`, `MiB`, `GiB`, `TiB`) whose size is
  at most n, with one decimal rounded **down**, and a trailing `.0` dropped:
  `1024` is `"1 KiB"`, `1536` is `"1.5 KiB"`, `1535` is `"1.4 KiB"`,
  `1048575` is `"1023.9 KiB"`. Values of a PiB or more stay in TiB.
- A negative n is formatted as `-` followed by the format of -n.
