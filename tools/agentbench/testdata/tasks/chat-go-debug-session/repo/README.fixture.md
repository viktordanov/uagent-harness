# billing

A small billing library and its CLI, `billctl`. It turns a customer list
into monthly invoices and decides what to do about unpaid ones.

```sh
go run ./cmd/billctl invoice -month 2026-03 testdata/customers.json
go run ./cmd/billctl overdue -month 2026-03 -today 2026-05-20 testdata/customers.json
```

## Packages

| Package | Holds |
| --- | --- |
| `internal/money` | amounts in cents, proration and rounding |
| `internal/period` | billing months and day counts |
| `internal/plan` | the plan catalog |
| `internal/customer` | the customer file and subscription ranges |
| `internal/invoice` | building and printing invoices |
| `internal/dunning` | reminders, late fees and suspension for unpaid invoices |
| `cmd/billctl` | the CLI |

## Billing rules

- Every date is a calendar date in the customer's own time zone (`timezone`
  in the customer file, UTC when empty). A month runs from midnight on the
  1st to midnight on the 1st of the next month, in that zone.
- A customer subscribed for the whole month pays the plan's monthly price.
  A customer who starts or stops during the month pays
  `monthly * days / days in month`, rounded half away from zero to the cent.
  The start date is billed, the end date is not.
- An invoice is due 14 days after the end of the billed month.

## Dunning

An unpaid invoice is overdue from the day after its due date. Days overdue
are whole calendar days in the customer's time zone.

| Days overdue | Stage | Fee |
| --- | --- | --- |
| 1 to 13 | `reminder` | none |
| 14 to 29 | `late-fee` | 2% of the total, at least 1.00 |
| 30 or more | `suspend` | 5% of the total, at least 1.00 |

## Customer file

```json
[
  {"id": "acme", "name": "Acme Corp", "plan": "team", "start": "2025-11-01"},
  {"id": "umbrella", "plan": "pro", "timezone": "Europe/Berlin", "start": "2026-03-15", "end": "2026-06-01"}
]
```

## Development

```sh
go test ./...
```
