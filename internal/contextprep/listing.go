package contextprep

import "context"

// Status is one module as `uah context` lists it: where it comes from,
// whether it is on, and whether it applies to the session and why.
type Status struct {
	Path        string   `json:"path"`
	ID          string   `json:"id,omitempty"`
	Description string   `json:"description,omitempty"`
	Source      Source   `json:"source"`
	File        string   `json:"file,omitempty"`
	Block       string   `json:"block"`
	Overrides   bool     `json:"overrides,omitempty"`
	Enabled     bool     `json:"enabled"`
	Trusted     bool     `json:"trusted"`
	Files       []string `json:"files,omitempty"`
	Check       []string `json:"check,omitempty"`
	Applies     bool     `json:"applies"`
	Reason      string   `json:"reason"`
}

// Explain lists every module with whether it applies to the session
// described by f, running the checks of the modules that would otherwise
// apply.
func (ms *Modules) Explain(ctx context.Context, f Facts) []Status {
	all := ms.All()
	out := make([]Status, 0, len(all))
	for _, m := range all {
		r := ms.Evaluate(ctx, m, f)
		block := blockOf(m.Path)
		if block == "" {
			block = m.Meta.ID
		}
		out = append(out, Status{
			Path: m.Path, ID: m.Meta.ID, Description: m.Meta.Description, Source: m.Source, File: m.File, Block: block,
			Overrides: m.Overrides, Enabled: m.Err == nil && ms.Enabled(m), Trusted: ms.Trusted(m),
			Files: m.Meta.Files, Check: m.Meta.Check, Applies: r.Applies, Reason: r.Reason,
		})
	}

	return out
}

// Untrusted are the project's modules that are not trusted, for `uah
// context trust`.
func (ms *Modules) Untrusted() []*Module {
	var out []*Module
	for _, m := range ms.extras {
		if m.Source == SourceProject && m.Err == nil && !ms.Trusted(m) {
			out = append(out, m)
		}
	}

	return out
}
