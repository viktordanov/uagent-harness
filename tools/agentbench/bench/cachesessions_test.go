package bench_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uah/internal/usage/cachestats"
	"github.com/viktordanov/uah/tools/agentbench/bench"
)

func TestCacheSessionsReport(t *testing.T) {
	at := time.Date(2026, 10, 3, 9, 0, 0, 0, time.UTC)
	req := func(start time.Duration, input, cached int64, opener bool) cachestats.Request {
		return cachestats.Request{Start: at.Add(start), End: at.Add(start + 10*time.Second), Model: "m", Effort: "high", Input: input, Cached: cached, Opener: opener}
	}
	reqs := cachestats.Attribute([]cachestats.Request{
		req(0, 12_000, 4_096, true),
		req(20*time.Second, 20_000, 11_904, false),
		req(3*time.Minute, 25_000, 19_968, true), // held after a short pause
		req(2*time.Hour, 30_000, 4_096, true),    // expired after a long one
		req(2*time.Hour+20*time.Second, 35_000, 29_952, false),
	}, cachestats.TTL)

	md := bench.CacheSessionsReport(map[string][]cachestats.Attributed{"0123456789": reqs}, cachestats.TTL, cachestats.APIPrice)

	assert.Contains(t, md, "from 1 sessions, 5 model requests")
	assert.Contains(t, md, "| idle | 21k |")
	assert.Contains(t, md, "2 user messages followed another request of their session; 1 (50%) came more than 30m0s after it")
	assert.Contains(t, md, "| 1–5 min | 1 | 1 | 1 (100%) |")
	assert.Contains(t, md, "| 1–3 h | 1 | 1 | 0 (0%) |")
	assert.Contains(t, md, "| 01234567 | 5 | cache ")
}
