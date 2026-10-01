// Package size parses and formats byte sizes such as "1.5 MiB"; see
// docs/SPEC.md.
package size

import "errors"

// The sentinel errors that ParseSize's errors wrap.
var (
	ErrSyntax = errors.New("size: invalid syntax")
	ErrUnit   = errors.New("size: unknown unit")
	ErrRange  = errors.New("size: value out of range")
)

// ParseSize parses a size string into bytes.
func ParseSize(s string) (int64, error) {
	return 0, errors.New("not implemented")
}

// FormatSize formats n bytes for people.
func FormatSize(n int64) string {
	return ""
}
