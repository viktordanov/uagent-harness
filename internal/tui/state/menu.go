package state

import (
	"strings"

	"github.com/sahilm/fuzzy"

	"github.com/viktordanov/uagent-harness/internal/models"
	"github.com/viktordanov/uagent-harness/internal/session"
)

// menuSize is how many suggestions the menu shows.
const menuSize = 6

// Menu is the suggestion menu under the composer: commands and their
// arguments after "/", workspace files after "@".
type Menu struct {
	// Index is the selected suggestion.
	Index int
	// Closed is the draft the menu was closed for; it opens again when the
	// draft changes.
	Closed string
	// Files are the workspace's files for "@", loaded on first use.
	Files        []string
	filesLoading bool
	// Models is the provider's model list for /model, loaded on first use.
	Models        *models.Catalog
	modelsLoading bool
}

// Suggestion is one menu entry. Accepting it replaces the draft with Draft.
type Suggestion struct {
	Label string
	Help  string
	Draft string
}

// Menu intents carry the composer's text, which the shell owns.
type (
	// MenuMove moves the selection.
	MenuMove struct {
		Draft string
		Delta int
	}
	// MenuAccept puts the selected suggestion into the composer.
	MenuAccept struct{ Draft string }
	// MenuEnter runs the selected command, or accepts the selection when it
	// still needs an argument or is a file.
	MenuEnter struct{ Draft string }
	// MenuClose hides the menu until the draft changes.
	MenuClose struct{ Draft string }
	// DraftChanged reports a new composer text, so the selection resets and
	// "@" can load the file list.
	DraftChanged struct{ Draft string }
	// FilesLoaded carries the workspace's files for "@".
	FilesLoaded struct{ Paths []string }
)

// Suggestions returns the menu for a draft, or nothing when it is closed.
func (s State) Suggestions(draft string) []Suggestion {
	if draft == "" || draft == s.Menu.Closed || s.Shell {
		return nil
	}
	if at, ok := mentionAt(draft); ok {
		return s.fileSuggestions(draft, at)
	}
	if !strings.HasPrefix(draft, "/") || strings.Contains(draft, "\n") {
		return nil
	}
	name, arg, hasArg := strings.Cut(strings.TrimPrefix(draft, "/"), " ")
	if !hasArg {
		return commandSuggestions(name)
	}

	return s.argSuggestions(name, arg)
}

func commandSuggestions(prefix string) []Suggestion {
	var out []Suggestion
	for _, c := range Complete(prefix) {
		label := "/" + c.Name
		if c.Args != "" {
			label += " " + c.Args
		}
		draft := "/" + c.Name
		if c.Args != "" {
			draft += " "
		}
		out = append(out, Suggestion{Label: label, Help: c.Help, Draft: draft})
	}

	return out
}

// argSuggestions completes a command's argument where the values are known.
func (s State) argSuggestions(name, arg string) []Suggestion {
	var values []string
	switch name {
	case "model":
		return s.modelSuggestions(arg)
	case "effort":
		values = session.Efforts
	case cmdAgentsName:
		values = s.agentNames()
	case "resume":
		for _, in := range s.Picker.Sessions {
			values = append(values, in.ID[:min(8, len(in.ID))])
		}
	default:
		return nil
	}
	var out []Suggestion
	for _, v := range values {
		if strings.HasPrefix(v, arg) && v != arg {
			out = append(out, Suggestion{Label: v, Draft: "/" + name + " " + v})
		}
	}

	return out
}

// mentionAt finds an "@" word at the end of the draft and returns its start.
func mentionAt(draft string) (int, bool) {
	start := strings.LastIndexAny(draft, " \n\t") + 1
	if strings.HasPrefix(draft[start:], "@") {
		return start, true
	}

	return 0, false
}

func (s State) fileSuggestions(draft string, at int) []Suggestion {
	query := draft[at+1:]
	var out []Suggestion
	if query == "" {
		for _, f := range s.Menu.Files[:min(menuSize, len(s.Menu.Files))] {
			out = append(out, Suggestion{Label: f, Draft: draft[:at] + f + " "})
		}

		return out
	}
	for _, m := range fuzzy.Find(query, s.Menu.Files) {
		out = append(out, Suggestion{Label: m.Str, Draft: draft[:at] + m.Str + " "})
		if len(out) == menuSize {
			break
		}
	}

	return out
}

// onMenu handles the menu intents; ok is false for any other event.
func (s *State) onMenu(ev any) (effects []Effect, ok bool) {
	switch e := ev.(type) {
	case DraftChanged:
		s.Menu.Index = 0
		s.pruneImages(e.Draft)
		if eff := s.loadModels(e.Draft); eff != nil {
			return []Effect{eff}, true
		}
		if _, mention := mentionAt(e.Draft); mention && s.Menu.Files == nil && !s.Menu.filesLoading {
			s.Menu.filesLoading = true

			return []Effect{EffLoadFiles{}}, true
		}
	case FilesLoaded:
		s.Menu.Files, s.Menu.filesLoading = e.Paths, false
		if s.Menu.Files == nil {
			s.Menu.Files = []string{}
		}
	case ModelsLoaded:
		s.Menu.Models, s.Menu.modelsLoading = &e.Catalog, false
	case MenuMove:
		if n := min(len(s.Suggestions(e.Draft)), menuSize); n > 0 {
			s.Menu.Index = ((s.Menu.Index+e.Delta)%n + n) % n
		}
	case MenuAccept:
		items := s.Suggestions(e.Draft)
		if len(items) == 0 {
			return nil, true
		}
		picked := items[min(s.Menu.Index, len(items)-1)]
		s.Menu.Index = 0
		if effects, ok := s.acceptImage(e.Draft, picked); ok {
			return effects, true
		}

		return s.setDraft(picked.Draft), true
	case MenuEnter:
		items := s.Suggestions(e.Draft)
		if len(items) == 0 {
			return nil, false // an ordinary enter
		}
		picked := items[min(s.Menu.Index, len(items)-1)]
		s.Menu.Index = 0
		if effects, ok := s.acceptImage(e.Draft, picked); ok {
			return effects, true
		}
		if strings.HasSuffix(picked.Draft, " ") {
			return s.setDraft(picked.Draft), true
		}
		next, effects := s.command(picked.Draft)
		*s = next

		return append([]Effect{EffSetDraft{Text: ""}}, effects...), true
	case MenuClose:
		s.Menu.Closed = e.Draft
	default:
		return nil, false
	}

	return nil, true
}

// setDraft puts an accepted suggestion in the composer and, for /model,
// starts loading the model list.
func (s *State) setDraft(draft string) []Effect {
	effects := []Effect{EffSetDraft{Text: draft}}
	if eff := s.loadModels(draft); eff != nil {
		effects = append(effects, eff)
	}

	return effects
}

// MenuOpen reports whether the draft shows a menu, so the shell sends the
// menu keys to it.
func (s State) MenuOpen(draft string) bool { return len(s.Suggestions(draft)) > 0 }
