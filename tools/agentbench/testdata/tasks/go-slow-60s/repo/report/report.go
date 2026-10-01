// Package report builds monthly billing reports.
package report

import (
	"sort"

	"example.com/billing/money"
)

// Line is one charge on a customer's bill.
type Line struct {
	Customer string
	Item     string
	Amount   money.Cents
}

// Totals sums the lines per customer.
func Totals(lines []Line) map[string]money.Cents {
	out := map[string]money.Cents{}
	for _, l := range lines {
		out[l.Customer] += l.Amount
	}

	return out
}

// Customers lists the customers with lines, sorted.
func Customers(lines []Line) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range lines {
		if !seen[l.Customer] {
			seen[l.Customer] = true
			out = append(out, l.Customer)
		}
	}
	sort.Strings(out)

	return out
}
