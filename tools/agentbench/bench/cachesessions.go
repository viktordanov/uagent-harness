package bench

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/viktordanov/uah/internal/session"
	"github.com/viktordanov/uah/internal/usage/cachestats"
)

// Real sessions' prompt cache: the accounting of internal/cachestats over
// every session in a uah home, and how the cache fared across the pauses
// between the user's messages, which the benchmark's back-to-back
// messages never have.

// CacheSessions is the cache accounting of each session in stateDir that
// made a model request, keyed by session ID.
func CacheSessions(stateDir string) (map[string][]cachestats.Attributed, error) {
	infos, err := session.Sessions(stateDir)
	if err != nil {
		return nil, err
	}
	out := map[string][]cachestats.Attributed{}
	for _, in := range infos {
		reqs, err := session.CacheStats(stateDir, in.ID)
		if err != nil {
			return nil, fmt.Errorf("session %s: %w", in.ID, err)
		}
		if len(reqs) > 0 {
			out[in.ID] = reqs
		}
	}

	return out, nil
}

// gapBucket is a row of the pause table: the pauses up to max.
type gapBucket struct {
	name string
	max  time.Duration
}

var gapBuckets = []gapBucket{
	{"< 1 min", time.Minute},
	{"1–5 min", 5 * time.Minute},
	{"5–10 min", 10 * time.Minute},
	{"10–30 min", 30 * time.Minute},
	{"30–60 min", time.Hour},
	{"1–3 h", 3 * time.Hour},
	{"3–24 h", 24 * time.Hour},
	{"> 24 h", 1 << 62},
}

// minProbe is the smallest same-key prefix a request must expect for the
// pause table: a smaller one says little about whether the cache held.
const minProbe = 8_000

// CacheSessionsReport is the markdown report of sessions' cache accounting:
// the totals by cause, and per pause before a user's message, how often
// the cache at the message's model and effort still served what it held.
func CacheSessionsReport(sessions map[string][]cachestats.Attributed, ttl time.Duration, price cachestats.Price) string {
	var b strings.Builder
	var all []cachestats.Attributed
	ids := make([]string, 0, len(sessions))
	for id := range sessions {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	for _, id := range ids {
		all = append(all, sessions[id]...)
	}
	sum := cachestats.Summarize(all, price)
	fmt.Fprintf(&b, "# Prompt cache in real sessions\n\nGenerated %s from %d sessions, %d model requests; TTL estimate %s. Usage at $%.2f / $%.3f / $%.2f per million input / cached / output tokens.\n\n%s\n",
		time.Now().UTC().Format(time.DateTime), len(sessions), len(all), ttl, price.Input, price.Cached, price.Output, sum.Line())
	b.WriteString("\n## Missed input by cause\n\n| Cause | Tokens | Share of missed |\n| --- | ---: | ---: |\n")
	for _, m := range sum.Missed {
		fmt.Fprintf(&b, "| %s | %s | %.0f%% |\n", m.Cause, cachestats.Tokens(m.Tokens), 100*float64(m.Tokens)/float64(max(1, sum.MissedTokens())))
	}
	pauses(&b, all, ttl)
	worst(&b, ids, sessions, price)

	return b.String()
}

// pauses tabulates the user's messages by the pause before them: how many,
// and, of those whose model and effort held a prefix of at least minProbe,
// how many found it cached (all but NoiseFloor).
func pauses(b *strings.Builder, all []cachestats.Attributed, ttl time.Duration) {
	type row struct{ n, probed, held int }
	rows := make([]row, len(gapBuckets))
	var messages, expired int
	for i, r := range all {
		if !r.Opener || i == 0 || r.Rewritten || r.Gap <= 0 {
			continue
		}
		messages++
		if r.Gap > ttl {
			expired++
		}
		x := &rows[slices.IndexFunc(gapBuckets, func(g gapBucket) bool { return r.Gap <= g.max })]
		x.n++
		if r.KeyGap == r.Gap && r.Expected >= minProbe {
			x.probed++
			if r.Cached >= r.Expected-cachestats.NoiseFloor {
				x.held++
			}
		}
	}
	fmt.Fprintf(b, "\n## Pauses before a message\n\n%d user messages followed another request of their session; %d (%.0f%%) came more than %s after it. A probe is a message at the same model and effort as the request before it, with at least %dk of it expected cached; it held when all but %d tokens were.\n\n| Pause | Messages | Probes | Held |\n| --- | ---: | ---: | ---: |\n",
		messages, expired, 100*float64(expired)/float64(max(1, messages)), ttl, minProbe/1000, cachestats.NoiseFloor)
	for i, x := range rows {
		if x.n == 0 {
			continue
		}
		held := "-"
		if x.probed > 0 {
			held = fmt.Sprintf("%d (%.0f%%)", x.held, 100*float64(x.held)/float64(x.probed))
		}
		fmt.Fprintf(b, "| %s | %d | %d | %s |\n", gapBuckets[i].name, x.n, x.probed, held)
	}
}

// worst lists the sessions that missed the most.
func worst(b *strings.Builder, ids []string, sessions map[string][]cachestats.Attributed, price cachestats.Price) {
	type ses struct {
		id  string
		sum cachestats.Summary
	}
	var ss []ses
	for _, id := range ids {
		ss = append(ss, ses{id, cachestats.Summarize(sessions[id], price)})
	}
	slices.SortFunc(ss, func(x, y ses) int { return int(y.sum.MissedTokens() - x.sum.MissedTokens()) })
	b.WriteString("\n## Sessions that missed the most\n\n| Session | Requests | Summary |\n| --- | ---: | --- |\n")
	for _, s := range ss[:min(10, len(ss))] {
		fmt.Fprintf(b, "| %s | %d | %s |\n", s.id[:min(8, len(s.id))], s.sum.Requests, s.sum.Line())
	}
}
