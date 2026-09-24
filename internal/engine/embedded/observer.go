package embedded

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"io"
	"sync"

	"github.com/unreallabsai/unreal-agent/harness/session"
	"github.com/unreallabsai/unreal-agent/harness/sessionstore"
)

// observer writes each persisted session item as one JSON line, exactly as
// the runner prints it.
type observer struct {
	sessionID session.ID
	out       io.Writer
	cancel    context.CancelFunc

	mu      sync.Mutex
	failure error
}

func (o *observer) observe(id session.ID, item sessionstore.Item) {
	if id != o.sessionID {
		return
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.failure != nil {
		return
	}
	line, err := json.Marshal(item)
	if err == nil {
		_, err = o.out.Write(append(line, '\n'))
	}
	if err != nil {
		o.failure = fmt.Errorf("failed to write session item %d: %w", item.Sequence, err)
		o.cancel()
	}
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
