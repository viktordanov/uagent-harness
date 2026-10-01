package textstat

import "sort"

// Count is a word and how often it occurs.
type Count struct {
	Word string
	N    int
}

// Top returns the n most frequent words, most frequent first; words with
// the same count are in alphabetical order.
func Top(words []string, n int) []Count {
	seen := map[string]int{}
	for _, w := range words {
		seen[w]++
	}
	var out []Count
	for w, c := range seen {
		out = append(out, Count{w, c})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].N > out[j].N
	})
	if len(out) > n {
		out = out[:n]
	}

	return out
}
