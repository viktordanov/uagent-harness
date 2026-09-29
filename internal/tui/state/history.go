package state

import (
	"slices"
	"strings"

	"github.com/viktordanov/uagent-harness/internal/images"
)

// Prompt history, after Codex's composer history (chat_composer_history.rs,
// rust-v0.159.1; docs/design/prompt-history.md). ↑ on an empty composer
// recalls the previous prompt and ↓ the next, past the newest back to the
// empty composer; once a recalled prompt is edited, ↑ and ↓ move the cursor
// again. The shell loads <home>/history.jsonl at startup (EffLoadPrompts)
// and appends each prompt sent (EffRecordPrompt); ctrl+r searches it
// (historysearch.go).
type PromptHistory struct {
	// entries are the prompts, oldest first: the file's, then this
	// process's. A prompt of this process keeps its images' tags, so
	// recalling it attaches them again; a shell command starts with "!".
	entries  []string
	fromFile int // how many entries came from the file
	// cursor is the entry shown while browsing.
	cursor   int
	browsing bool
	// recalled is the composer text the last recall showed, "!" and the
	// command in shell mode: ↑ and ↓ recall only while the text is still it.
	recalled string
	// Search is the open ctrl+r search (historysearch.go).
	Search *HistorySearch
}

type (
	// PromptsLoaded carries the history file's prompts, oldest first.
	PromptsLoaded struct{ Texts []string }
	// RecallOlder is ↑ when Recalls says it recalls.
	RecallOlder struct{}
	// RecallNewer is ↓ when Recalls says it recalls.
	RecallNewer struct{}
	// DraftCleared is ctrl+c on a draft: it goes, and ↑ brings it back,
	// as Codex keeps a cleared draft in the session's history.
	DraftCleared struct{ Draft string }
)

type (
	// EffLoadPrompts reads the history file.
	EffLoadPrompts struct{}
	// EffRecordPrompt appends a prompt to the history file.
	EffRecordPrompt struct{ SessionID, Text string }
)

func (EffLoadPrompts) effect()  {}
func (EffRecordPrompt) effect() {}

// Len is how many prompts the history holds.
func (h PromptHistory) Len() int { return len(h.entries) }

// Recalls reports whether ↑ (↓ when newer) recalls a prompt for the
// composer's draft, as Codex's should_handle_navigation: always on an empty
// composer, and on a recalled prompt left as it was while the cursor is at
// its start or end (atEdge), so the cursor moves through a prompt being
// edited. ↓ recalls only while browsing.
func (s State) Recalls(draft string, atEdge, newer bool) bool {
	h := s.History
	if len(h.entries) == 0 || (newer && !h.browsing) {
		return false
	}
	text := s.composerText(draft)

	return text == "" || (atEdge && text == h.recalled)
}

// composerText is the draft as history keeps it: "!" and the command in
// shell mode.
func (s State) composerText(draft string) string {
	if s.Shell {
		return "!" + draft
	}

	return draft
}

// showsRecalled reports whether the draft is a recalled prompt, so the
// menu stays closed on a recalled "/model …", as Codex's popups do.
func (s State) showsRecalled(draft string) bool {
	return s.History.Search != nil || (s.History.recalled != "" && s.composerText(draft) == s.History.recalled)
}

// onHistory handles the history intents; ok is false for any other event.
func (s *State) onHistory(ev any) (effects []Effect, ok bool) {
	switch ev.(type) {
	case PromptsLoaded, RecallOlder, RecallNewer:
	default:
		return s.onSearch(ev)
	}
	h := &s.History
	switch e := ev.(type) {
	case PromptsLoaded:
		h.entries = append(slices.Clip(e.Texts), h.entries[h.fromFile:]...)
		h.fromFile = len(e.Texts)
		h.reset()
	case RecallOlder:
		switch {
		case len(h.entries) == 0 || (h.browsing && h.cursor == 0):
			return nil, true // the oldest stays
		case h.browsing:
			h.cursor--
		default:
			h.cursor, h.browsing = len(h.entries)-1, true
		}

		return s.show(h.entries[h.cursor]), true
	case RecallNewer:
		if !h.browsing {
			return nil, true
		}
		if h.cursor+1 >= len(h.entries) {
			h.reset() // past the newest: back to the empty composer
			s.Shell, s.Attached = false, nil

			return []Effect{EffSetDraft{}}, true
		}
		h.cursor++

		return s.show(h.entries[h.cursor]), true
	}

	return nil, true
}

// show puts an entry in the composer: a command in shell mode, a message
// with its images.
func (s *State) show(raw string) []Effect {
	if command, ok := strings.CutPrefix(raw, "!"); ok {
		s.Shell, s.Attached, s.History.recalled = true, nil, raw

		return []Effect{EffSetDraft{Text: command}}
	}
	text, imgs := images.Split(raw)
	s.Shell, s.Attached, s.History.recalled = false, imgs, text

	return []Effect{EffSetDraft{Text: text}}
}

// remember records what a Submit or Steer sends, and a draft ctrl+c
// clears, before the reducer acts on it. Every prompt joins this process's
// history; the file gets messages and shell commands, not slash commands,
// as Codex's does, with images as their placeholders.
func (s *State) remember(ev any) []Effect {
	var text string
	local := false
	switch e := ev.(type) {
	case Submit:
		text = e.Text
	case Steer:
		text = e.Text
	case DraftCleared:
		text, local = e.Draft, true
	default:
		return nil
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	shown, raw := text, s.tagged(text)
	if s.Shell {
		shown = "!" + text
		raw = shown
	}
	s.History.add(raw)
	s.History.reset()
	if local || (!s.Shell && strings.HasPrefix(text, "/")) {
		return nil
	}

	return []Effect{EffRecordPrompt{SessionID: s.SessionID, Text: shown}}
}

// tagged is text with the tags of the draft's images it still names.
func (s *State) tagged(text string) string {
	var named []images.Image
	for _, img := range s.Attached {
		if strings.Contains(text, img.Label) {
			named = append(named, img)
		}
	}

	return images.Join(text, named)
}

// add appends a prompt of this process, skipping a repeat of the last one,
// as Codex's record_local_submission does.
func (h *PromptHistory) add(raw string) {
	if len(h.entries) > h.fromFile && h.entries[len(h.entries)-1] == raw {
		return
	}
	h.entries = append(h.entries, raw)
}

// reset ends browsing and searching, so the next ↑ starts at the newest.
func (h *PromptHistory) reset() {
	h.browsing, h.cursor, h.recalled, h.Search = false, 0, "", nil
}
