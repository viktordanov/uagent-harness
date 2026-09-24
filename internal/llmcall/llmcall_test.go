package llmcall_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/unreallabsai/unreal-agent/harness/llm"
	"github.com/unreallabsai/unreal-agent/harness/llm/clients/openai"

	"github.com/viktordanov/uagent-harness/internal/llmcall"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

func client(t *testing.T, srv *fakellm.Server) llm.Adapter {
	t.Helper()
	attempts := 1
	c, err := openai.NewClient(openai.Config{APIKey: "test-key", BaseURL: srv.URL, MaxAttempts: &attempts})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.Close() })

	return c
}

func TestCall_SendsMessagesAndReturnsText(t *testing.T) {
	srv := fakellm.New(t, fakellm.Reply{Text: "the summary"})
	res, err := llmcall.Call(context.Background(), client(t, srv), llmcall.Request{
		Model: "gpt-test", Effort: llm.ReasoningEffortLow, Instructions: "Be brief.",
		Input: []llm.Item{llmcall.Message(llm.RoleUser, "first"), llmcall.Message(llm.RoleUser, "second")},
	})
	require.NoError(t, err)
	assert.Equal(t, "the summary", res.Text)
	assert.Equal(t, int64(100), res.Usage.InputTokens)
	reqs := srv.Requests()
	require.Len(t, reqs, 1)
	assert.Equal(t, "gpt-test", reqs[0].Model)
	assert.Equal(t, "low", reqs[0].Effort)
	assert.Equal(t, "Be brief.", reqs[0].System)
	assert.Equal(t, []string{"first", "second"}, reqs[0].UserTexts)
	assert.Empty(t, reqs[0].Tools)
}

func TestCall_NoText(t *testing.T) {
	srv := fakellm.New(t, fakellm.Reply{Commands: []string{"ls"}})
	_, err := llmcall.Call(context.Background(), client(t, srv), llmcall.Request{
		Model: "gpt-test", Input: []llm.Item{llmcall.Message(llm.RoleUser, "hi")},
	})
	require.ErrorIs(t, err, llmcall.ErrNoText)
}

func TestCall_Timeout(t *testing.T) {
	gate := make(chan struct{})
	t.Cleanup(func() { close(gate) })
	srv := fakellm.New(t, fakellm.Reply{Text: "late", Gate: gate})
	start := time.Now()
	_, err := llmcall.Call(context.Background(), client(t, srv), llmcall.Request{
		Model: "gpt-test", Input: []llm.Item{llmcall.Message(llm.RoleUser, "hi")}, Timeout: 200 * time.Millisecond,
	})
	require.Error(t, err)
	assert.Less(t, time.Since(start), 10*time.Second)
}
