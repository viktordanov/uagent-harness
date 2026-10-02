# Changelog

## Unreleased

### Fixed

- Prorated invoices for customers in a time zone with daylight saving time
  were a day short when the billed range crossed the clock change: a Berlin
  customer starting 2026-03-15 got "16 of 30 days" instead of "17 of 31".
  Day counts divided the elapsed time by 24 hours, and a spring-forward day
  has 23. They now count calendar dates.
- `dunning.DaysOverdue` had the same mistake and reported one day fewer
  across a spring-forward change, which could delay a late fee.

### Changed

- `period.DaysBetween` is renamed `period.CalendarDays`.
