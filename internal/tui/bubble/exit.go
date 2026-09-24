package bubble

import "github.com/viktordanov/uagent/core"

// Exit is what the TUI leaves behind when it quits: the session that was
// open and its token totals, for the summary uah prints once the screen is
// restored, as Codex prints "To continue this session, run: codex resume".
type Exit struct {
	SessionID string
	// Resumable means the session ran at least once, so resuming it
	// brings something back; an empty new session is not worth naming.
	Resumable bool
	Tokens    core.Tokens
}

// Exit is the model's exit summary.
func (m Model) Exit() Exit {
	return Exit{
		SessionID: m.st.SessionID,
		Resumable: m.st.SessionID != "" && (m.st.Totals.Runs > 0 || m.st.Live != nil),
		Tokens:    m.st.Totals.Tokens,
	}
}
