package compaction_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent-harness/internal/compaction"
)

func TestSettingsLimit(t *testing.T) {
	tests := []struct {
		name string
		s    compaction.Settings
		want int64
	}{
		{name: "percent of the window", s: compaction.Settings{Percent: 90}, want: 90_000},
		{name: "a lower token limit wins, as in Codex", s: compaction.Settings{Percent: 90, TokenLimit: 50_000}, want: 50_000},
		{name: "a higher token limit does not raise it", s: compaction.Settings{Percent: 90, TokenLimit: 95_000}, want: 90_000},
		{name: "percent 0 turns it off", s: compaction.Settings{TokenLimit: 50_000}, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.s.Limit(100_000))
		})
	}
}

func TestSettingsSummaryPrompt(t *testing.T) {
	assert.Equal(t, compaction.Prompt, compaction.Settings{}.SummaryPrompt(""))
	assert.Equal(t, "Mine.", compaction.Settings{Prompt: " Mine. "}.SummaryPrompt("  "))
	assert.Equal(t, compaction.Prompt+"\n\nThe user asked this summary to focus on:\nthe API", compaction.Settings{}.SummaryPrompt(" the API "))
}

func TestSettingsKeepTokens(t *testing.T) {
	assert.Equal(t, compaction.UserMessageMaxTokens, compaction.Settings{}.KeepTokens())
	assert.Equal(t, 500, compaction.Settings{UserMessageMaxTokens: 500}.KeepTokens())
}

func TestSettingsKeepFor(t *testing.T) {
	assert.Equal(t, compaction.UserMessageMaxTokens, compaction.Settings{}.KeepFor(272_000), "Codex's cap in Codex's window")
	assert.Equal(t, 2_048, compaction.Settings{}.KeepFor(8_192), "a quarter of a small window")
	assert.Equal(t, 6_000, compaction.Settings{UserMessageMaxTokens: 6_000}.KeepFor(8_192), "a configured cap wins")
}
