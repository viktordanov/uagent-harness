package store

// Clamp limits n to [lo, hi].
func Clamp(n, lo, hi int) int {
	if n < lo {
		n = lo
	}
	if n > hi {
		n = hi
	}

	return n
}

// Sign returns -1, 0, or 1.
func Sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	default:
		return 0
	}
}
