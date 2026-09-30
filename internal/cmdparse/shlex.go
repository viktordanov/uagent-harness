// Adapted from comex/rust-shlex 2.0.1 (MIT or Apache License 2.0; Copyright
// 2015 Nicholas Allegra), the crate Codex's parse_command uses:
// src/bytes.rs (split and try_join).

package cmdparse

import "strings"

// split splits s into words as POSIX shlex does: quotes group, a
// backslash escapes, and # starts a comment where a word would start. ok is
// false for an unterminated quote or a trailing backslash.
func split(s string) (words []string, ok bool) {
	i := 0
	for {
		for i < len(s) && isBlank(s[i]) {
			i++
		}
		if i < len(s) && s[i] == '#' {
			for i < len(s) && s[i] != '\n' {
				i++
			}

			continue
		}
		if i >= len(s) {
			return words, true
		}
		word, next, wordOK := splitWord(s, i)
		if !wordOK {
			return words, false
		}
		words = append(words, word)
		i = next
	}
}

// splitWord reads one word starting at i and returns where it ended.
func splitWord(s string, i int) (word string, next int, ok bool) {
	var b strings.Builder
	for i < len(s) && !isBlank(s[i]) {
		switch c := s[i]; c {
		case '"':
			end, dq := doubleQuoted(s, i+1, &b)
			if !dq {
				return "", 0, false
			}
			i = end
		case '\'':
			end := strings.IndexByte(s[i+1:], '\'')
			if end < 0 {
				return "", 0, false
			}
			b.WriteString(s[i+1 : i+1+end])
			i += end + 2
		case '\\':
			if i+1 >= len(s) {
				return "", 0, false
			}
			if s[i+1] != '\n' {
				b.WriteByte(s[i+1])
			}
			i += 2
		default:
			b.WriteByte(c)
			i++
		}
	}

	return b.String(), i, true
}

// doubleQuoted reads a double-quoted string's body from i into b and
// returns the index after its closing quote.
func doubleQuoted(s string, i int, b *strings.Builder) (int, bool) {
	for i < len(s) {
		switch c := s[i]; c {
		case '"':
			return i + 1, true
		case '\\':
			if i+1 >= len(s) {
				return 0, false
			}
			switch n := s[i+1]; n {
			case '$', '`', '"', '\\':
				b.WriteByte(n)
			case '\n':
			default:
				b.WriteByte('\\')
				b.WriteByte(n)
			}
			i += 2
		default:
			b.WriteByte(c)
			i++
		}
	}

	return 0, false
}

func isBlank(c byte) bool { return c == ' ' || c == '\t' || c == '\n' }

// join quotes each word where the shell needs it and joins them with
// spaces, as shlex::try_join does.
func join(words []string) string {
	quoted := make([]string, len(words))
	for i, w := range words {
		quoted[i] = quote(w)
	}

	return strings.Join(quoted, " ")
}

// The quoting strategies a chunk of a word allows.
const (
	unquotedOK = 1 << iota
	singleOK
	doubleOK
)

// quote quotes one word: bare when it can be, else in single quotes,
// else in double quotes, splitting the word into chunks when no one
// strategy fits all of it.
func quote(w string) string {
	if w == "" {
		return "''"
	}
	var b strings.Builder
	for w != "" {
		n, strategy := quotingStrategy(w)
		chunk := w[:n]
		w = w[n:]
		switch {
		case strategy&unquotedOK != 0:
			b.WriteString(chunk)
		case strategy&singleOK != 0:
			b.WriteString("'" + chunk + "'")
		default:
			b.WriteByte('"')
			for i := range len(chunk) {
				if strings.IndexByte("$`\"\\", chunk[i]) >= 0 {
					b.WriteByte('\\')
				}
				b.WriteByte(chunk[i])
			}
			b.WriteByte('"')
		}
	}

	return b.String()
}

// quotingStrategy is how long a prefix of w one strategy covers, and the
// strategies that cover it.
func quotingStrategy(w string) (int, int) {
	ok, i := unquotedOK|singleOK|doubleOK, 0
	if w[0] == '^' {
		ok, i = singleOK, 1 // ^ only right after an opening single quote
	}
	for ; i < len(w); i++ {
		cur := ok &^ charDenies(w[i])
		if cur == 0 {
			break
		}
		ok = cur
	}

	return i, best(ok)
}

// best keeps the preferred strategy of those allowed.
func best(ok int) int {
	switch {
	case ok&unquotedOK != 0:
		return unquotedOK
	case ok&singleOK != 0:
		return singleOK
	}

	return doubleOK
}

// charDenies are the strategies that cannot hold c.
func charDenies(c byte) int {
	deny := 0
	if c >= 0x80 || !unquotedChar(c) {
		deny |= unquotedOK
	}
	if c == '\'' || c == '^' || c == '\\' {
		deny |= singleOK
	}
	if c == '`' || c == '$' || c == '!' || c == '^' {
		deny |= doubleOK
	}

	return deny
}

// unquotedChar is a character a word may hold bare.
func unquotedChar(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte("+-./:@]_", c) >= 0
}
