package review

// Codex's circuit breaker limits (codex-rs/ext/guardian-reviewer/src/
// circuit_breaker.rs): 3 denials in a row, or 10 in the last 50 reviews.
const (
	MaxConsecutiveDenials = 3
	MaxRecentDenials      = 10
	RecentWindow          = 50
)

// Breaker stops auto-review after too many denials, so the user decides
// instead of the agent looping on refused actions. Unlike Codex, a failed
// review counts as a denial: it denies too. The zero Breaker is closed. It
// is not safe for concurrent use; Reviewer guards its own.
type Breaker struct {
	consecutive int
	recent      []bool
	open        bool
}

// Open reports whether the breaker has tripped.
func (b *Breaker) Open() bool { return b.open }

// Record counts one review and reports whether the breaker is now open.
func (b *Breaker) Record(denied bool) bool {
	b.recent = append(b.recent, denied)
	if len(b.recent) > RecentWindow {
		b.recent = b.recent[1:]
	}
	if !denied {
		b.consecutive = 0

		return b.open
	}
	b.consecutive++
	recent := 0
	for _, d := range b.recent {
		if d {
			recent++
		}
	}
	if b.consecutive >= MaxConsecutiveDenials || recent >= MaxRecentDenials {
		b.open = true
	}

	return b.open
}
