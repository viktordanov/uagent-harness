package usage_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/usage"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)

	return b
}

var captured = time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

// The shape a Pro login returned on 2026-09-24, identifiers replaced: one
// weekly window, sent as the primary one.
func TestParse_RecordedProResponse(t *testing.T) {
	s, err := usage.Parse(fixture(t, "pro_weekly_only.json"), captured)
	require.NoError(t, err)
	assert.Equal(t, "pro", s.Plan)
	assert.Equal(t, captured, s.CapturedAt)
	require.Len(t, s.Limits, 1)
	l, ok := s.Codex()
	require.True(t, ok)
	require.NotNil(t, l.Primary)
	assert.Nil(t, l.Secondary)
	assert.InDelta(t, 22, l.Primary.UsedPercent, 0)
	assert.Equal(t, int64(7*24*60), l.Primary.Minutes)
	assert.Equal(t, time.Unix(1790426679, 0), l.Primary.ResetsAt)
	assert.Equal(t, "weekly", l.Primary.Label(false))
	assert.True(t, *l.Allowed)
	assert.False(t, *l.Reached)
	require.NotNil(t, s.Credits)
	assert.Equal(t, usage.Credits{Balance: "0"}, *s.Credits)
	assert.Empty(t, s.ReachedType)
}

func TestParse_TwoWindowsAndAnAdditionalLimit(t *testing.T) {
	s, err := usage.Parse(fixture(t, "plus_two_windows.json"), captured)
	require.NoError(t, err)
	require.Len(t, s.Limits, 2)
	codex := s.Limits[0]
	assert.Equal(t, "codex", codex.ID)
	assert.Equal(t, "5h", codex.Primary.Label(false))
	assert.Equal(t, "weekly", codex.Secondary.Label(true))
	assert.InDelta(t, 0, codex.Primary.LeftPercent(), 0)
	assert.InDelta(t, 55, codex.Secondary.LeftPercent(), 0)
	assert.True(t, *codex.Reached)
	other := s.Limits[1]
	assert.Equal(t, "codex_other", other.ID)
	assert.Equal(t, "GPT-5.6-Sol", other.Name)
	assert.Nil(t, other.Secondary)
	assert.Equal(t, &usage.Credits{HasCredits: true, Balance: "12.50"}, s.Credits)
	assert.Equal(t, "rate_limit_reached", s.ReachedType)
}

func TestParse_Rejects(t *testing.T) {
	for name, body := range map[string]string{
		"not json": "<html>",
		"empty":    "{}",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := usage.Parse([]byte(body), captured)
			assert.Error(t, err)
		})
	}
}

func TestParse_NullLimitKeepsThePlan(t *testing.T) {
	s, err := usage.Parse([]byte(`{"plan_type":"free","rate_limit":null}`), captured)
	require.NoError(t, err)
	assert.Equal(t, "free", s.Plan)
	l, ok := s.Codex()
	require.True(t, ok)
	assert.Nil(t, l.Primary)
	assert.Nil(t, l.Allowed)
}

func TestWindow_Label(t *testing.T) {
	for minutes, want := range map[int64]string{
		300: "5h", 290: "5h", 1440: "daily", 10080: "weekly", 43200: "monthly", 525600: "annual", 90: "usage", 0: "usage",
	} {
		assert.Equal(t, want, usage.Window{Minutes: minutes}.Label(false), minutes)
	}
	assert.Equal(t, "secondary usage", usage.Window{}.Label(true))
}

func TestWindow_String(t *testing.T) {
	now := time.Date(2026, 9, 24, 8, 0, 0, 0, time.UTC)
	w := usage.Window{UsedPercent: 45, Minutes: 300, ResetsAt: time.Date(2026, 9, 24, 9, 25, 0, 0, time.UTC)}
	assert.Equal(t, "5h 55% left (resets 09:25)", w.String(false, now))
	w = usage.Window{UsedPercent: 130, Minutes: 10080, ResetsAt: time.Date(2026, 9, 26, 15, 44, 0, 0, time.UTC)}
	assert.Equal(t, "weekly 0% left (resets 15:44 on 26 Sep)", w.String(true, now))
	assert.Equal(t, "usage 100% left", usage.Window{UsedPercent: -1}.String(false, now))
}

func TestSnapshot_Stale(t *testing.T) {
	s := usage.Snapshot{CapturedAt: captured}
	assert.False(t, s.Stale(captured.Add(usage.StaleAfter)))
	assert.True(t, s.Stale(captured.Add(usage.StaleAfter+time.Second)))
}
