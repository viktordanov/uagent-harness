package state

import (
	"fmt"
	"slices"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/engine"
)

// Streamed text (docs/design/streaming.md): the engine's deltas build
// transcript items marked Streaming as the model writes them. The runner's
// AssistantMessage or ReasoningSummary for the response replaces the oldest
// streamed item of its kind in place, so the recorded text wins. A
// StreamReset, or the end of the run, drops the streamed items no final
// event claimed.

// streamed is a transcript item the model is still writing.
type streamed struct {
	// id is the model's item ID, with the summary part for reasoning.
	id   string
	key  string
	kind Kind
}

// onStream handles the engine's streaming events and reports whether ev
// was one.
func (s *State) onStream(ev core.Event) bool {
	switch e := ev.(type) {
	case engine.TextDelta:
		s.addStreamed(KindAssistant, e.ItemID, e.Text, e.Final)
	case engine.ReasoningDelta:
		s.addStreamed(KindReasoning, fmt.Sprintf("%s:%d", e.ItemID, e.Part), e.Text, false)
	case engine.StreamReset:
		s.dropStreamed()
	default:
		return false
	}

	return true
}

// addStreamed appends text to the streamed item id, or starts one.
func (s *State) addStreamed(kind Kind, id, text string, final bool) {
	i := slices.IndexFunc(s.streaming, func(x streamed) bool { return x.kind == kind && x.id == id })
	if i >= 0 {
		if s.update(s.streaming[i].key, func(it *Item) { it.Text += text; it.Final = it.Final || final }) {
			return
		}
		s.streaming = slices.Delete(s.streaming, i, i+1) // its item is gone, as after /clear
	}
	key := s.nextKey("stream")
	s.streaming = append(s.streaming, streamed{id: id, key: key, kind: kind})
	s.put(Item{Kind: kind, Key: key, Text: text, Final: final, Streaming: true})
}

// finalKey is the key of the item a final message of kind goes in: the
// oldest streamed item of that kind, or a new item's.
func (s *State) finalKey(kind Kind, prefix string) string {
	for i, x := range s.streaming {
		if x.kind != kind {
			continue
		}
		s.streaming = slices.Delete(s.streaming, i, i+1)
		if _, ok := s.index[x.key]; ok {
			return x.key
		}

		return s.finalKey(kind, prefix) // its item is gone; try the next
	}

	return s.nextKey(prefix)
}

// dropStreamed removes the streamed items: their text is void.
func (s *State) dropStreamed() {
	if len(s.streaming) == 0 {
		return
	}
	drop := make(map[string]bool, len(s.streaming))
	for _, x := range s.streaming {
		drop[x.key] = true
	}
	s.streaming = nil
	items := make([]Item, 0, len(s.Items))
	index := make(map[string]int, len(s.Items))
	for _, it := range s.Items {
		if drop[it.Key] {
			continue
		}
		index[it.Key] = len(items)
		items = append(items, it)
	}
	s.Items, s.index = items, index
}

// Writing reports whether the model is writing an answer now, for the
// status line.
func (s State) Writing() bool {
	return slices.ContainsFunc(s.streaming, func(x streamed) bool { return x.kind == KindAssistant })
}
