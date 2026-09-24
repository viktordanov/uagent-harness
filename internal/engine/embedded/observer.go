package embedded

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"sync"

	"github.com/unreallabsai/unreal-agent/harness/session"
	"github.com/unreallabsai/unreal-agent/harness/sessionstore"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
)

// observer writes each persisted session item as one JSON line, exactly as
// the runner prints it. It also emits the diff of each applied patch, read
// from the same line, so the live view and a reloaded one agree.
type observer struct {
	sessionID session.ID
	out       io.Writer
	cancel    context.CancelFunc
	// emit, when set, receives engine.PatchApplied.
	emit func(core.Event)

	mu      sync.Mutex
	failure error
	patched map[string]bool
}

func (o *observer) observe(id session.ID, item sessionstore.Item) {
	if id != o.sessionID {
		return
	}
	if ev, ok := o.write(item); ok && o.emit != nil {
		o.emit(ev)
	}
}

// write writes the item and returns the patch it completes, once per call.
func (o *observer) write(item sessionstore.Item) (engine.PatchApplied, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.failure != nil {
		return engine.PatchApplied{}, false
	}
	line, err := json.Marshal(item)
	if err == nil {
		_, err = o.out.Write(append(line, '\n'))
	}
	if err != nil {
		o.failure = fmt.Errorf("failed to write session item %d: %w", item.Sequence, err)
		o.cancel()

		return engine.PatchApplied{}, false
	}
	ev, ok := engine.PatchFromItem(line)
	if !ok || o.patched[ev.CallID] {
		return engine.PatchApplied{}, false
	}
	if o.patched == nil {
		o.patched = map[string]bool{}
	}
	o.patched[ev.CallID] = true

	return ev, true
}

func (o *observer) err() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	return o.failure
}

// writeError writes the runner's error event.
func writeError(out io.Writer, err error) {
	line, merr := json.Marshal(struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}{"error", err.Error()})
	if merr == nil {
		_, _ = out.Write(append(line, '\n'))
	}
}
