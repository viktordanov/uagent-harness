// Package ids cleans lists of identifiers.
package ids

import (
	"strings"
	"unicode"
)

// Dedupe returns ids without duplicates, in the order of first appearance.
// IDs compare case-insensitively after trimming spaces; the first spelling
// (trimmed) is kept. Empty IDs are dropped.
func Dedupe(ids []string) []string {
	var out []string
	seen := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		k := foldKey(id)
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, id)
	}

	return out
}

// foldKey maps each rune to the smallest rune of its simple fold orbit, so
// two strings have the same key exactly when strings.EqualFold holds.
func foldKey(s string) string {
	return strings.Map(func(r rune) rune {
		m := r
		for f := unicode.SimpleFold(r); f != r; f = unicode.SimpleFold(f) {
			m = min(m, f)
		}

		return m
	}, s)
}
