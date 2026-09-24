package patch

import (
	"fmt"
	"strings"
)

// Plain writes diffs as plain text, for `uah sessions show`: a
// "Edited path (+3 -1)" line per file, then its lines as "12 +text",
// hunks separated by "⋮", each line after indent.
func Plain(files []FileDiff, indent string) string {
	var b strings.Builder
	for _, f := range files {
		verb := map[string]string{"add": "Added", "delete": "Deleted"}[f.Op]
		if verb == "" {
			verb = "Edited"
		}
		path := f.Path
		if f.MovePath != "" {
			path += " → " + f.MovePath
		}
		fmt.Fprintf(&b, "%s%s %s (+%d -%d)\n", indent, verb, path, f.Added, f.Removed)
		for i, h := range f.Hunks {
			if i > 0 {
				fmt.Fprintf(&b, "%s  ⋮\n", indent)
			}
			for _, l := range h.Lines {
				fmt.Fprintf(&b, "%s  %4d %s%s\n", indent, l.Line(), l.Kind, l.Text)
			}
		}
		if f.Omitted > 0 {
			fmt.Fprintf(&b, "%s  … %d more lines not kept\n", indent, f.Omitted)
		}
	}

	return b.String()
}
