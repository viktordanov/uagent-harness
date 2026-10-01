// Package textstat computes small statistics over text.
package textstat

import "strings"

// Words splits text into lower-case words; punctuation around a word is
// dropped.
func Words(text string) []string {
	var out []string
	for _, f := range strings.Fields(text) {
		w := strings.Trim(f, ".,;:!?\"'()")
		if w == "" {
			continue
		}
		out = append(out, w)
	}

	return out
}
