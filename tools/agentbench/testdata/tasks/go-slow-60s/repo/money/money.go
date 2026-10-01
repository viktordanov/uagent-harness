// Package money does arithmetic on amounts in cents.
package money

import "fmt"

// Cents is an amount of money in cents.
type Cents int64

// Format writes c as dollars with two decimals, e.g. -$12.05.
func Format(c Cents) string {
	sign := ""
	if c < 0 {
		sign = "-"
		c = -c
	}

	return fmt.Sprintf("%s$%d.%d", sign, c/100, c%100)
}

// Split divides c into n parts that differ by at most one cent, larger
// parts first, and sum to c.
func Split(c Cents, n int) []Cents {
	parts := make([]Cents, n)
	base := c / Cents(n)
	rest := c % Cents(n)
	for i := range parts {
		parts[i] = base
		if Cents(i) < rest {
			parts[i]++
		}
	}

	return parts
}
