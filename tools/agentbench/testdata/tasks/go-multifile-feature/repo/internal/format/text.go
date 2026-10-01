// Package format writes items for people and programs.
package format

import (
	"fmt"
	"io"
	"text/tabwriter"

	"example.com/inventory/internal/store"
)

// Text writes an aligned table with a header.
func Text(w io.Writer, items []store.Item) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tQTY\tPRICE")
	for _, it := range items {
		fmt.Fprintf(tw, "%s\t%d\t%d.%02d\n", it.Name, it.Qty, it.PriceCents/100, it.PriceCents%100)
	}

	return tw.Flush()
}
