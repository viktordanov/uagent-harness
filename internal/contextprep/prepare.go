package contextprep

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"unicode/utf8"
)

// The prepared block's tags. The block is one developer message before
// the session's first user message, so the system prompt, and with it the
// prompt cache, stays the same.
const (
	Open  = "<context_preparation>"
	Close = "</context_preparation>"
)

// Size caps: each adapter's block, unless it says otherwise (Limited), and
// the whole prepared block.
const (
	MaxAdapterBytes = 4 << 10
	MaxBytes        = 16 << 10
)

// intro follows Open.
const intro = "uah prepared this when the session started, so you need not look it up again. " +
	"It describes the session as it began; files and git's state may change as you work."

// Limited is an adapter whose block may be longer, or must be shorter,
// than MaxAdapterBytes.
type Limited interface {
	// MaxBytes is the block's cap in bytes.
	MaxBytes() int
}

// Prepare runs the adapters at once and joins their blocks, in the
// adapters' order, into the prepared block: one section per adapter with
// something to say, under its name, each cut to its cap and all of them to
// MaxBytes. It is "" when no adapter has anything to say.
func Prepare(ctx context.Context, f Facts, adapters ...Adapter) string {
	blocks := make([]string, len(adapters))
	var wg sync.WaitGroup
	for i, a := range adapters {
		wg.Go(func() {
			blocks[i] = cut(strings.TrimSpace(a.Prepare(ctx, f)), capOf(a))
		})
	}
	wg.Wait()

	var b strings.Builder
	room := MaxBytes - len(Open) - len(intro) - len(Close) - 4
	for i, a := range adapters {
		if blocks[i] == "" {
			continue
		}
		section := "\n\n## " + a.Name() + "\n" + blocks[i]
		if len(section) > room {
			section = cut(section, room)
		}
		if section == "" {
			break
		}
		b.WriteString(section)
		room -= len(section)
	}
	if b.Len() == 0 {
		return ""
	}

	return Open + "\n" + intro + b.String() + "\n" + Close
}

// capOf is the adapter's cap.
func capOf(a Adapter) int {
	if l, ok := a.(Limited); ok {
		return l.MaxBytes()
	}

	return MaxAdapterBytes
}

// cut shortens text to at most limit bytes, at a line's end when it can,
// with a note that the rest was left out; "" when not even the note fits.
func cut(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	note := fmt.Sprintf("\n(cut: the rest is over %d bytes)", limit)
	keep := limit - len(note)
	if keep <= 0 {
		return ""
	}
	head := text[:keep]
	for !utf8.ValidString(head) {
		head = head[:len(head)-1]
	}
	if i := strings.LastIndexByte(head, '\n'); i > keep/2 {
		head = head[:i]
	}

	return head + note
}

// IsPrepared reports whether a message is a prepared block, which is
// uah's, not the user's. A session from before the developer role has it
// as a user message.
func IsPrepared(text string) bool {
	return strings.HasPrefix(text, Open+"\n")
}
