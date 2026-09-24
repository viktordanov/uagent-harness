package embedded

import (
	"sync"

	"github.com/unreallabsai/unreal-agent/harness/llm"

	"github.com/viktordanov/uagent-harness/internal/compaction"
	"github.com/viktordanov/uagent-harness/internal/contextusage"
)

// lastRequests keep each session's latest model request, as sent after any
// compaction, and the input tokens reported for it, for /context. Subagents
// share their parent's engine, so requests are kept by session.
type lastRequests struct {
	mu       sync.Mutex
	sessions map[string]lastRequest
}

type lastRequest struct {
	req   llm.Request
	input int64
}

// recorder returns the switcher hook that records a session's requests.
func (l *lastRequests) recorder(sessionID string) func(llm.Request, llm.Usage) {
	return func(req llm.Request, usage llm.Usage) {
		l.mu.Lock()
		defer l.mu.Unlock()
		if l.sessions == nil {
			l.sessions = map[string]lastRequest{}
		}
		l.sessions[sessionID] = lastRequest{req: req, input: usage.InputTokens}
	}
}

// ContextUsage breaks the session's last request down by what fills the
// context. ok is false before its first model request.
func (e *Engine) ContextUsage(sessionID string) (contextusage.Usage, bool) {
	e.last.mu.Lock()
	last, ok := e.last.sessions[sessionID]
	e.last.mu.Unlock()
	if !ok {
		return contextusage.Usage{}, false
	}
	window := compaction.ContextWindow(last.req.Model.ID, e.cfg.ContextWindow)

	return contextusage.Analyze(last.req, last.input, window, e.cfg.Compaction.Limit(window), e.cfg.InstructionFiles), true
}
