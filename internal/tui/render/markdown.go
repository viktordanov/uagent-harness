package render

import (
	"charm.land/lipgloss/v2"

	"github.com/viktordanov/uagent-harness/internal/tui/render/markdown"
)

// markdownLines renders the Markdown the model writes at width w, the way
// Codex draws it (package markdown). The first line starts with first, the
// others with rest (both already styled and of equal width). A text that
// grows, as a streaming answer does, re-renders only its last block.
func (st *Styles) markdownLines(text string, w int, first, rest string) []string {
	return st.markdown.Render(text, w, first, rest)
}

// newMarkdown is the theme's Markdown renderer.
func (st *Styles) newMarkdown() *markdown.Renderer {
	return markdown.New(markdown.Styles{
		Bold:        st.bold,
		Italic:      lipgloss.NewStyle().Italic(true),
		Strike:      lipgloss.NewStyle().Strikethrough(true),
		Code:        st.codeSpan,
		Dim:         st.dim,
		Heading:     st.bold,
		TableHeader: st.bold,
		Band:        st.band,
		CodeStyle:   st.codeStyle,
	})
}
