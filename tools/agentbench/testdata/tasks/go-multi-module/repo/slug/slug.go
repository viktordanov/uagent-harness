// Package slug makes URL slugs.
package slug

import (
	"strings"
	"unicode"
)

// Make lower-cases s and joins its runs of letters and digits with single
// hyphens: "Hello, World!" becomes "hello-world".
func Make(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}

	return strings.Trim(b.String(), "-")
}
