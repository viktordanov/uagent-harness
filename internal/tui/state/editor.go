package state

import (
	"strings"

	"github.com/viktordanov/uah/internal/session"
)

// ctrl+g edits the draft in an external editor, as Claude Code's and
// Codex's ctrl+g do (docs/design/editor.md). The shell runs the editor on a
// temporary file holding the draft, placeholders as text, and the saved
// text comes back as the draft: images whose placeholder is gone drop, and
// a placeholder typed in the editor attaches nothing.
type (
	// EditDraft is ctrl+g: open Draft in the editor.
	EditDraft struct{ Draft string }
	// DraftEdited is the editor's result: the saved text, or Err when the
	// editor could not start or exited with an error.
	DraftEdited struct {
		Text string
		Err  error
	}
)

// EffEditDraft runs the editor on Text and answers with DraftEdited.
type EffEditDraft struct{ Text string }

func (EffEditDraft) effect() {}

// onEditor handles the editor's intents; ok is false for any other event.
func (s *State) onEditor(ev any) (effects []Effect, ok bool) {
	switch e := ev.(type) {
	case EditDraft:
		if s.Mode != ModeChat || s.Config != nil {
			return nil, true
		}

		return []Effect{EffEditDraft{Text: e.Draft}}, true
	case DraftEdited:
		if e.Err != nil {
			s.notice(session.LevelWarning, "editor: "+e.Err.Error()+"; the draft is unchanged")

			return nil, true
		}
		text := editedText(e.Text)
		s.pruneImages(text)

		return []Effect{EffSetDraft{Text: text}}, true
	}

	return nil, false
}

// editedText is a saved file as a draft: line ends as \n, and without the
// one newline an editor adds at the end of the last line.
func editedText(saved string) string {
	text := strings.ReplaceAll(saved, "\r\n", "\n")

	return strings.TrimSuffix(text, "\n")
}
