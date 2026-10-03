package cachestats_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/internal/usage/cachestats"
)

var t0 = time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)

// seq builds requests that each start when the one before ended, plus
// pause, and take ten seconds.
type seq struct {
	at   time.Time
	reqs []cachestats.Request
}

func (s *seq) add(pause time.Duration, model, effort string, input, cached int64) *cachestats.Request {
	if s.at.IsZero() {
		s.at = t0
	}
	start := s.at.Add(pause)
	s.at = start.Add(10 * time.Second)
	s.reqs = append(s.reqs, cachestats.Request{Start: start, End: s.at, Model: model, Effort: effort, Input: input, Cached: cached, Output: 500})

	return &s.reqs[len(s.reqs)-1]
}

func misses(a cachestats.Attributed) map[cachestats.Cause]int64 {
	out := map[cachestats.Cause]int64{}
	for _, m := range a.Misses {
		out[m.Cause] += m.Tokens
	}

	return out
}

func TestAttribute_SameEffort(t *testing.T) {
	var s seq
	s.add(0, "m", "high", 12_000, 4_096)
	s.add(0, "m", "high", 20_000, 11_904)
	s.add(time.Minute, "m", "high", 30_000, 19_968)
	got := cachestats.Attribute(s.reqs, cachestats.TTL)

	assert.Equal(t, map[cachestats.Cause]int64{cachestats.CauseCold: 7_904}, misses(got[0]), "the first request's uncached input is cold")
	assert.Equal(t, int64(11_904), got[1].Expected, "the prompt sent before, in whole blocks")
	assert.Empty(t, got[1].Misses, "new content is not a miss")
	assert.Empty(t, got[2].Misses)
	assert.Equal(t, time.Minute, got[2].Gap)
}

func TestAttribute_EffortSwitch(t *testing.T) {
	var s seq
	s.add(0, "m", "high", 12_000, 4_096)    // the user's message
	s.add(0, "m", "low", 20_000, 4_096)     // a follow-up: only the base was cached at low
	s.add(0, "m", "low", 30_000, 19_968)    // low's cache now holds the follow-up
	s.add(0, "m", "high", 40_000, 11_904)   // the next message: high holds only the first prompt
	s.add(0, "m", "high", 45_000, 39_936)   // back on high's cache
	s.add(0, "m", "medium", 50_000, 44_800) // a third effort finding the prefix: the provider shared it
	got := cachestats.Attribute(s.reqs, cachestats.TTL)

	assert.Equal(t, int64(4_096), got[1].Expected, "low has only the cross-session base")
	assert.Equal(t, map[cachestats.Cause]int64{cachestats.CauseEffort: 11_904 - 4_096}, misses(got[1]), "the prefix existed only at high")
	assert.Empty(t, got[2].Misses)
	assert.Equal(t, map[cachestats.Cause]int64{cachestats.CauseEffort: 29_952 - 11_904}, misses(got[3]), "the follow-ups' work, sent only at low")
	assert.Empty(t, got[4].Misses)
	assert.Empty(t, got[5].Misses, "cached beyond what its key held is no miss")
}

func TestAttribute_IdleGap(t *testing.T) {
	var s seq
	s.add(0, "m", "high", 12_000, 4_096)
	s.add(0, "m", "high", 20_000, 11_904)
	s.add(2*time.Hour, "m", "high", 25_000, 4_096)  // the cache expired
	s.add(2*time.Hour, "m", "high", 30_000, 24_960) // it held this time
	got := cachestats.Attribute(s.reqs, cachestats.TTL)

	assert.Equal(t, map[cachestats.Cause]int64{cachestats.CauseIdle: 19_968 - 4_096}, misses(got[2]))
	assert.Equal(t, 2*time.Hour, got[2].Gap)
	assert.Empty(t, got[3].Misses, "a long pause without a miss costs nothing")

	sum := cachestats.Summarize(got, cachestats.APIPrice)
	assert.Equal(t, 1, sum.IdleGaps)
	assert.Equal(t, 2*time.Hour, sum.LongestGap)
}

func TestAttribute_ShortGapIsOther(t *testing.T) {
	var s seq
	s.add(0, "m", "high", 12_000, 4_096)
	s.add(5*time.Minute, "m", "high", 20_000, 4_096)
	got := cachestats.Attribute(s.reqs, cachestats.TTL)

	assert.Equal(t, map[cachestats.Cause]int64{cachestats.CauseOther: 11_904 - 4_096}, misses(got[1]), "within the TTL the provider evicted it")
}

