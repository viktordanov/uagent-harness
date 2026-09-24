package usage_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/usage"
)

// recordedHeaders are the x-codex-* names a /responses call returned on
// 2026-09-24 for a Pro login, with values like the /wham/usage read.
func recordedHeaders() http.Header {
	h := http.Header{}
	for k, v := range map[string]string{
		"x-codex-active-limit":                         "codex",
		"x-codex-credits-balance":                      "0",
		"x-codex-credits-has-credits":                  "False",
		"x-codex-credits-unlimited":                    "False",
		"x-codex-plan-type":                            "pro",
		"x-codex-primary-over-secondary-limit-percent": "0",
		"x-codex-primary-reset-after-seconds":          "155800",
		"x-codex-primary-reset-at":                     "1790426679",
		"x-codex-primary-used-percent":                 "22",
		"x-codex-primary-window-minutes":               "10080",
		"x-codex-secondary-reset-after-seconds":        "0",
		"x-codex-secondary-reset-at":                   "",
		"x-codex-secondary-used-percent":               "0",
		"x-codex-secondary-window-minutes":             "0",
		"x-codex-turn-state":                           "opaque",
	} {
		h.Set(k, v)
	}

	return h
}

func TestParseHeaders_Recorded(t *testing.T) {
	s, ok := usage.ParseHeaders(recordedHeaders(), captured)
	require.True(t, ok)
	assert.Equal(t, "pro", s.Plan)
	require.Len(t, s.Limits, 1)
	l := s.Limits[0]
	assert.Equal(t, "codex", l.ID)
	assert.Equal(t, &usage.Window{UsedPercent: 22, Minutes: 10080, ResetsAt: time.Unix(1790426679, 0)}, l.Primary)
	assert.Nil(t, l.Secondary, "an all-zero window is dropped, as Codex does")
	assert.Equal(t, &usage.Credits{Balance: "0"}, s.Credits)
}

func TestParseHeaders_ExtraLimitFamilies(t *testing.T) {
	h := http.Header{}
	h.Set("X-Codex-Primary-Used-Percent", "12.5")
	h.Set("X-Codex-Primary-Window-Minutes", "300")
	h.Set("X-Codex-Secondary-Primary-Used-Percent", "80")
	h.Set("X-Codex-Secondary-Primary-Window-Minutes", "1440")
	h.Set("X-Codex-Bengalfox-Primary-Used-Percent", "40")
	h.Set("X-Codex-Bengalfox-Limit-Name", "gpt-5.2-codex-sonic")
	h.Set("X-Codex-Rate-Limit-Reached-Type", "rate_limit_reached")
	s, ok := usage.ParseHeaders(h, captured)
	require.True(t, ok)
	require.Len(t, s.Limits, 3)
	assert.Equal(t, "codex", s.Limits[0].ID)
	assert.Equal(t, "5h", s.Limits[0].Primary.Label(false))
	assert.Equal(t, "codex_bengalfox", s.Limits[1].ID)
	assert.Equal(t, "gpt-5.2-codex-sonic", s.Limits[1].Name)
	assert.Equal(t, "codex_secondary", s.Limits[2].ID)
	assert.Equal(t, "daily", s.Limits[2].Primary.Label(false))
	assert.Nil(t, s.Credits)
	assert.Equal(t, "rate_limit_reached", s.ReachedType)
}

func TestParseHeaders_NoneOrInvalid(t *testing.T) {
	_, ok := usage.ParseHeaders(http.Header{"Content-Type": {"text/event-stream"}}, captured)
	assert.False(t, ok)
	h := http.Header{}
	h.Set("x-codex-primary-used-percent", "NaN")
	_, ok = usage.ParseHeaders(h, captured)
	assert.False(t, ok)
}

func TestParseLimitReached(t *testing.T) {
	r, ok := usage.ParseLimitReached([]byte(`{"error":{"type":"usage_limit_reached","plan_type":"plus","resets_at":1790300000}}`))
	require.True(t, ok)
	assert.Equal(t, usage.LimitReached{Plan: "plus", ResetsAt: time.Unix(1790300000, 0)}, r)
	_, ok = usage.ParseLimitReached([]byte(`{"error":{"type":"usage_not_included"}}`))
	assert.False(t, ok)
	_, ok = usage.ParseLimitReached([]byte(`nope`))
	assert.False(t, ok)
}

func TestTransport_ObservesEachResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		for k, v := range recordedHeaders() {
			w.Header()[k] = v
		}
		_, _ = w.Write([]byte("data: {}\n\n"))
	}))
	t.Cleanup(srv.Close)
	var got []usage.Snapshot
	client := &http.Client{Transport: usage.Transport{
		Observe: func(s usage.Snapshot) { got = append(got, s) },
		Now:     func() time.Time { return captured },
	}}
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL, http.NoBody)
	require.NoError(t, err)
	resp, err := client.Do(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Len(t, got, 1)
	assert.Equal(t, captured, got[0].CapturedAt)
	assert.InDelta(t, 22, got[0].Limits[0].Primary.UsedPercent, 0)
}
