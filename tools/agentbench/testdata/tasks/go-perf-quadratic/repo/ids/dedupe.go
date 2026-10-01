// Package ids cleans lists of identifiers.
package ids

import "strings"

// Dedupe returns ids without duplicates, in the order of first appearance.
// IDs compare case-insensitively after trimming spaces; the first spelling
// (trimmed) is kept. Empty IDs are dropped.
func Dedupe(ids []string) []string {
	var out []string
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		dup := false
		for _, seen := range out {
			if strings.EqualFold(seen, id) {
				dup = true

				break
			}
		}
		if !dup {
			out = append(out, id)
		}
	}

	return out
}
