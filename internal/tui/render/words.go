package render

import (
	"unicode"
	"unicode/utf8"
)

// seg is a run of a diff line's text; changed runs get the word tint.
type seg struct {
	text    string
	changed bool
}

// maxWordTokens bounds the word diff's work per line pair.
const maxWordTokens = 256

// wordDiff marks the words that differ between a removed line and the
// added line that replaced it, as Claude Code highlights them. It returns
// nil segments when the lines share too little for the marks to help.
func wordDiff(before, after string) (beforeSegs, afterSegs []seg) {
	a, b := wordTokens(before), wordTokens(after)
	if len(a) > maxWordTokens || len(b) > maxWordTokens {
		return nil, nil
	}
	keepA, keepB := lcs(a, b)
	if shared(a, keepA) < 0.4*max(shared(a, nil), shared(b, nil)) {
		return nil, nil
	}

	return segments(a, keepA), segments(b, keepB)
}

// wordTokens splits text into words (letters, digits, underscores), runs of
// spaces, and single other characters.
func wordTokens(s string) []string {
	var out []string
	for s != "" {
		r, size := utf8.DecodeRuneInString(s)
		n := size
		switch {
		case isWord(r):
			n = runLength(s, isWord)
		case r == ' ':
			n = runLength(s, func(r rune) bool { return r == ' ' })
		}
		out = append(out, s[:n])
		s = s[n:]
	}

	return out
}

func isWord(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

func runLength(s string, in func(rune) bool) int {
	n := 0
	for n < len(s) {
		r, size := utf8.DecodeRuneInString(s[n:])
		if !in(r) {
			break
		}
		n += size
	}

	return n
}

// lcs marks the tokens of a and b in their longest common subsequence.
func lcs(a, b []string) (keepA, keepB []bool) {
	n, m := len(a), len(b)
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else {
				dp[i][j] = max(dp[i+1][j], dp[i][j+1])
			}
		}
	}
	keepA, keepB = make([]bool, n), make([]bool, m)
	for i, j := 0, 0; i < n && j < m; {
		switch {
		case a[i] == b[j]:
			keepA[i], keepB[j] = true, true
			i, j = i+1, j+1
		case dp[i+1][j] >= dp[i][j+1]:
			i++
		default:
			j++
		}
	}

	return keepA, keepB
}

// shared is how many bytes of the kept tokens (all of them with a nil
// keep) are not spaces.
func shared(toks []string, keep []bool) float64 {
	n := 0
	for i, t := range toks {
		if (keep == nil || keep[i]) && t[0] != ' ' {
			n += len(t)
		}
	}

	return float64(n)
}

// segments joins tokens into runs of kept and changed text.
func segments(toks []string, keep []bool) []seg {
	var out []seg
	for i, t := range toks {
		changed := !keep[i]
		if n := len(out); n > 0 && out[n-1].changed == changed {
			out[n-1].text += t

			continue
		}
		out = append(out, seg{text: t, changed: changed})
	}

	return out
}
