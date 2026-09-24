package models

import (
	"cmp"
	"slices"
	"strings"
)

// maxSuggestions is how many near misses Suggest returns.
const maxSuggestions = 3

// Suggest returns up to three of candidates that id was probably meant to
// be, best first: the same words in another order (gpt-luna-6 → gpt-6-luna),
// then the smallest edit distances within a third of id's length. Case is
// ignored.
func Suggest(id string, candidates []string) []string {
	want := strings.ToLower(strings.TrimSpace(id))
	if want == "" {
		return nil
	}
	limit := max(2, len(want)/3)
	type scored struct {
		id    string
		score int
	}
	var hits []scored
	for _, c := range candidates {
		lower := strings.ToLower(c)
		switch {
		case lower == want:
			hits = append(hits, scored{c, 0})
		case sameWords(lower, want):
			hits = append(hits, scored{c, 1})
		default:
			if d := distance(lower, want); d <= limit {
				hits = append(hits, scored{c, 1 + d})
			}
		}
	}
	slices.SortStableFunc(hits, func(a, b scored) int {
		if c := cmp.Compare(a.score, b.score); c != 0 {
			return c
		}

		return strings.Compare(a.id, b.id)
	})
	out := make([]string, 0, min(len(hits), maxSuggestions))
	for _, h := range hits[:min(len(hits), maxSuggestions)] {
		out = append(out, h.id)
	}

	return out
}

// sameWords reports whether a and b have the same words, split at - . _ / :
// and spaces, in any order.
func sameWords(a, b string) bool {
	split := func(s string) []string {
		words := strings.FieldsFunc(s, func(r rune) bool { return strings.ContainsRune("-._/: ", r) })
		slices.Sort(words)

		return words
	}

	return slices.Equal(split(a), split(b))
}

// distance is the Levenshtein distance between a and b, in bytes.
func distance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}

	return prev[len(b)]
}
