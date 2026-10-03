package cachestats

import (
	"slices"
	"time"
)

// Cause is why input missed the prompt cache.
type Cause string

const (
	// CauseCold is the session's first request: nothing of it was sent before.
	CauseCold Cause = "cold"
	// CauseEffort is input sent before only at another effort, or whose
	// cache at this effort expired while the session used another one.
	CauseEffort Cause = "effort"
	// CauseModel is input sent before only to another model.
	CauseModel Cause = "model"
	// CauseIdle is input whose cache expired in a pause: the request came
	// longer than the TTL after the one before it.
	CauseIdle Cause = "idle"
	// CauseRewrite is the first request after a compaction or a rewind: the
	// history changed, so the prompt is new past the instructions.
	CauseRewrite Cause = "compaction"
	// CauseOther is a miss the model does not explain: the provider evicted
	// or did not route to the cache.
	CauseOther Cause = "other"
)

// Causes lists every cause in the order a summary shows them.
var Causes = []Cause{CauseEffort, CauseIdle, CauseCold, CauseRewrite, CauseModel, CauseOther}

// TTL is the estimated time an idle prompt cache lives. OpenAI documents 5
// to 10 minutes of inactivity, up to an hour off-peak.
const TTL = 30 * time.Minute

// NoiseFloor is the miss a request may have without counting: the
// provider caches in blocks and from breakpoints, so a few blocks of a
// prefix often miss.
const NoiseFloor = 1024

// Request is one model request of a session, in order.
type Request struct {
	Start, End time.Time
	Model      string
	// Effort is the effort the request carried, which keys its cache. With
	// effort updates it is the session's base effort, whatever effort a
	// configuration update in the history set, so a switch keeps the cache.
	Effort string
	// Input includes Cached; Output includes the reasoning.
	Input, Cached, Output int64
	// Rewritten is set when the history was compacted or rewound since the
	// request before.
	Rewritten bool
	// Opener is set for the first request after a user's message.
	Opener bool
}

// Key is the request's prompt cache: its model and effort.
func (r Request) Key() string { return r.Model + "/" + r.Effort }

// Miss is input of a request that missed the cache, by cause.
type Miss struct {
	Cause  Cause `json:"cause"`
	Tokens int64 `json:"tokens"`
}

// Attributed is a request with its cache accounting.
type Attributed struct {
	Request

	// Expected is the input cached at this request's model and effort:
	// the longest prompt sent before at the same key, in whole blocks.
	Expected int64
	// Reachable is the input sent before at any key: what one cache would
	// have served. Input past it is new and cannot be cached.
	Reachable int64
	// Gap is the time since the request before; KeyGap since the last one
	// at the same key (0 when none).
	Gap, KeyGap time.Duration
	Misses      []Miss
}

// Missed is the request's missed input.
func (a Attributed) Missed() int64 {
	var n int64
	for _, m := range a.Misses {
		n += m.Tokens
	}

	return n
}

// Attribute accounts for each request's cached input with a Cache, and
// attributes the input it could have found cached and did not:
//
//   - the session's first request: all its uncached input is cold;
//   - the first request after a compaction or rewind: all of it is compaction;
//   - after a pause longer than ttl since the request before: all of it is idle;
//   - otherwise the part sent before only at another key is effort, or
//     model when the request before went to another model; the rest is the
//     same when this key's cache sat unused longer than ttl (it expired
//     while the other key served), else other.
//
// A request whose miss is at most NoiseFloor has none.
func Attribute(reqs []Request, ttl time.Duration) []Attributed {
	out := make([]Attributed, 0, len(reqs))
	var cache *Cache
	last := map[string]time.Time{} // the last request's end, per key
	for i, r := range reqs {
		a := Attributed{Request: r}
		uncached := max(0, r.Input-r.Cached)
		switch {
		case i == 0:
			cache = NewCache(r.Cached)
			a.Misses = misses(Miss{CauseCold, uncached})
		case r.Rewritten:
			cache.Reset()
			clear(last)
			a.Gap = r.Start.Sub(reqs[i-1].End)
			a.Misses = misses(Miss{CauseRewrite, uncached})
		default:
			a.Gap = r.Start.Sub(reqs[i-1].End)
			if t, ok := last[r.Key()]; ok {
				a.KeyGap = r.Start.Sub(t)
			}
			a.Expected, a.Reachable = cache.Hit(r.Key(), r.Input), cache.Longest(r.Input)
			a.Misses = attribute(a, ttl, r.Model != reqs[i-1].Model)
		}
		if i == 0 || r.Rewritten {
			a.Expected, a.Reachable = cache.Hit(r.Key(), r.Input), cache.Longest(r.Input)
		}
		cache.Sent(r.Key(), r.Input)
		last[r.Key()] = r.End
		out = append(out, a)
	}

	return out
}

// attribute splits a's miss, with modelChanged set when the request before
// went to another model.
func attribute(a Attributed, ttl time.Duration, modelChanged bool) []Miss {
	missed := a.Reachable - a.Cached
	if missed <= NoiseFloor {
		return nil
	}
	if a.Gap > ttl {
		return []Miss{{CauseIdle, missed}}
	}
	switched := CauseEffort
	if modelChanged {
		switched = CauseModel
	}
	other := max(0, a.Expected-a.Cached)
	if a.KeyGap > ttl || modelChanged {
		// This key's cache expired while another key served, or it is
		// another model's, whose cache shares not even the base.
		other = 0
	}

	return misses(Miss{switched, missed - other}, Miss{CauseOther, other})
}

// misses keeps the misses over NoiseFloor, or none when their sum is not.
func misses(ms ...Miss) []Miss {
	var sum int64
	for _, m := range ms {
		sum += m.Tokens
	}
	if sum <= NoiseFloor {
		return nil
	}

	return slices.DeleteFunc(ms, func(m Miss) bool { return m.Tokens <= 0 })
}
