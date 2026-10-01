# Errors

The store defines the errors callers may test for, as exported sentinel
values in package `store`:

- `ErrNotFound`: the key does not exist.
- `ErrConflict`: the write's version does not match the stored version.

Every layer wraps the error it got with `fmt.Errorf("...: %w", err)` and adds
context (at least the key), so the message reads like
`service: rename "a": store: get "a": not found`. Nobody compares error
strings: callers use `errors.Is`.

The API maps errors to HTTP statuses in `api.Status`:

| Error           | Status |
| --------------- | ------ |
| nil             | 200    |
| `ErrNotFound`   | 404    |
| `ErrConflict`   | 409    |
| anything else   | 500    |
