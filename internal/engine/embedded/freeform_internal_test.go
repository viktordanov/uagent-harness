package embedded

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBodyRewritesKeepCustomToolCalls: a freeform apply_patch call and its
// output pass through the web searches' insertion and the remote
// compaction's replacement byte for byte, and anchor a search by their ID.
func TestBodyRewritesKeepCustomToolCalls(t *testing.T) {
	call := `{"type":"custom_tool_call","id":"ctc_1","call_id":"c1","name":"apply_patch",` +
		`"input":"*** Begin Patch\n*** Add File: a.txt\n+{\"input\":[1]}\n*** End Patch\n"}`
	output := `{"type":"custom_tool_call_output","call_id":"c1","output":[{"type":"input_text","text":"Success."}]}`
	placeholder := `{"type":"message","role":"user","content":[{"type":"input_text","text":"[uah-remote-compaction:abc]"}]}`
	body := []byte(`{"input":[` + placeholder + `,` + call + `,` + output + `],"stream":true}`)
	search := `{"type":"web_search_call","id":"ws_a"}`

	got, ok := insertSearches(body, []searchRecord{{Before: "ctc_1", Item: []byte(search)}})
	require.True(t, ok, "the custom call anchors the search")
	assert.Equal(t, `{"input":[`+placeholder+`,`+search+`,`+call+`,`+output+`],"stream":true}`, string(got))

	got = replaceItem(body, remoteItem{marker: "uah-remote-compaction:abc", item: []byte(`{"type":"compaction"}`)})
	assert.Equal(t, `{"input":[{"type":"compaction"},`+call+`,`+output+`],"stream":true}`, string(got))
	got = appendInput(got, compactionTrigger)
	assert.Equal(t, `{"input":[{"type":"compaction"},`+call+`,`+output+`,{"type":"compaction_trigger"}],"stream":true}`, string(got))
}
