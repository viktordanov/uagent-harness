package state

import (
	"fmt"
	"strings"
	"time"
)

// heatmapWeeks is how far back /status shows activity.
const heatmapWeeks = 12

// heatShades go from no runs to the busiest day.
var heatShades = []string{"·", "░", "▒", "▓", "█"}

// Heatmap draws runs per day as a GitHub-style grid: a row per weekday
// (Monday first), a column per week, ending with the week of now.
func Heatmap(counts map[string]int, now time.Time, weeks int) string {
	// Start on the Monday weeks-1 weeks before this week's Monday.
	offset := (int(now.Weekday()) + 6) % 7
	start := now.AddDate(0, 0, -offset-7*(weeks-1))
	peak, total := 0, 0
	for _, n := range counts {
		peak = max(peak, n)
		total += n
	}
	var b strings.Builder
	fmt.Fprintf(&b, "activity · last %d weeks · %d runs\n", weeks, total)
	for day, label := range []string{"Mon", "", "Wed", "", "Fri", "", "Sun"} {
		fmt.Fprintf(&b, "%-4s", label)
		for week := range weeks {
			d := start.AddDate(0, 0, 7*week+day)
			if d.After(now) {
				break
			}
			b.WriteString(shade(counts[d.Format(time.DateOnly)], peak))
		}
		if day < 6 {
			b.WriteByte('\n')
		}
	}

	return b.String()
}

// shade picks a shade for n runs relative to the busiest day.
func shade(n, peak int) string {
	if n == 0 || peak == 0 {
		return heatShades[0]
	}
	level := 1 + (n-1)*(len(heatShades)-2)/max(peak-1, 1)

	return heatShades[min(level, len(heatShades)-1)]
}
