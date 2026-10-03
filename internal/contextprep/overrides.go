package contextprep

import (
	"sort"
)

// DefaultsDir is under the user's prompts folder: `uah prompts init`
// writes the built-in modules there as a reference to copy from. uah never
// reads it, so the built-ins stay in use and a later version's text reaches
// the session.
const DefaultsDir = "context.defaults"

// Override is a user file under <prompts>/context that names a built-in's
// path, as `uah prompts status` lists it.
type Override struct {
	// Path is the module's path, such as "environment/fish".
	Path string
	// File is the file.
	File string
	// Pinned is true when the file is byte for byte the built-in as this
	// version ships it: it changes nothing today, but while it exists a
	// later version's text of the module does not reach the session.
	Pinned bool
	// Err is why the file is not used: it does not parse, or names no
	// built-in. The built-in stays.
	Err error
}

// Overrides are the user's files under <userDir>/context, in path order.
func Overrides(userDir string) []Override {
	var out []Override
	for _, m := range Load(Sources{UserDir: userDir}).All() {
		if !m.Overrides {
			continue
		}
		out = append(out, Override{Path: m.Path, File: m.File, Pinned: pinned(m), Err: m.Err})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })

	return out
}

// pinned reports whether m is a user file identical to the built-in it
// replaces.
func pinned(m *Module) bool {
	if !m.Overrides || m.Err != nil {
		return false
	}
	raw, err := BuiltinFile(m.Path)

	return err == nil && raw == m.Raw
}
