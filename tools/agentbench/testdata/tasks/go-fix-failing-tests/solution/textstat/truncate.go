package textstat

// Truncate shortens s to at most max characters (runes), ending in "…"
// when it was cut.
func Truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}

	return string(r[:max-1]) + "…"
}