func TestAttribute_EffortCacheExpiredMeanwhile(t *testing.T) {
	var s seq
	s.add(0, "m", "high", 12_000, 4_096)
	for range 4 {
		s.add(10*time.Minute, "m", "low", 20_000, 19_968) // follow-ups kept low's cache warm for 40 minutes
	}
	s.add(time.Minute, "m", "high", 25_000, 4_096)
	got := cachestats.Attribute(s.reqs, cachestats.TTL)

	last := got[len(got)-1]
	assert.Greater(t, last.KeyGap, cachestats.TTL)
	assert.Equal(t, map[cachestats.Cause]int64{cachestats.CauseEffort: 19_968 - 4_096}, misses(last),
		"high's cache expired while low served: the switch's cost, not a pause")
}

func TestAttribute_ModelChange(t *testing.T) {
	var s seq
	s.add(0, "a", "high", 12_000, 4_096)
	s.add(0, "a", "high", 20_000, 11_904)
	s.add(0, "b", "high", 25_000, 0)
	got := cachestats.Attribute(s.reqs, cachestats.TTL)

	assert.Equal(t, map[cachestats.Cause]int64{cachestats.CauseModel: 19_968}, misses(got[2]))
}

func TestAttribute_Compaction(t *testing.T) {
	var s seq
	s.add(0, "m", "high", 12_000, 4_096)
	s.add(0, "m", "high", 90_000, 11_904)
	s.add(0, "m", "high", 15_000, 4_096).Rewritten = true // the summary replaced the history
	s.add(0, "m", "low", 16_000, 4_096)                   // low lost its prefix to the compaction, then to the switch
	s.add(0, "m", "high", 17_000, 14_976)
	got := cachestats.Attribute(s.reqs, cachestats.TTL)

	assert.Equal(t, map[cachestats.Cause]int64{cachestats.CauseRewrite: 15_000 - 4_096}, misses(got[2]))
	assert.Equal(t, map[cachestats.Cause]int64{cachestats.CauseEffort: 14_976 - 4_096}, misses(got[3]), "after the compaction only high held the new history")
	assert.Equal(t, int64(14_976), got[4].Expected, "the history before the compaction no longer counts")
	assert.Empty(t, got[4].Misses)
}

func TestAttribute_NoiseFloor(t *testing.T) {
	var s seq
	s.add(0, "m", "high", 4_500, 3_712) // a cold start of 788 tokens
	s.add(0, "m", "high", 20_000, 3_712)
	s.add(0, "m", "high", 21_000, 19_200) // 768 short of the 19,968 expected
	got := cachestats.Attribute(s.reqs, cachestats.TTL)

	assert.Empty(t, got[0].Misses)
	assert.Equal(t, int64(4_480-3_712), got[1].Reachable-got[1].Cached, "the miss is under the floor")
	assert.Empty(t, got[1].Misses)
	assert.Empty(t, got[2].Misses)
}

func TestSummarize(t *testing.T) {
	var s seq
	s.add(0, "m", "high", 12_000, 4_096)
	s.add(0, "m", "low", 40_000, 4_096)
	s.add(2*time.Hour, "m", "low", 50_000, 4_096)
	sum := cachestats.Summarize(cachestats.Attribute(s.reqs, cachestats.TTL), cachestats.APIPrice)

	require.Equal(t, []cachestats.Miss{{cachestats.CauseEffort, 7_808}, {cachestats.CauseIdle, 35_840}, {cachestats.CauseCold, 7_904}}, sum.Missed, "in the order of Causes")
	assert.Equal(t, int64(102_000), sum.Input)
	assert.InDelta(t, 0.12, sum.CachedShare(), 0.01)
	// 51,552 missed at $1.125 against 89,712 uncached at $1.25, 12,288
	// cached at $0.125, and 1,500 output at $10.
	assert.InDelta(t, 51_552*1.125/(89_712*1.25+12_288*0.125+1_500*10), sum.MissedShare, 1e-9)
	assert.Equal(t, "cache 12% · missed 52k: effort switches 8k, idle 36k, cold start 8k · ≈45% of usage (API-price estimate)", sum.Line())

	assert.Equal(t, "cache: no model requests yet", cachestats.Summarize(nil, cachestats.APIPrice).Line())
	var warm seq
	warm.add(0, "m", "high", 4_000, 3_712)
	assert.Equal(t, "cache 93% · no misses", cachestats.Summarize(cachestats.Attribute(warm.reqs, cachestats.TTL), cachestats.APIPrice).Line())
}

func TestCache(t *testing.T) {
	c := cachestats.NewCache(1_000)
	assert.Equal(t, int64(896), c.Hit("high", 50_000), "the base, in whole blocks")
	c.Sent("high", 30_000)
	assert.Equal(t, int64(29_952), c.Hit("high", 50_000))
	assert.Equal(t, int64(896), c.Hit("low", 50_000))
	assert.Equal(t, int64(29_952), c.Longest(50_000))
	assert.Equal(t, int64(19_968), c.Hit("high", 20_000), "at most the request's input")
	c.Reset()
	assert.Equal(t, int64(896), c.Longest(50_000))
}
