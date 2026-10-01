// Package slug makes URL slugs.
package slug

import (
	"strings"
	"unicode"
)

// Make lower-cases s and joins its runs of letters and digits with single
// hyphens: "Hello, World!" becomes "hello-world".
func Make(s string) string {
	words := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})

	return strings.Join(words, "-")
}
