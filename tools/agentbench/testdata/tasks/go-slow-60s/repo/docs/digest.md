# Digest

`report.Digest(lines []Line, top int) string` summarizes a month's lines as
text for the billing channel.

- The first line is `N customers, total $X.YY` where N is the number of
  distinct customers and the total is the sum of all lines, formatted with
  `money.Format`.
- Then one line per customer, the `top` customers with the largest totals
  (all of them when `top` is 0 or more than there are), largest first; ties
  in alphabetical order. Each line is `- <customer>: <total>`, with the
  total formatted by `money.Format`.
- Lines end with `\n`, including the last one.
- No lines: `0 customers, total $0.00\n`.

Example, with top 2:

```
3 customers, total $12.50
- acme: $8.00
- bolt: $3.00
```
