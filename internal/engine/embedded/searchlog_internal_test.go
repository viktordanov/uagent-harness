package embedded

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInsertSearches: a search goes in before the item it preceded, or
// after the one before it, and the body is otherwise byte for byte the
// runner's, spacing and key order included.
func TestInsertSearches(t *testing.T) {
	body := `{"model":"m", "input" : [ {"role":"user","content":"hi"},` + "\n" +
		`{"id":"rs_1","type":"reasoning"} ,{"type":"message","id":"msg_1"}],"tools":[{"type":"web_search"}]}`
	a := `{"type":"web_search_call","id":"ws_a"}`
	b := `{"type":"web_search_call","id":"ws_b"}`
	c := `{"type":"web_search_call","id":"ws_c"}`
	records := []searchRecord{
		{Before: "msg_1", Item: []byte(a)},
		{Before: "msg_1", Item: []byte(b)},
		{After: "rs_1", Before: "gone", Item: []byte(c)},
		{Before: "elsewhere", Item: []byte(`{"id":"ws_x"}`)},
	}

	got, ok := insertSearches([]byte(body), records)
	require.True(t, ok)
	at := bytes.Index([]byte(body), []byte(`{"type":"message"`))
	after := bytes.Index([]byte(body), []byte(`} ,{"type":"message"`)) + 1
	want := body[:after] + "," + c + body[after:at] + a + "," + b + "," + body[at:]
	assert.Equal(t, want, string(got))

	_, ok = insertSearches([]byte(body), []searchRecord{{Before: "elsewhere", Item: []byte(a)}})
	assert.False(t, ok, "no anchor in this request")
	_, ok = insertSearches([]byte(`not json`), records)
	assert.False(t, ok)
}

// TestSearchLogApply: a body that gets no search stays byte for byte the
// same, and one that gets them has them before their anchors.
func TestSearchLogApply(t *testing.T) {
	body := []byte(`{"input":[{"type":"message","id":"msg_1"}]}`)
	assert.Equal(t, body, (&searchLog{}).apply(body), "no searches recorded")
	other := &searchLog{records: []searchRecord{{Before: "msg_9", Item: []byte(`{"id":"ws"}`)}}}
	assert.Equal(t, body, other.apply(body), "no anchor")
	l := &searchLog{records: []searchRecord{{Before: "msg_1", Item: []byte(`{"id":"ws"}`)}}}
	assert.Equal(t, `{"input":[{"id":"ws"},{"type":"message","id":"msg_1"}]}`, string(l.apply(body)))
}

// TestRewriteBodyNoSearches: with no search recorded, a turn's request
// goes out as it is, its body not read; with one, the body has it.
func TestRewriteBodyNoSearches(t *testing.T) {
	body := `{"input":[{"type":"message","id":"msg_1"}]}`
	req := httptest.NewRequest(http.MethodPost, "/responses", strings.NewReader(body))
	got, err := (&modelCall{log: &searchLog{}}).rewriteBody(req)
	require.NoError(t, err)
	assert.Same(t, req, got)

	l := &searchLog{records: []searchRecord{{Before: "msg_1", Item: []byte(`{"id":"ws"}`)}}}
	got, err = (&modelCall{log: l}).rewriteBody(req)
	require.NoError(t, err)
	sent, err := io.ReadAll(got.Body)
	require.NoError(t, err)
	assert.Equal(t, `{"input":[{"id":"ws"},{"type":"message","id":"msg_1"}]}`, string(sent))
}
