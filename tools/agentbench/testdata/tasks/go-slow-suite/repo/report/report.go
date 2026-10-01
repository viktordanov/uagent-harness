// Package report formats ledger data for people.
package report

import "fmt"

// Cents formats an amount in cents as dollars, e.g. -1234 as "-12.34".
func Cents(c int64) string {
	sign := ""
	if c < 0 {
		sign, c = "-", -c
	}

	return fmt.Sprintf("%s%d.%02d", sign, c/100, c%100)
}
