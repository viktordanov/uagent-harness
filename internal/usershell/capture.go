package usershell

import (
	"fmt"
	"sync"
	"unicode/utf8"
)

// capture collects a command's output (stdout and stderr interleaved, as
// Codex's aggregated output) with bounded memory: the first and the last
// keep bytes (keep must be at least 4 times the limit bounded is called
// with). Each write also goes to stream when it is set.
type capture struct {
	mu     sync.Mutex
	keep   int
	head   []byte
	tail   []byte
	total  int64
	stream func(string)
}

func (c *capture) Write(p []byte) (int, error) {
	c.mu.Lock()
	c.total += int64(len(p))
	rest := p
	if room := c.keep - len(c.head); room > 0 {
		n := min(room, len(rest))
		c.head = append(c.head, rest[:n]...)
		rest = rest[n:]
	}
	if len(rest) > 0 {
		c.tail = append(c.tail, rest...)
		if over := len(c.tail) - c.keep; over > 0 {
			c.tail = append(c.tail[:0], c.tail[over:]...)
		}
	}
	c.mu.Unlock()
	if c.stream != nil {
		c.stream(string(p))
	}

	return len(p), nil
}

// bounded is the output cut to limit characters: its first and last half,
// with the Bash tool's marker for what was left out in between
// (operation.BoundOutput in the runner).
func (c *capture) bounded(limit int) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	// keep is at least 4 bytes per character of limit, so when bytes were
	// dropped in the middle, what is kept has more than limit characters.
	whole := string(c.head) + string(c.tail)
	if utf8.RuneCountInString(whole) <= limit {
		return string([]rune(whole)) // valid UTF-8
	}
	head, headSize := firstRunes(whole, limit/2)
	tail, tailSize := lastRunes(whole, limit-limit/2)

	return fmt.Sprintf("%s...%d bytes truncated...%s", head, c.total-int64(headSize+tailSize), tail)
}

// firstRunes returns the first n runes of s and their size in bytes.
func firstRunes(s string, n int) (string, int) {
	end := 0
	for end < len(s) && n > 0 {
		_, size := utf8.DecodeRuneInString(s[end:])
		end += size
		n--
	}

	return string([]rune(s[:end])), end
}

// lastRunes returns the last n runes of s and their size in bytes.
func lastRunes(s string, n int) (string, int) {
	start := len(s)
	for start > 0 && n > 0 {
		_, size := utf8.DecodeLastRuneInString(s[:start])
		start -= size
		n--
	}

	return string([]rune(s[start:])), len(s) - start
}
