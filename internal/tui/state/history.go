package state

import (
	"slices"
	"strings"

	"github.com/viktordanov/uagent-harness/internal/images"
	"github.com/viktordanov/uagent-harness/internal/session"
)

// Prompt history, after Codex's composer history (chat_composer_history.rs,
// rust-v0.159.1; docs/design/prompt-history.md). ↑ on an empty composer
// recalls the previous prompt and ↓ the next, past the newest back to the
// empty composer; once a recalled prompt is edited, ↑ and ↓ move the cursor
// again. The shell loads <home>/history.jsonl at startup (EffLoadPrompts)
// and appends each prompt sent (EffRecordPrompt); ctrl+r searches it
// (historysearch.go). ↑ and ctrl+r see only the prompts of the session's
// workspace, as Claude Code keeps history per project: the file holds every
// folder's, and the view follows the session to another folder.
type PromptHistory struct {
	// file is the history file's prompts and local this process's, oldest
	// first, of every workspace. A prompt of this process keeps its images'
	// tags, so recalling it attaches them again; a shell command starts
	// with "!".
	file, local []Prompt
	// workspace is the folder the view shows: the session's.
	workspace string
	// unsent are the prompts for the file sent before a session opened.
	unsent []string
	// entries are the view: the workspace's prompts from the file, then
	// its prompts of this process.
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

// Prompt is a prompt and the workspace of the session it was sent in; ""
// is no folder, as on a line Codex wrote.
type Prompt struct{ Workspace, Text string }

type (
	// PromptsLoaded carries the history file's prompts, oldest first.
	PromptsLoaded struct{ Prompts []Prompt }
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
	// EffRecordPrompt appends a prompt of the session's workspace to the
	// history file.
	EffRecordPrompt struct{ SessionID, Workspace, Text string }
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
		h.file = e.Prompts
		h.view()
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
// as Codex's does, with images as their placeholders. A session opening
// moves the view to its workspace.
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
	case session.SessionOpened:
		return s.History.opened(e.ID, e.Settings.Workspace)
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
	switch {
	case local || (!s.Shell && strings.HasPrefix(text, "/")):
		return nil
	case s.SessionID == "": // sent once the session opens, recorded then with its workspace
		s.History.unsent = append(s.History.unsent, shown)

		return nil
	}

	return []Effect{EffRecordPrompt{SessionID: s.SessionID, Workspace: s.Settings.Workspace, Text: shown}}
}

// opened follows the session's workspace, which the prompts sent before
// the first session opened join, and records those prompts now.
func (h *PromptHistory) opened(id, workspace string) []Effect {
	for i := range h.local {
		if h.local[i].Workspace == "" {
			h.local[i].Workspace = workspace
		}
	}
	h.follow(workspace)
	effects := make([]Effect, 0, len(h.unsent))
	for _, text := range h.unsent {
		effects = append(effects, EffRecordPrompt{SessionID: id, Workspace: workspace, Text: text})
	}
	h.unsent = nil

	return effects
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

// add appends a prompt of this process to the workspace's, skipping a
// repeat of the last one, as Codex's record_local_submission does.
func (h *PromptHistory) add(raw string) {
	if len(h.entries) > h.fromFile && h.entries[len(h.entries)-1] == raw {
		return
	}
	h.local = append(h.local, Prompt{Workspace: h.workspace, Text: raw})
	h.entries = append(h.entries, raw)
}

// follow shows the prompts of workspace, the session's, from now on: a
// session opened in another folder has that folder's history.
func (h *PromptHistory) follow(workspace string) {
	if workspace != h.workspace {
		h.workspace = workspace
		h.view()
	}
}

// view rebuilds the entries from the workspace's prompts and ends
// browsing. A prompt with no workspace, or before a session gave one, is
// in no folder's view.
func (h *PromptHistory) view() {
	h.entries, h.fromFile = nil, 0
	for i, p := range slices.Concat(h.file, h.local) {
		if p.Workspace == "" || p.Workspace != h.workspace {
			continue
		}
		if i < len(h.file) {
			h.fromFile++
		}
		h.entries = append(h.entries, p.Text)
	}
	h.reset()
}

// reset ends browsing and searching, so the next ↑ starts at the newest.
func (h *PromptHistory) reset() {
	h.browsing, h.cursor, h.recalled, h.Search = false, 0, "", nil
}
