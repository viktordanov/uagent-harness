package textstat

// Truncate shortens s to at most max characters (runes), ending in "…"
// when it was cut.
func Truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}

	return s[:max-1] + "…"
}
