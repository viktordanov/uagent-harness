package report

import (
	"fmt"
	"sort"
	"strings"

	"example.com/billing/money"
)

// Digest summarizes lines for the billing channel: see docs/digest.md.
func Digest(lines []Line, top int) string {
	totals := Totals(lines)
	customers := Customers(lines)
	var sum money.Cents
	for _, c := range customers {
		sum += totals[c]
	}
	sort.SliceStable(customers, func(i, j int) bool { return totals[customers[i]] > totals[customers[j]] })
	if top > 0 && top < len(customers) {
		customers = customers[:top]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d customers, total %s\n", len(Customers(lines)), money.Format(sum))
	for _, c := range customers {
		fmt.Fprintf(&b, "- %s: %s\n", c, money.Format(totals[c]))
	}

	return b.String()
}
