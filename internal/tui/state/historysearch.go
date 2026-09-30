package state

import (
	"slices"
	"strings"

	"github.com/viktordanov/uah/internal/images"
)

// Reverse search, as Codex's ctrl+r (chat_composer/history_search.rs,
// rust-v0.159.1): ctrl+r opens "reverse-i-search:" in the footer without
// previewing anything; typing finds the newest prompt that contains the
// query, ignoring case, and shows it in the composer; ctrl+r or ↑ go to
// older matches and ctrl+s or ↓ to newer ones, each text once; enter keeps
// the match as a draft to edit, and esc or ctrl+c put the draft back.
type HistorySearch struct {
	Query  string
	Status SearchStatus
	// matches are the entries that contain the query, newest first, one
	// per text; at is the one shown.
	matches []int
	at      int
	// saved is the composer before the search, for esc and for a query
	// that matches nothing.
	saved savedDraft
}

// SearchStatus is what the search shows.
type SearchStatus int

const (
	// SearchIdle has no query yet: the composer shows the saved draft.
	SearchIdle SearchStatus = iota
	// SearchMatch shows a match.
	SearchMatch
	// SearchNoMatch found nothing: the composer shows the saved draft.
	SearchNoMatch
)

type savedDraft struct {
	text     string
	shell    bool
	attached []images.Image
}

type (
	// SearchOpen is ctrl+r: open the search, or go to an older match.
	SearchOpen struct{ Draft string }
	// SearchType edits the query: "\b" deletes its last character, and
	// any other text is appended.
	SearchType struct{ Text string }
	// SearchClear empties the query (ctrl+u).
	SearchClear struct{}
	// SearchMove goes to an older (ctrl+r, ↑) or a newer (ctrl+s, ↓) match.
	SearchMove struct{ Older bool }
	// SearchAccept is enter: keep the match as the draft.
	SearchAccept struct{}
	// SearchCancel is esc or ctrl+c: put the draft back.
	SearchCancel struct{}
)

// onSearch handles the search intents; ok is false for any other event.
func (s *State) onSearch(ev any) (effects []Effect, ok bool) {
	switch ev.(type) {
	case SearchOpen, SearchMove, SearchType, SearchClear, SearchAccept, SearchCancel:
	default:
		return nil, false
	}
	h := &s.History
	search := h.Search
	if open, isOpen := ev.(SearchOpen); isOpen && search == nil {
		h.Search = &HistorySearch{saved: savedDraft{text: open.Draft, shell: s.Shell, attached: slices.Clone(s.Attached)}}

		return nil, true
	}
	if search == nil {
		return nil, true // no search is open
	}
	switch e := ev.(type) {
	case SearchOpen:
		return s.searchMove(true), true
	case SearchMove:
		return s.searchMove(e.Older), true
	case SearchType:
		if e.Text == "\b" {
			r := []rune(search.Query)
			search.Query = string(r[:max(len(r)-1, 0)])
		} else {
			search.Query += e.Text
		}

		return s.searchFind(), true
	case SearchClear:
		search.Query = ""

		return s.searchFind(), true
	case SearchAccept:
		if search.Status == SearchMatch {
			h.Search = nil
			h.cursor, h.browsing = search.matches[search.at], true // ↑ goes on from the match, as in Codex
		}

		return nil, true
	case SearchCancel:
		h.reset()

		return s.restore(search.saved), true
	}

	return nil, true
}

// searchFind looks for the query from the newest prompt, as each edit of
// the query restarts the search.
func (s *State) searchFind() []Effect {
	search := s.History.Search
	search.matches, search.at = nil, 0
	if search.Query == "" {
		search.Status = SearchIdle

		return s.restore(search.saved)
	}
	query := strings.ToLower(search.Query)
	seen := map[string]bool{}
	for i, entry := range slices.Backward(s.History.entries) {
		text := images.Display(entry)
		if seen[text] || !strings.Contains(strings.ToLower(text), query) {
			continue
		}
		seen[text] = true
		search.matches = append(search.matches, i)
	}
	if len(search.matches) == 0 {
		search.Status = SearchNoMatch

		return s.restore(search.saved)
	}
	search.Status = SearchMatch

	return s.show(s.History.entries[search.matches[0]])
}

// searchMove goes to an older or newer match; at either end the match
// stays, as Codex's AtBoundary.
func (s *State) searchMove(older bool) []Effect {
	search := s.History.Search
	next := search.at - 1
	if older {
		next = search.at + 1
	}
	if search.Status != SearchMatch || next < 0 || next >= len(search.matches) {
		return nil
	}
	search.at = next

	return s.show(s.History.entries[search.matches[next]])
}

// restore puts a saved draft back in the composer.
func (s *State) restore(d savedDraft) []Effect {
	s.Shell, s.Attached = d.shell, d.attached

	return []Effect{EffSetDraft{Text: d.text}}
}
