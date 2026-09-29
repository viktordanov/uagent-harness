package state

import "path/filepath"

// WindowTitle is the terminal's title: yolo mode, the session's state, and
// its workspace's name, such as "uah · yolo · working · api". It is "" when
// Title is off, and "uah" before a session opens.
func (s State) WindowTitle() string {
	if !s.Title {
		return ""
	}
	title := "uah"
	if s.Settings.Mode.AsksNoOne() {
		title += " · yolo"
	}
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
