package main

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/tui/bubble"
)

func TestPrintExit(t *testing.T) {
	var out bytes.Buffer
	printExit(&out, bubble.Exit{SessionID: "3f2a", Resumable: true, Tokens: core.Tokens{InputTokens: 12_500, CachedInputTokens: 10_000, OutputTokens: 1_200, ReasoningTokens: 800}})
	assert.Equal(t, "Token usage: total=3,700 input=2,500 (+ 10,000 cached) output=1,200 (reasoning 800)\nTo continue this session, run:\n  uah resume 3f2a\n", out.String())

	out.Reset()
	printExit(&out, bubble.Exit{SessionID: "3f2a", Resumable: true})
	assert.Equal(t, "To continue this session, run:\n  uah resume 3f2a\n", out.String(), "no usage line without tokens")

	out.Reset()
	printExit(&out, bubble.Exit{SessionID: "3f2a"})
	assert.Empty(t, out.String(), "a session that never ran is not worth resuming")
}

func TestThousands(t *testing.T) {
	for n, want := range map[int64]string{0: "0", 999: "999", 1000: "1,000", 1234567: "1,234,567", -1234: "-1,234"} {
		assert.Equal(t, want, thousands(n))
	}
}
