package embedded

import (
	"bytes"
	"io"
	"net/http"
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

// TestWithSearches: a request that gets no search keeps its body byte for
// byte, and one that gets them carries the new length.
func TestWithSearches(t *testing.T) {
	body := `{"input":[{"type":"message","id":"msg_1"}]}`
	request := func() *http.Request {
		req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, "http://x/responses", bytes.NewReader([]byte(body)))
		require.NoError(t, err)

		return req
	}
	read := func(req *http.Request) string {
		out, err := io.ReadAll(req.Body)
		require.NoError(t, err)

		return string(out)
	}

	empty := &searchLog{}
	req := request()
	got, err := empty.withSearches(req)
	require.NoError(t, err)
	assert.Same(t, req, got, "no searches recorded")

	other := &searchLog{records: []searchRecord{{Before: "msg_9", Item: []byte(`{"id":"ws"}`)}}}
	got, err = other.withSearches(request())
	require.NoError(t, err)
	assert.Equal(t, body, read(got), "unchanged")

	l := &searchLog{records: []searchRecord{{Before: "msg_1", Item: []byte(`{"id":"ws"}`)}}}
	got, err = l.withSearches(request())
	require.NoError(t, err)
	want := `{"input":[{"id":"ws"},{"type":"message","id":"msg_1"}]}`
	assert.Equal(t, want, read(got))
	assert.Equal(t, int64(len(want)), got.ContentLength)
}
