package invoice

import (
	"fmt"
	"io"
	"text/tabwriter"
)

// WriteText writes inv as a small plain-text table.
func WriteText(w io.Writer, inv Invoice) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "%s\tcustomer %s\tdue %s\n", inv.Number, inv.CustomerID, inv.Due.Format("2006-01-02"))
	for _, l := range inv.Lines {
		fmt.Fprintf(tw, "  %s\t%s\t%d days\t%s\n", l.Description, l.Period, l.Days, l.Amount)
	}
	fmt.Fprintf(tw, "  total\t\t\t%s\n", inv.Total)
	return tw.Flush()
}
