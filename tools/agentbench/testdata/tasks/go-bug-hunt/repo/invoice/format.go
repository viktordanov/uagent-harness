package invoice

import (
	"fmt"
	"strings"
)

// Format renders the totals as the invoice footer.
func Format(t Totals) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Subtotal: %s\n", t.Subtotal)
	if t.Discounted != t.Subtotal {
		fmt.Fprintf(&b, "Discount: %s\n", t.Discounted-t.Subtotal)
	}
	fmt.Fprintf(&b, "Tax:      %s\n", t.Tax)
	fmt.Fprintf(&b, "Total:    %s\n", t.Total)

	return b.String()
}
