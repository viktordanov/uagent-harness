package sessionfile_test

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/viktordanov/unreal-agent/harness/llm"
	"github.com/viktordanov/unreal-agent/harness/session"
	"github.com/viktordanov/unreal-agent/harness/sessionstore"
	"github.com/viktordanov/unreal-agent/harness/sessionstore/localfile"

	"github.com/viktordanov/uah/internal/sessionfile"
)

const recorded = "testdata/recorded.session.jsonl"

// TestReadRecordedFile reads a session file the embedded engine recorded
// (a Bash call with a reasoning summary, an answer, and a second message)
// by the documented rules alone, and checks it against the runner's own
// reader.
func TestReadRecordedFile(t *testing.T) {
	h, page, err := sessionfile.Read(recorded, sessionfile.BeforeFirst, 0)
	require.NoError(t, err)

	assert.Equal(t, sessionfile.Version, h.Version)
	assert.Equal(t, "eebfbad7-9778-4cba-83d7-6ddd1d049bd5", h.ID)
	assert.False(t, page.More)
	assert.Equal(t, transcript{
		said:      []string{"hello", "again"},
		calls:     []string{"Bash {\"command\":\"echo hi\"}"},
		reasoning: []string{"think"},
		answers:   []string{"done", "bye"},
		output:    map[string]string{"call-1-0": `{"Out":"hi\n","Err":"","OutSize":3,"ErrSize":0,"ExitCode":0}`},
	}, read(t, page.Items))
	for i, it := range page.Items {
		assert.Equal(t, uint64(i+1), it.Sequence, "sequences start at 1 and grow by 1")
	}
	assert.Equal(t, page.Items[len(page.Items)-1].Sequence, page.Next)

	runner := runnerItems(t, h.ID)
	require.Len(t, page.Items, len(runner))
	for i, it := range page.Items {
		assert.Equal(t, uint64(runner[i].Sequence), it.Sequence)
		assert.Equal(t, string(runner[i].Kind), string(it.Kind))
		assert.True(t, runner[i].RecordedAt.Equal(it.RecordedAt))
	}
}

// TestReadPages pages by Sequence, as the runner's Items call does.
func TestReadPages(t *testing.T) {
	_, first, err := sessionfile.Read(recorded, sessionfile.BeforeFirst, 5)
	require.NoError(t, err)
	assert.Equal(t, []uint64{1, 2, 3, 4, 5}, sequences(first.Items))
	assert.Equal(t, uint64(5), first.Next)
	assert.True(t, first.More)

	_, rest, err := sessionfile.Read(recorded, first.Next, 0)
	require.NoError(t, err)
	assert.Equal(t, []uint64{6, 7, 8, 9, 10, 11, 12, 13, 14}, sequences(rest.Items))
	assert.False(t, rest.More)

	_, none, err := sessionfile.Read(recorded, rest.Next, 0)
	require.NoError(t, err)
	assert.Empty(t, none.Items)
	assert.Equal(t, uint64(14), none.Next, "the cursor stays")
}

// TestLast reads the last committed item from the end of the file.
func TestLast(t *testing.T) {
	data, err := os.ReadFile(recorded)
	require.NoError(t, err)
	dir := t.TempDir()
	write := func(name string, b []byte) string {
		p := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(p, b, 0o600))

		return p
	}

	it, found, err := sessionfile.Last(recorded)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, uint64(14), it.Sequence)
	assert.Equal(t, sessionfile.KindModelResponse, it.Kind)

	partial := write("partial", append(append([]byte{}, data...), `{"type":"item","data":{"Item":{"Sequence":15`...))
	it, _, err = sessionfile.Last(partial)
	require.NoError(t, err)
	assert.Equal(t, uint64(14), it.Sequence, "a line without its newline is not committed")

	lines := splitLines(data)
	withOps := write("ops", joinLines(lines[:15])) // item 6 and the operation updates after it
	it, _, err = sessionfile.Last(withOps)
	require.NoError(t, err)
	assert.Equal(t, uint64(6), it.Sequence, "operation records are not items")

	long := []byte(`{"type":"operation","data":{"Operation":{"ID":"` + strings.Repeat("x", 300_000) + `"}}}` + "\n")
	it, _, err = sessionfile.Last(write("long", joinLines(append(lines[:7:7], long))))
	require.NoError(t, err)
	assert.Equal(t, uint64(6), it.Sequence, "reads further back past a long record")

	_, found, err = sessionfile.Last(write("header", joinLines(lines[:1])))
	require.NoError(t, err)
	assert.False(t, found)
}

