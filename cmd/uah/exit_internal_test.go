package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/tui/bubble"
	"github.com/viktordanov/uagent-harness/internal/tui/render"
)

func TestPrintExit(t *testing.T) {
	var out bytes.Buffer
	printExit(&out, bubble.Exit{SessionID: "3f2a", Resumable: true, Tokens: core.Tokens{InputTokens: 12_500, CachedInputTokens: 10_000, OutputTokens: 1_200, ReasoningTokens: 800}})
	assert.Equal(t, "Token usage: total=3,700 input=2,500 (+ 10,000 cached) output=1,200 (reasoning 800)\nTo continue this session, run:\nuah resume 3f2a\n", out.String())

	out.Reset()
	printExit(&out, bubble.Exit{SessionID: "3f2a", Resumable: true})
	assert.Equal(t, "To continue this session, run:\nuah resume 3f2a\n", out.String(), "no usage line without tokens")

	out.Reset()
	printExit(&out, bubble.Exit{SessionID: "3f2a", Resumable: true, Queued: 2})
	assert.Equal(t, "Queued messages kept in the session: 2\nTo continue this session, run:\nuah resume 3f2a\n", out.String())

	out.Reset()
	printExit(&out, bubble.Exit{SessionID: "3f2a"})
	assert.Empty(t, out.String(), "a session that never ran is not worth resuming")
}

func TestPrintExitColors(t *testing.T) {
	t.Setenv("CLICOLOR_FORCE", "1")
	t.Setenv("COLORTERM", "truecolor")
	var out bytes.Buffer
	printExit(&out, bubble.Exit{SessionID: "3f2a", Resumable: true, Theme: render.Amber, Tokens: core.Tokens{InputTokens: 10, OutputTokens: 2}})
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	require.Len(t, lines, 3)
	assert.Contains(t, lines[2], "38;2;255;196;0", "the command is in the accent")
	assert.Contains(t, lines[2], "uah resume 3f2a")
	assert.Contains(t, lines[0], "38;2;160;140;100", "labels are dim")
}

func TestThousands(t *testing.T) {
	for n, want := range map[int64]string{0: "0", 999: "999", 1000: "1,000", 1234567: "1,234,567", -1234: "-1,234"} {
		assert.Equal(t, want, thousands(n))
	}
}
