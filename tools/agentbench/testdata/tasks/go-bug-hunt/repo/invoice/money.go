// Package invoice prices orders: lines, discounts, tax, and totals.
package invoice

import "fmt"

// Cents is an amount of money in cents.
type Cents int64

// String formats c as dollars.
func (c Cents) String() string {
	sign := ""
	if c < 0 {
		sign, c = "-", -c
	}

	return fmt.Sprintf("%s$%d.%02d", sign, c/100, c%100)
}

// mulRate returns c multiplied by rate/10000 (rate in basis points),
// rounded half up to the cent. Every percentage in the package goes
// through it so rounding is the same everywhere.
func mulRate(c Cents, basisPoints int64) Cents {
	n := int64(c) * basisPoints
	q, r := n/10000, n%10000
	if r*2 >= 10000 {
		q++
	}

	return Cents(q)
}
