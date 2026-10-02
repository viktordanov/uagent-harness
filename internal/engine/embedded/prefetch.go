package embedded

import (
	"context"
	"fmt"
	"sync"

	"github.com/viktordanov/uah-core/harness/llm"
	"github.com/viktordanov/uah-core/harness/session"
	"github.com/viktordanov/uah-core/harness/sessionstore"
	"github.com/viktordanov/uah-core/harness/tool"
)

// The coordinator translates the calls of a model response one at a time
// on its event loop, and a call that needs a decision waits there for it:
// PreToolUse hooks, the auto-reviewer's model call, PermissionRequest
// hooks, or the user. So once a response is in the session, the decisions
// of all its calls start together, each on its own goroutine, and the
// coordinator's Translate of a call takes the decision its goroutine
// reached; only the rest, which needs no I/O, runs on the loop. N
// escalations then take as long as the slowest review, not the sum. See
// docs/design/agent-tuning.md.

// gatedTranslator is a translator whose Translate waits for a decision
// before it submits anything. decide makes the decision under ctx and
// returns the rest of Translate.
type gatedTranslator interface {
	tool.Translator
	decide(ctx context.Context, call llm.ToolCall) submit
}

// submit is the rest of a gated call's Translate: it submits the call's
// operation, or returns its error, without I/O.
type submit func(tool.Context) tool.CallStatus

// refuse is a submit that only returns the status.
func refuse(status tool.CallStatus) submit {
	return func(tool.Context) tool.CallStatus { return status }
}

// prefetcher starts the decisions of each stored response's calls and
// hands them to the coordinator's Translate.
type prefetcher struct {
	// ctx bounds the decisions: the run's, ended early by an interrupt.
	ctx context.Context
	// registry resolves the calls, hooks included.
	registry  tool.Registry
	sessionID session.ID
	// note, when set, records a response's calls for the auto-reviewer
	// before the decisions start.
	note func([]llm.ToolCall)

	mu      sync.Mutex
	pending map[string]*decision
}

// decision is a call's decision in progress; rest is set once done closes.
type decision struct {
	call   llm.ToolCall
	cancel context.CancelFunc
	done   chan struct{}
	rest   submit
}

func newPrefetcher(ctx context.Context, registry tool.Registry, sessionID session.ID, note func([]llm.ToolCall)) *prefetcher {
	return &prefetcher{ctx: ctx, registry: registry, sessionID: sessionID, note: note, pending: map[string]*decision{}}
}

// observe starts the decisions when the coordinator stores a model
// response, just before it translates the response's calls.
func (p *prefetcher) observe(id session.ID, item sessionstore.Item) {
	r, ok := item.Data.(sessionstore.ModelResponse)
	if !ok || id != p.sessionID {
		return
	}
	var calls []llm.ToolCall
	for _, out := range r.Response.Output {
		if call, ok := out.Data.(llm.ToolCall); ok && out.Type == llm.ItemToolCall {
			calls = append(calls, call)
		}
	}
	p.start(calls)
}

// start drops the decisions no Translate took, which belong to an earlier
// response, and starts one for each gated call.
func (p *prefetcher) start(calls []llm.ToolCall) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, d := range p.pending {
		d.cancel()
		delete(p.pending, id)
	}
	if len(calls) > 0 && p.note != nil {
		p.note(calls)
	}
	for _, call := range calls {
		t, ok := p.registry.Resolve(call.Name)
		g, gated := t.(gatedTranslator)
		if !ok || !gated {
			continue
		}
		ctx, cancel := context.WithCancel(p.ctx)
		d := &decision{call: call, cancel: cancel, done: make(chan struct{})}
		p.pending[call.CallID] = d
		go d.decide(ctx, g)
	}
}

// decide makes the call's decision. A panic refuses the call rather than
// take down the TUI, as runCoordinator's recover does for the loop.
func (d *decision) decide(ctx context.Context, g gatedTranslator) {
	defer close(d.done)
	defer func() {
		if v := recover(); v != nil {
			d.rest = refuse(tool.ErrorStatus(fmt.Sprintf("the approval of this call failed: %v", v), 0))
		}
	}()
	d.rest = g.decide(ctx, d.call)
}

// take returns the decision started for the call, unless none was or the
// call differs from the one it decides.
func (p *prefetcher) take(call llm.ToolCall) (*decision, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	d, ok := p.pending[call.CallID]
	if !ok {
		return nil, false
	}
	delete(p.pending, call.CallID)
	if d.call != call {
		d.cancel()

		return nil, false
	}

	return d, true
}

// wrap returns the registry the coordinator runs: each gated translator
// takes its call's decision when one was started.
func (p *prefetcher) wrap(r tool.Registry) tool.Registry { return prefetchRegistry{Registry: r, p: p} }

type prefetchRegistry struct {
	tool.Registry

	p *prefetcher
}

func (r prefetchRegistry) Resolve(name string) (tool.Translator, bool) {
	t, ok := r.Registry.Resolve(name)
	if g, gated := t.(gatedTranslator); ok && gated {
		return prefetched{gatedTranslator: g, p: r.p}, true
	}

	return t, ok
}

// prefetched waits for the call's decision, or decides it now when none
// was started, such as for a call a resumed session left untranslated.
type prefetched struct {
	gatedTranslator

	p *prefetcher
}

func (t prefetched) Translate(ctx tool.Context, call llm.ToolCall) tool.CallStatus {
	d, ok := t.p.take(call)
	if !ok {
		return t.gatedTranslator.Translate(ctx, call)
	}
	<-d.done
	d.cancel()

	return d.rest(ctx)
}
