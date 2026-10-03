package cachestats

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Price weighs tokens as the API bills them, in dollars per million: what
// docs/design/adaptive-effort-costs.md calls usage. A subscription's
// weighting is unpublished, so a cost in these terms is an estimate.
type Price struct {
	Input, Cached, Output float64
}

// APIPrice is the API's price of the models uah's cost study used.
var APIPrice = Price{Input: 1.25, Cached: 0.125, Output: 10}

// Summary totals a session's cache accounting.
type Summary struct {
	Requests int   `json:"requests"`
	Input    int64 `json:"input"`
	Cached   int64 `json:"cached"`
	Output   int64 `json:"output"`
	// Missed is the input that could have been cached, by cause in the
	// order of Causes; a cause without a miss is left out.
	Missed []Miss `json:"missed"`
	// MissedShare is what the missed input cost, at the uncached price less
	// the cached, as a share of the session's usage at Price.
	MissedShare float64 `json:"missed_share"`
	// IdleGaps counts the requests that missed after a pause longer than
	// the TTL, and LongestGap is the longest such pause.
	IdleGaps   int           `json:"idle_gaps"`
	LongestGap time.Duration `json:"-"`
}

// Summarize totals reqs, with the costs at p.
func Summarize(reqs []Attributed, p Price) Summary {
	s := Summary{Missed: []Miss{}}
	by := map[Cause]int64{}
	for _, r := range reqs {
		s.Requests++
		s.Input += r.Input
		s.Cached += r.Cached
		s.Output += r.Output
		for _, m := range r.Misses {
			by[m.Cause] += m.Tokens
			if m.Cause == CauseIdle {
				s.IdleGaps++
				s.LongestGap = max(s.LongestGap, r.Gap)
			}
		}
	}
	var missed int64
	for _, c := range Causes {
		if by[c] > 0 {
			s.Missed = append(s.Missed, Miss{c, by[c]})
			missed += by[c]
		}
	}
	usage := float64(s.Input-s.Cached)*p.Input + float64(s.Cached)*p.Cached + float64(s.Output)*p.Output
	if usage > 0 {
		s.MissedShare = float64(missed) * (p.Input - p.Cached) / usage
	}

	return s
}

// CachedShare is the share of the input served from the cache.
func (s Summary) CachedShare() float64 {
	if s.Input == 0 {
		return 0
	}

	return float64(s.Cached) / float64(s.Input)
}

// MissedTokens is the input that could have been cached.
func (s Summary) MissedTokens() int64 {
	var n int64
	for _, m := range s.Missed {
		n += m.Tokens
	}

	return n
}

// causeLabels are the causes as a summary names them.
var causeLabels = map[Cause]string{
	CauseCold: "cold start", CauseEffort: "effort switches", CauseModel: "model switches",
	CauseIdle: "idle", CauseRewrite: "compaction", CauseOther: "other",
}

// Line is the summary in one line, such as "cache 88% · missed 41k: effort
// switches 28k, idle 9k, cold start 4k · ≈6% of usage (API-price
// estimate)". The share is what the missed input cost, as a share of all
// the session's usage, weighted at API prices.
func (s Summary) Line() string {
	if s.Requests == 0 {
		return "cache: no model requests yet"
	}
	line := fmt.Sprintf("cache %.0f%%", 100*s.CachedShare())
	if len(s.Missed) == 0 {
		return line + " · no misses"
	}
	parts := make([]string, len(s.Missed))
	for i, m := range s.Missed {
		parts[i] = causeLabels[m.Cause] + " " + Tokens(m.Tokens)
	}

	share := fmt.Sprintf("≈%.0f%%", 100*s.MissedShare)
	if s.MissedShare < 0.005 {
		share = "<1%"
	}

	return fmt.Sprintf("%s · missed %s: %s · %s of usage (API-price estimate)", line, Tokens(s.MissedTokens()), strings.Join(parts, ", "), share)
}

// Tokens formats a token count short: 950, 4k, 41k, 1.2M.
func Tokens(n int64) string {
	switch {
	case n < 1000:
		return strconv.FormatInt(n, 10)
	case n < 1_000_000:
		return fmt.Sprintf("%.0fk", float64(n)/1000)
	}

	return fmt.Sprintf("%.1fM", float64(n)/1e6)
}
