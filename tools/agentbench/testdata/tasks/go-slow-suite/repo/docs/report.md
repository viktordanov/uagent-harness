# report.Summary

`func Summary(entries []ledger.Entry) string`

Returns one line describing a list of entries:

    <n> entries: <credits> in, <debits> out, balance <balance>

- `<n>` is the number of entries; a single entry is written `1 entry`.
- `<credits>` and `<debits>` are the totals of each kind, and `<balance>` is
  credits minus debits, all formatted with `Cents` (so `12.34`, `-0.50`).
- No entries gives `no entries`.

Example: a credit of 1000 and a debit of 250 give
`2 entries: 10.00 in, 2.50 out, balance 7.50`.
