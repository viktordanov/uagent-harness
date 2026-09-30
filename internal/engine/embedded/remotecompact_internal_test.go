package embedded

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The ChatGPT backend sends the item in output_item.done and an empty
// output in response.completed.
const chatgptStream = "event: response.output_item.added\n" +
	`data: {"type":"response.output_item.added","output_index":0,"item":{"id":"cmp_1","type":"compaction"}}` + "\n\n" +
	"event: response.output_item.done\n" +
	`data: {"type":"response.output_item.done","output_index":0,"item":{"id":"cmp_1","type":"compaction","encrypted_content":"E=="}}` + "\n\n" +
	"event: response.completed\n" +
	`data: {"type":"response.completed","response":{"id":"r","status":"completed","output":[],"usage":{"input_tokens":124,"output_tokens":115}}}` + "\n\n"

func TestRemoteCall_KeepsTheItemAndHidesItFromTheRunner(t *testing.T) {
	call := &remoteCall{}
	out := readAttempt(t, &modelCall{ctx: t.Context(), remote: call, final: map[string]bool{}}, chatgptStream)
	item, err := call.result()
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":"cmp_1","type":"compaction","encrypted_content":"E=="}`, string(item))
	assert.NotContains(t, out, `"type":"compaction"`, "the runner's parser never sees the item")
	assert.Equal(t, 2, strings.Count(out, "msg_uah_compaction"))
	assert.Contains(t, out, `"output_tokens":115`, "the usage passes")
	assert.Contains(t, out, "event: response.completed\n")
}

func TestRemoteCall_AnAnswerWithoutAnItemFails(t *testing.T) {
	call := &remoteCall{}
	readAttempt(t, &modelCall{ctx: t.Context(), remote: call, final: map[string]bool{}},
		`data: {"type":"response.completed","response":{"id":"r","status":"completed","output":[]}}`+"\n\n")
	_, err := call.result()
	assert.Error(t, err)
}

// TestRemoteCall_ACutAnswerIsForgotten: the item of an attempt cut after
// it is not the answer of the next attempt, which has none.
func TestRemoteCall_ACutAnswerIsForgotten(t *testing.T) {
	call := &remoteCall{}
	c := &modelCall{ctx: t.Context(), remote: call, final: map[string]bool{}}
	readAttempt(t, c, chatgptStream[:strings.Index(chatgptStream, "event: response.completed")])
	readAttempt(t, c, `data: {"type":"response.completed","response":{"id":"r","status":"completed","output":[]}}`+"\n\n")
	_, err := call.result()
	assert.Error(t, err)
}

func TestRemoteCall_OtherRequestsPass(t *testing.T) {
	assert.Equal(t, chatgptStream, readAttempt(t, &modelCall{ctx: t.Context(), final: map[string]bool{}}, chatgptStream))
}

func TestRemoteBodies(t *testing.T) {
	body := []byte(`{"model":"m","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]},` +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"[uah-remote-compaction:abc] note"}]},` +
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"next"}]}],"stream":true}`)
	got := replaceItem(body, remoteItem{marker: "uah-remote-compaction:abc", item: []byte(`{"type":"compaction","encrypted_content":"E"}`)})
	assert.Equal(t, `{"model":"m","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hi"}]},`+
		`{"type":"compaction","encrypted_content":"E"},`+
		`{"type":"message","role":"user","content":[{"type":"input_text","text":"next"}]}],"stream":true}`, string(got))
	assert.Equal(t, body, replaceItem(body, remoteItem{marker: "uah-remote-compaction:zzz", item: []byte(`{}`)}), "no placeholder, no change")
	got = appendInput([]byte(`{"input":[{"type":"message"}],"x":1}`), compactionTrigger)
	assert.Equal(t, `{"input":[{"type":"message"},{"type":"compaction_trigger"}],"x":1}`, string(got))
}
