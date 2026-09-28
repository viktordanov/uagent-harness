package state

import "path/filepath"

// WindowTitle is the terminal's title: the session's state and its
// workspace's name, such as "uah · working · api". It is "" when Title is
// off, and "uah" before a session opens.
func (s State) WindowTitle() string {
	if !s.Title {
		return ""
	}
	title := "uah"
	switch {
	case len(s.Approvals) > 0:
		title += " · approve?"
	case s.Busy:
		title += " · working"
	}
	if s.Settings.Workspace == "" {
		return title
	}

	return title + " · " + filepath.Base(s.Settings.Workspace)
}

// Working reports whether the terminal shows progress (OSC 9;4): while the
// agent works, also while it waits for an approval, and only with Title on.
func (s State) Working() bool { return s.Title && s.Busy }
