# salesrep

Reports over the CSV sales exports that the shop system writes every
quarter. No dependencies beyond Python 3.10+.

```
python3 -m salesrep summary sales/2026-q1.csv
python3 -m salesrep summary --by product --region North sales/*.csv
python3 -m salesrep summary --by month --format json sales/*.csv
python3 -m salesrep top --n 3 sales/2026-q1.csv
python3 -m salesrep units sales/2026-q2.csv
```

## Input

One or more CSV files with a header row:

| Column | Meaning |
| --- | --- |
| `date` | ISO date, e.g. `2026-01-31` |
| `region` | sales region, e.g. `North` |
| `product` | product name |
| `quantity` | whole number of units |
| `unit_price` | price per unit, e.g. `12.50` (a `$` and thousands commas are allowed) |
| `discount` | optional percentage off the line, e.g. `10` or `12.5%` |

Column names are case-insensitive; extra columns are ignored. A file with
a bad date, price, or discount is an error that names the file and line.

A row whose `quantity` is missing or empty is skipped rather than failing
the run: a warning such as
`warning: sales/2026-q1.csv: line 7: missing quantity, skipped` goes to
stderr, the row counts toward no total, and the summary footer counts it
(`13 rows, 1 skipped`).

## Totals

A line's total is `quantity x unit_price`, less the discount, **rounded to
the cent, half up**, before it is added to any group: the reports must
match the finance spreadsheet to the cent.

## Commands

All commands take the filters `--region NAME`, `--product NAME`,
`--from YYYY-MM-DD` and `--to YYYY-MM-DD` (inclusive).

### summary

Totals per group, sorted by group name, then a `TOTAL` line and a footer
with the number of rows read:

```
region   total
------  ------
East    122.47
North   259.20
South    68.48
TOTAL   450.15

14 rows
```

`--by region` (the default), `--by product`, or `--by month`, which groups
by calendar month (`YYYY-MM`) in ascending order.

`--format json` prints the same groups as JSON instead of the table: a
list of objects with the keys `group` and `total`, the total a string with
two decimals, and no `TOTAL` line or footer:

```
[
  {"group": "East", "total": "122.47"},
  ...
]
```

### top

The best-selling products by total, highest first; equal totals are listed
by name. `--n N` (default 5).

### units

Units sold per group, `--by product` (the default) or `--by region`.

## Exit status

0 on success, 1 when a file cannot be read or holds a bad row, 2 for a
usage error.

## Development

```
python3 -m unittest discover -s tests
```
