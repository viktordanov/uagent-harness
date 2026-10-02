package embedded

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah-core/harness/llm"
	"github.com/viktordanov/uah-core/harness/operation"
	"github.com/viktordanov/uah-core/harness/session"
	"github.com/viktordanov/uah-core/harness/sessionstore"
	"github.com/viktordanov/uah-core/harness/tool"
)

// countingGate decides each call by its arguments and counts its
// decisions; "panic" panics and "wait" waits for ctx to end.
type countingGate struct {
	decided *atomic.Int32
}

func (g countingGate) Translate(tc tool.Context, call llm.ToolCall) tool.CallStatus {
	return g.decide(context.Background(), call)(tc)
}

func (countingGate) TranslateResult(callID string, _ tool.CallStatus, _ []operation.Operation) (llm.ToolResult, error) {
	return llm.ToolResult{CallID: callID}, nil
}

func (g countingGate) decide(ctx context.Context, call llm.ToolCall) submit {
	g.decided.Add(1)
	switch call.Arguments {
	case "panic":
		panic("boom")
	case "wait":
		<-ctx.Done()

		return refuse(tool.CallStatus{Error: "canceled"})
	}

	return refuse(tool.CallStatus{Error: "decided " + call.Arguments})
}

type gateRegistry struct {
	tool.Registry

	gate countingGate
}

func (r gateRegistry) Resolve(name string) (tool.Translator, bool) {
	if name == "gated" {
		return r.gate, true
	}

	return nil, false
}

func newTestPrefetcher(ctx context.Context) (*prefetcher, tool.Registry, *atomic.Int32) {
	var decided atomic.Int32
	r := gateRegistry{gate: countingGate{decided: &decided}}
	p := newPrefetcher(ctx, r, "s", nil)

	return p, p.wrap(r), &decided
}

func storeResponse(p *prefetcher, calls ...llm.ToolCall) {
	var out []llm.Item
	for _, c := range calls {
		out = append(out, llm.Item{Type: llm.ItemToolCall, Data: c})
	}
	p.observe(session.ID("s"), sessionstore.Item{Kind: sessionstore.ItemModelResponse, Data: sessionstore.ModelResponse{Response: llm.Response{Output: out}}})
}

func translate(t *testing.T, r tool.Registry, call llm.ToolCall) string {
	t.Helper()
	tr, ok := r.Resolve(call.Name)
	require.True(t, ok)

	return tr.Translate(nil, call).Error
}

// TestPrefetcher_DecidesOnce pins that a stored response's gated calls are
// each decided once, before Translate, and that Translate takes that
// decision.
func TestPrefetcher_DecidesOnce(t *testing.T) {
	p, r, decided := newTestPrefetcher(t.Context())
	a, b := llm.ToolCall{CallID: "1", Name: "gated", Arguments: "a"}, llm.ToolCall{CallID: "2", Name: "gated", Arguments: "b"}
	storeResponse(p, a, b, llm.ToolCall{CallID: "3", Name: "other"})

	assert.Equal(t, "decided b", translate(t, r, b))
	assert.Equal(t, "decided a", translate(t, r, a))
	assert.Equal(t, int32(2), decided.Load())
}

// TestPrefetcher_FallsBack pins that a call no decision was started for,
// such as one a resumed session left untranslated, or one whose arguments
// differ from the stored call's, is decided in Translate.
func TestPrefetcher_FallsBack(t *testing.T) {
	p, r, decided := newTestPrefetcher(t.Context())

	assert.Equal(t, "decided a", translate(t, r, llm.ToolCall{CallID: "1", Name: "gated", Arguments: "a"}))
	assert.Equal(t, int32(1), decided.Load())

	storeResponse(p, llm.ToolCall{CallID: "2", Name: "gated", Arguments: "wait"})
	assert.Equal(t, "decided b", translate(t, r, llm.ToolCall{CallID: "2", Name: "gated", Arguments: "b"}))
	assert.Empty(t, p.pending, "the stale decision was dropped")
}

// TestPrefetcher_DropsStale pins that the next stored response cancels the
// decisions no Translate took, and that a panic refuses its call.
func TestPrefetcher_DropsStale(t *testing.T) {
	p, r, _ := newTestPrefetcher(t.Context())
	storeResponse(p, llm.ToolCall{CallID: "1", Name: "gated", Arguments: "wait"})
	stale := p.pending["1"]
	storeResponse(p, llm.ToolCall{CallID: "2", Name: "gated", Arguments: "panic"})

	<-stale.done
	assert.Equal(t, "canceled", stale.rest(nil).Error)
	assert.Contains(t, translate(t, r, llm.ToolCall{CallID: "2", Name: "gated", Arguments: "panic"}), "the approval of this call failed: boom")
}

// TestPrefetcher_StopEndsDecisions pins that ending the prefetcher's
// context, as an interrupt does, ends the decisions in progress.
func TestPrefetcher_StopEndsDecisions(t *testing.T) {
	ctx, stop := context.WithCancel(t.Context())
	p, r, _ := newTestPrefetcher(ctx)
	call := llm.ToolCall{CallID: "1", Name: "gated", Arguments: "wait"}
	storeResponse(p, call)

	stop()
	assert.Equal(t, "canceled", translate(t, r, call))
}
