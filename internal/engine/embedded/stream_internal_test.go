package embedded

import (
	"io"
	"strings"
	"sync"
	"testing"
	"testing/iotest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
)

// TestTeeBody: the runner reads the stream byte for byte, however it
// arrives, while the tee finds the deltas in it: across reads, after CRLF
// line ends, and after a line too long to parse.
func TestTeeBody(t *testing.T) {
	long := `data: {"type":"response.output_item.done","item":{"text":"` + strings.Repeat("x", maxStreamLine) + `"}}`
	body := strings.Join([]string{
		`event: response.output_item.added`,
		`data: {"type":"response.output_item.added","output_index":0,"item":{"id":"m1","type":"message","phase":"final_answer"}}`,
		``,
		`data: {"type":"response.output_text.delta","item_id":"m1","delta":"Hel"}` + "\r",
		"\r",
		long,
		``,
		`data: {"type":"response.output_text.delta","item_id":"m1","delta":"lo"}`,
		``,
		`data: {"type":"response.reasoning_summary_text.delta","item_id":"r1","summary_index":1,"delta":"why"}`,
		``,
		`data: {"type":"response.completed","response":{"output_text.delta":"not a delta"}}`,
		``,
	}, "\n")

	var mu sync.Mutex
	var got []core.Event
	sw := &switcher{stream: func(e core.Event) { mu.Lock(); got = append(got, e); mu.Unlock() }}
	ctx, done := sw.streaming(t.Context())
	tee := teed(ctx, io.NopCloser(iotest.OneByteReader(strings.NewReader(body))))
	read, err := io.ReadAll(tee)
	require.NoError(t, err)
	done(nil)

	assert.Equal(t, body, string(read), "the runner reads the stream unchanged")
	var text, reasons []string
	for _, e := range got {
		switch v := e.(type) {
		case engine.TextDelta:
			assert.Equal(t, "m1", v.ItemID)
			assert.True(t, v.Final)
			text = append(text, v.Text)
		case engine.ReasoningDelta:
			assert.Equal(t, [2]any{"r1", 1}, [2]any{v.ItemID, v.Part})
			reasons = append(reasons, v.Text)
		default:
			t.Errorf("unexpected %T", e)
		}
	}
	assert.Equal(t, "Hello", strings.Join(text, ""), "the deltas, in order, whether the pump merged them or not")
	assert.Equal(t, []string{"why"}, reasons)
}
