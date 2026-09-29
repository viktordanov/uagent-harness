package render

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/viktordanov/uagent-harness/internal/tui/state"
)

// searchPrompt starts the footer while ctrl+r searches, as Codex's.
const searchPrompt = " reverse-i-search: "

// searchQuery shows new lines and tabs in the query as marks, as Codex's
// footer does; the search matches the query itself.
func searchQuery(q string) string {
	return strings.NewReplacer("\n", "↵", "\t", "⇥").Replace(q)
}

// searchLine is the footer while ctrl+r searches: the query, then how to
// accept a match, or that nothing matches.
func (st *Styles) searchLine(search *state.HistorySearch, w int) string {
	line := st.dim.Render(searchPrompt) + st.accent.Render(searchQuery(search.Query))
	switch search.Status {
	case state.SearchMatch:
		line += st.dim.Render("  enter accept · esc cancel")
	case state.SearchNoMatch:
		line += st.bad.Render("  no match")
	case state.SearchIdle:
	}

	return ansi.Truncate(line, w, "")
}

// SearchCursorX is the footer column after the search's query, where the
// terminal's cursor goes while ctrl+r searches.
func SearchCursorX(search *state.HistorySearch, w int) int {
	return min(ansi.StringWidth(searchPrompt+searchQuery(search.Query)), max(w-1, 0))
}
