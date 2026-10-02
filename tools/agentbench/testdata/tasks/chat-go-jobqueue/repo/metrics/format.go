package metrics

import (
	"fmt"
	"io"
	"text/tabwriter"
	"time"
)

// Summary returns the totals in one line, as jobq run prints them:
//
//	5 claimed, 3 succeeded, 1 retried, 1 failed
func (s Snapshot) Summary() string {
	return fmt.Sprintf("%d claimed, %d succeeded, %d retried, %d failed",
		s.Claimed, s.Succeeded, s.Retried, s.Failed)
}

// WriteTable writes one row per kind, sorted by kind, with the average time
// of a successful attempt.
func (s Snapshot) WriteTable(w io.Writer) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "KIND\tCLAIMED\tSUCCEEDED\tRETRIED\tFAILED\tAVG")
	for _, k := range s.KindNames() {
		kc := s.Kinds[k]
		fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%s\n",
			k, kc.Claimed, kc.Succeeded, kc.Retried, kc.Failed, average(kc))
	}
	return tw.Flush()
}

func average(kc KindCounts) string {
	if kc.Succeeded == 0 {
		return "-"
	}
	avg := kc.Busy / time.Duration(kc.Succeeded)
	return avg.Round(time.Microsecond).String()
}
