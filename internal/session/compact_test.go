package session_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/engine"
	"github.com/viktordanov/uagent-harness/internal/session"
)

func TestCompact_NeedsTheEngine(t *testing.T) {
	h := newHarness(t, engine.Capabilities{})
	require.ErrorIs(t, h.s.Compact(), session.ErrNoCompaction)
}

func TestCompact_WhenIdleCompactsTheNextRun(t *testing.T) {
	h := newHarness(t, engine.Capabilities{Compaction: true})
	require.NoError(t, h.s.Compact())
	n := h.until(isType[session.Notice]).(session.Notice)
	assert.Equal(t, session.LevelInfo, n.Level)

	_, err := h.s.Submit("go on")
	require.NoError(t, err)
	assert.True(t, h.nextRun().opts.Compact, "the next run compacts first")
}
