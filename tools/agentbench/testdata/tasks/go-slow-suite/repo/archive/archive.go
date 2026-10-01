// Package archive stores old ledger memos.
package archive

// Compact drops empty memos.
func Compact(memos []string) []string {
	var out []string
	for _, m := range memos {
		if m != "" {
			out = append(out, m)
		}
	}

	return out
}
