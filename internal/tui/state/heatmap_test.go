package state_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

func TestHeatmap(t *testing.T) {
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC) // a Thursday
	counts := map[string]int{"2026-09-24": 4, "2026-09-21": 1, "2026-09-15": 2}
	assert.Equal(t, `activity · last 3 weeks · 7 runs
Mon ··░
    ·▒·
Wed ···
    ··█
Fri ··
    ··
Sun ··`, state.Heatmap(counts, now, 3))
}
