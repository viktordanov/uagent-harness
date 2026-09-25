package embedded

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
)

// The runner's Responses client parses the SSE stream and drops its deltas
// (responsesapi/stream.go in v0.1.1); nothing in the runner reports partial
// output. The transport sees each attempt's body (reconnect.go), so for a
// turn request that carries a stream it tees the body: the runner reads the
// same bytes, and a small parser turns the text deltas into engine events.
// See docs/design/streaming.md.

// maxStreamLine is the longest SSE line the tee parses. Deltas are small;
// the long lines are the final item and the completed response.
const maxStreamLine = 1 << 20

// streamKey carries a request's stream to the transport.
type streamKey struct{}

// stream is one model request's streamed text. The tee adds deltas without
// blocking; a pump goroutine sends them to emit, merged when they pile up.
type stream struct {
	emit func(core.Event)
	wake chan struct{}
	done chan struct{}

	mu      sync.Mutex
	pending []core.Event
	closed  bool
	// streamed means text went out since the last reset.
	streamed bool
	// final are the message items that are the final answer.
	final map[string]bool
}

// streaming gives a turn request its stream when the run streams. done
// sends what is left before the request returns, so every delta reaches
// the session before the runner's final events for the response.
func (s *switcher) streaming(ctx context.Context) (context.Context, func(error)) {
	if s.stream == nil {
		return ctx, func(error) {}
	}
	st := &stream{emit: s.stream, wake: make(chan struct{}, 1), done: make(chan struct{}), final: map[string]bool{}}
	go st.pump()

	return context.WithValue(ctx, streamKey{}, st), st.close
}

func (s *stream) pump() {
	defer close(s.done)
	for range s.wake {
		s.flush()
	}
	s.flush()
}

func (s *stream) flush() {
	s.mu.Lock()
	events := s.pending
	s.pending = nil
	s.mu.Unlock()
	for _, e := range events {
		s.emit(e)
	}
}

// close ends the stream: a failed request's text is void, since the runner
// records nothing for it.
func (s *stream) close(err error) {
	s.mu.Lock()
	if err != nil {
		s.resetLocked()
	}
	s.closed = true
	close(s.wake)
	s.mu.Unlock()
	<-s.done
}

// attempt starts an attempt's body; the text of an earlier one is void.
func (s *stream) attempt() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.resetLocked()
	}
}

func (s *stream) resetLocked() {
	if s.streamed {
		s.streamed = false
		s.pending = append(s.pending, engine.StreamReset{At: time.Now()})
		s.wakeLocked()
	}
}

func (s *stream) wakeLocked() {
	select {
	case s.wake <- struct{}{}:
	default: // the pump is already due
	}
}

// add queues a delta, merged into the one before it when it continues the
// same text.
func (s *stream) add(e core.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.streamed = true
	if n := len(s.pending); n > 0 {
		if merged, ok := mergeDelta(s.pending[n-1], e); ok {
			s.pending[n-1] = merged

			return
		}
	}
	s.pending = append(s.pending, e)
	s.wakeLocked()
}

func mergeDelta(prev, next core.Event) (core.Event, bool) {
	switch p := prev.(type) {
	case engine.TextDelta:
		if n, ok := next.(engine.TextDelta); ok && n.ItemID == p.ItemID {
			p.Text += n.Text

			return p, true
		}
	case engine.ReasoningDelta:
		if n, ok := next.(engine.ReasoningDelta); ok && n.ItemID == p.ItemID && n.Part == p.Part {
			p.Text += n.Text

			return p, true
		}
	}

	return nil, false
}

// streamEvent is the part of a Responses stream event the tee reads.
type streamEvent struct {
	Type         string `json:"type"`
	ItemID       string `json:"item_id"`
	Delta        string `json:"delta"`
	SummaryIndex int    `json:"summary_index"`
	Item         struct {
		ID    string `json:"id"`
		Type  string `json:"type"`
		Phase string `json:"phase"`
	} `json:"item"`
}

// streamTypes are the event types the tee decodes; it skips any other line
// without decoding it.
var streamTypes = [][]byte{[]byte(`"response.output_text.delta"`), []byte(`"response.reasoning_summary_text.delta"`), []byte(`"response.output_item.added"`)}

// line reads one SSE line.
func (s *stream) line(line []byte) {
	payload, ok := bytes.CutPrefix(bytes.TrimSuffix(line, []byte("\r")), []byte("data:"))
	if !ok || !containsAny(payload, streamTypes) {
		return
	}
	var ev streamEvent
	if json.Unmarshal(payload, &ev) != nil {
		return
	}
	switch ev.Type {
	case "response.output_item.added":
		if ev.Item.Type == "message" && ev.Item.Phase == "final_answer" {
			s.mu.Lock()
			s.final[ev.Item.ID] = true
			s.mu.Unlock()
		}
	case "response.output_text.delta":
		if ev.Delta != "" {
			s.mu.Lock()
			final := s.final[ev.ItemID]
			s.mu.Unlock()
			s.add(engine.TextDelta{At: time.Now(), ItemID: ev.ItemID, Text: ev.Delta, Final: final})
		}
	case "response.reasoning_summary_text.delta":
		if ev.Delta != "" {
			s.add(engine.ReasoningDelta{At: time.Now(), ItemID: ev.ItemID, Part: ev.SummaryIndex, Text: ev.Delta})
		}
	}
}

func containsAny(b []byte, subs [][]byte) bool {
	for _, sub := range subs {
		if bytes.Contains(b, sub) {
			return true
		}
	}

	return false
}

// teed returns the body of an attempt that started, teed into the
// request's stream when it has one.
func teed(ctx context.Context, body io.ReadCloser) io.ReadCloser {
	s, _ := ctx.Value(streamKey{}).(*stream)
	if s == nil {
		return body
	}
	s.attempt()

	return &teeBody{ReadCloser: body, s: s}
}

// teeBody passes its base's bytes through unchanged and feeds each
// complete line to the stream.
type teeBody struct {
	io.ReadCloser

	s    *stream
	line []byte
	// skip drops the rest of a line longer than maxStreamLine.
	skip bool
}

func (b *teeBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.scan(p[:n])

	return n, err //nolint:wrapcheck // a body returns its base's errors unchanged
}

func (b *teeBody) scan(data []byte) {
	for len(data) > 0 {
		i := bytes.IndexByte(data, '\n')
		if i < 0 {
			b.keep(data)

			return
		}
		b.keep(data[:i])
		if !b.skip {
			b.s.line(b.line)
		}
		b.line, b.skip = b.line[:0], false
		data = data[i+1:]
	}
}

func (b *teeBody) keep(part []byte) {
	if b.skip {
		return
	}
	if len(b.line)+len(part) > maxStreamLine {
		b.line, b.skip = b.line[:0], true

		return
	}
	b.line = append(b.line, part...)
}
