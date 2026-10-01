// Package wrap wraps text to a width.
package wrap

import "strings"

// Lines breaks text into lines of at most width bytes, breaking between
// words; a word longer than width gets a line of its own.
func Lines(text string, width int) []string {
	var lines []string
	line := ""
	for _, w := range strings.Fields(text) {
		switch {
		case line == "":
			line = w
		case len(line)+1+len(w) < width:
			line += " " + w
		default:
			lines = append(lines, line)
			line = w
		}
	}
	if line != "" {
		lines = append(lines, line)
	}

	return lines
}
