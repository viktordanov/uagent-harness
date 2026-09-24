package usage_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/usage"
)

func TestRows(t *testing.T) {
	s, err := usage.Parse(fixture(t, "plus_two_windows.json"), captured)
	require.NoError(t, err)
	rows := s.Rows()
	require.Len(t, rows, 3)
	assert.Equal(t, []string{"5h", "weekly", "GPT-5.6-Sol 5h"}, []string{rows[0].Label, rows[1].Label, rows[2].Label})
	assert.Equal(t, "weekly 55% left (resets 09:06 on 27 Sep)", rows[1].Text(captured))

	tight, ok := s.Tightest()
	require.True(t, ok)
	assert.Equal(t, "5h", tight.Label, "the ordinary limit's fullest window")
	assert.True(t, s.Reached())
	at, ok := s.BlockedUntil()
	require.True(t, ok)
	assert.Equal(t, time.Unix(1790300000, 0), at, "the window at 100%")
}

func TestRows_WeeklyAsPrimary(t *testing.T) {
	s, err := usage.Parse(fixture(t, "pro_weekly_only.json"), captured)
	require.NoError(t, err)
	tight, ok := s.Tightest()
	require.True(t, ok)
	assert.Equal(t, "weekly", tight.Label, "named by its length, not its position")
	assert.Equal(t, "78% left", tight.Left())
	assert.False(t, s.Reached())
	at, ok := s.BlockedUntil()
	require.True(t, ok)
	assert.Equal(t, time.Unix(1790426679, 0), at, "not reached: the tightest window's reset")

	_, ok = usage.Snapshot{}.Tightest()
	assert.False(t, ok)
}

func TestBar(t *testing.T) {
	assert.Equal(t, "[███████████░░░░░░░░░]", usage.Bar(55))
	assert.Equal(t, "[░░░░░░░░░░░░░░░░░░░░]", usage.Bar(-3))
	assert.Equal(t, "[████████████████████]", usage.Bar(100))
}

func TestCrossed(t *testing.T) {
	for used, want := range map[float64]float64{10: 0, 75: 75, 89.9: 75, 90: 90, 99: 95, 100: 95} {
		assert.InDelta(t, want, usage.Window{UsedPercent: used}.Crossed(), 0, used)
	}
}

func TestLimitReachedIn(t *testing.T) {
	for name, c := range map[string]struct {
		text   string
		ok     bool
		resets int64
	}{
		"code":          {text: "responses API error usage_limit_reached: The usage limit has been reached", ok: true},
		"status":        {text: "responses API request failed with status 429: The usage limit has been reached", ok: true},
		"body kept":     {text: `responses API request failed with status 429: {"error":{"type":"usage_limit_reached","plan_type":"plus","resets_at":1790300000}}`, ok: true, resets: 1790300000},
		"broken body":   {text: `model failure: usage_limit_reached {"resets_at": 1790300001, `, ok: true, resets: 1790300001},
		"another 429":   {text: "responses API request failed with status 429: Rate limit reached for requests", ok: false},
		"another error": {text: "codex credentials rejected", ok: false},
	} {
		r, ok := usage.LimitReachedIn(c.text)
		assert.Equal(t, c.ok, ok, name)
		if c.resets != 0 {
			assert.Equal(t, time.Unix(c.resets, 0), r.ResetsAt, name)
		} else {
			assert.True(t, r.ResetsAt.IsZero(), name)
		}
	}
}