// TestReadRefusesOtherVersions reads only version 2.
func TestReadRefusesOtherVersions(t *testing.T) {
	p := filepath.Join(t.TempDir(), "v3")
	require.NoError(t, os.WriteFile(p, []byte(`{"type":"session","data":{"Version":3,"Session":{"ID":"x","CreatedAt":"2026-09-29T00:00:00Z"}}}`+"\n"), 0o600))

	_, _, err := sessionfile.Read(p, sessionfile.BeforeFirst, 0)

	require.ErrorIs(t, err, sessionfile.ErrVersion)
}

// transcript is what a reader shows of a session.
type transcript struct {
	said, calls, reasoning, answers []string
	// output is each finished call's operation result.
	output map[string]string
}

// read follows the documented rules: external inputs are the user's
// messages, a response's outputs are its reasoning, calls, and messages,
// and a status's operations carry the call's result.
func read(t *testing.T, items []sessionfile.Item) transcript {
	t.Helper()
	tr := transcript{output: map[string]string{}}
	for _, it := range items {
		switch it.Kind {
		case sessionfile.KindInput:
			var in sessionfile.Input
			require.NoError(t, it.Decode(&in))
			if in.Kind == sessionfile.InputExternal {
				text, err := in.Text()
				require.NoError(t, err)
				tr.said = append(tr.said, text)
			}
		case sessionfile.KindModelResponse:
			var r sessionfile.ModelResponse
			require.NoError(t, it.Decode(&r))
			readOutputs(t, &tr, r.Response.Output)
		case sessionfile.KindToolCallStatus:
			var st sessionfile.ToolCallStatus
			require.NoError(t, it.Decode(&st))
			for _, raw := range it.Operations {
				var op sessionfile.Operation
				require.NoError(t, json.Unmarshal(raw, &op))
				if op.State.Result != nil && string(op.State.Result) != "null" {
					tr.output[st.CallID] = string(op.State.Result)
				}
			}
		case sessionfile.KindTurn, sessionfile.KindFork:
		}
	}

	return tr
}

func readOutputs(t *testing.T, tr *transcript, outputs []sessionfile.Output) {
	t.Helper()
	for _, o := range outputs {
		switch o.Type {
		case sessionfile.OutputMessage:
			var m sessionfile.Message
			require.NoError(t, o.Decode(&m))
			tr.answers = append(tr.answers, m.Text)
		case sessionfile.OutputReasoning:
			var r sessionfile.Reasoning
			require.NoError(t, o.Decode(&r))
			tr.reasoning = append(tr.reasoning, r.Summary...)
		case sessionfile.OutputToolCall:
			var c sessionfile.ToolCall
			require.NoError(t, o.Decode(&c))
			tr.calls = append(tr.calls, c.Name+" "+c.Arguments)
		}
	}
}

// runnerItems reads the fixture with the runner's store, the reference.
func runnerItems(t *testing.T, id string) []sessionstore.Item {
	t.Helper()
	data, err := os.ReadFile(recorded)
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, id+".session.jsonl"), data, 0o600))
	store, err := localfile.New(dir)
	require.NoError(t, err)
	page, err := store.Items(context.Background(), session.ID(id), 0, 1000)
	require.NoError(t, err)

	return page.Items
}

func sequences(items []sessionfile.Item) []uint64 {
	out := make([]uint64, 0, len(items))
	for _, it := range items {
		out = append(out, it.Sequence)
	}

	return out
}

func splitLines(data []byte) [][]byte {
	return bytes.SplitAfter(bytes.TrimSuffix(data, []byte{'\n'}), []byte{'\n'})
}

func joinLines(lines [][]byte) []byte {
	out := bytes.Join(lines, nil)
	if !bytes.HasSuffix(out, []byte{'\n'}) {
		out = append(out, '\n')
	}

	return out
}

// TestDecodeCustomToolCall: a freeform call's output, as the runner writes
// it, decodes with its raw input and the Custom mark; a function call's has
// no mark.
func TestDecodeCustomToolCall(t *testing.T) {
	for _, want := range []sessionfile.ToolCall{
		{CallID: "c1", Name: "apply_patch", Arguments: "*** Begin Patch\n*** Add File: a\n+\"x\"\n*** End Patch\n", Custom: true},
		{CallID: "c2", Name: "Bash", Arguments: `{"command":"ls"}`},
	} {
		item, err := json.Marshal(llm.Item{ProviderID: "p", Type: llm.ItemToolCall, Data: llm.ToolCall{
			CallID: want.CallID, Name: want.Name, Arguments: want.Arguments, Custom: want.Custom,
		}})
		require.NoError(t, err)
		var o sessionfile.Output
		require.NoError(t, json.Unmarshal(item, &o))
		require.Equal(t, sessionfile.OutputToolCall, o.Type)
		var got sessionfile.ToolCall
		require.NoError(t, o.Decode(&got))
		assert.Equal(t, want, got)
	}
}
