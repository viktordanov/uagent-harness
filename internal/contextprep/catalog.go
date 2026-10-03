package contextprep

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
)

// builtinFS holds the built-in modules: the blocks' text under
// environment/, os/, sandbox/, harness/, and agent-files*.md, and the
// library under library/.
//
//go:embed context
var builtinFS embed.FS

// Directories of the modules a user or a project adds.
const (
	// ContextDir is under the user's prompts folder: a file there with a
	// built-in's path replaces it.
	ContextDir = "context"
	// ExtrasDir holds extra modules: under the user's prompts folder, and
	// under the project's .uah folder.
	ExtrasDir = "context.d"
	// maxDirModules caps the files read from one folder.
	maxDirModules = 64
)

// Sources are where a session's modules come from besides the built-ins.
type Sources struct {
	// UserDir is the user's prompts folder (~/.uah/prompts): files under
	// context/ replace built-ins, files in context.d/ add modules. "" reads
	// none.
	UserDir string
	// Workspace is the session's workspace: <workspace>/.uah/context.d
	// adds the project's modules, which apply only once trusted. "" reads
	// none.
	Workspace string
	// Enable are module ids the configuration turns on, such as the
	// library's "go".
	Enable []string
	// Trusted reports whether a project module's TrustKey was trusted for
	// the workspace; nil trusts none.
	Trusted func(key string) bool
	// Check runs a module's check; nil runs none, so a module with a check
	// does not apply.
	Check Checker
}

// Modules are a session's modules: the built-ins with the user's
// replacements, the library, and the extras.
type Modules struct {
	src Sources
	// builtin are the built-ins by path, replaced where the user has a file.
	builtin map[string]*Module
	// extras are the library, the user's, and the project's modules, in
	// that order, each by its file name.
	extras []*Module
	// problems are files that are not modules at all, such as a
	// replacement for a built-in that does not exist.
	problems []*Module

	mu sync.Mutex
	// checked are the checks' results by directory and argv: each check
	// runs once for the Modules.
	checked map[string]error
}

var (
	builtinOnce sync.Once
	builtinList []*Module
)

// Builtins are the built-in and library modules as uah ships them, in
// path order.
func Builtins() []*Module {
	builtinOnce.Do(func() {
		_ = fs.WalkDir(builtinFS, "context", func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(p, ".md") {
				return err
			}
			data, err := builtinFS.ReadFile(p)
			if err != nil {
				return err
			}
			rel := strings.TrimSuffix(strings.TrimPrefix(p, "context/"), ".md")
			m := newModule(rel, SourceBuiltin, "", data)
			if strings.HasPrefix(rel, "library/") {
				m.Source = SourceLibrary
			}
			builtinList = append(builtinList, m)

			return nil
		})
	})

	return builtinList
}

// newModule parses a module's file.
func newModule(rel string, src Source, file string, data []byte) *Module {
	m := &Module{Path: rel, Source: src, File: file, Raw: string(data)}
	m.Meta, m.Body, m.Err = ParseModule(path.Base(rel), data)

	return m
}

// Load reads the session's modules. A file that does not parse is kept,
// with its error, so `uah context` lists it; it never applies.
func Load(src Sources) *Modules {
	ms := &Modules{src: src, builtin: map[string]*Module{}}
	for _, m := range Builtins() {
		if m.Source == SourceLibrary {
			ms.extras = append(ms.extras, m)
		} else {
			ms.builtin[m.Path] = m
		}
	}
	if src.UserDir != "" {
		ms.loadReplacements(filepath.Join(src.UserDir, ContextDir))
		ms.loadExtras(filepath.Join(src.UserDir, ExtrasDir), SourceUser)
	}
	if src.Workspace != "" {
		ms.loadExtras(filepath.Join(src.Workspace, ".uah", ExtrasDir), SourceProject)
	}

	return ms
}

// loadReplacements reads the user's files that replace built-ins: the
// file at <dir>/<path>.md replaces the module at path.
func (ms *Modules) loadReplacements(dir string) {
	var files []string
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".md") && len(files) < maxDirModules*2 {
			files = append(files, p)
		}

		return nil
	})
	for _, file := range files {
		rel, err := filepath.Rel(dir, file)
		if err != nil {
			continue
		}
		rel = strings.TrimSuffix(filepath.ToSlash(rel), ".md")
		m := readModule(rel, SourceUser, file)
		m.Overrides = true
		if m.Err != nil {
			ms.problems = append(ms.problems, m) // the built-in stays

			continue
		}
		if _, ok := ms.builtin[rel]; ok {
			ms.builtin[rel] = m
		} else if i := slices.IndexFunc(ms.extras, func(e *Module) bool { return e.Source == SourceLibrary && e.Path == rel }); i >= 0 {
			ms.extras[i] = m
		} else {
			m.Err = fmt.Errorf("no built-in module %s to replace", rel)
			ms.problems = append(ms.problems, m)
		}
	}
}

// loadExtras reads <dir>/*.md as extra modules.
func (ms *Modules) loadExtras(dir string, src Source) {
	matches, _ := filepath.Glob(filepath.Join(dir, "*.md"))
	sort.Strings(matches)
	if len(matches) > maxDirModules {
		matches = matches[:maxDirModules]
	}
	for _, file := range matches {
		m := readModule(ExtrasDir+"/"+strings.TrimSuffix(filepath.Base(file), ".md"), src, file)
		if m.Err == nil {
			if other := ms.extra(m.Meta.ID); other != nil {
				m.Err = fmt.Errorf("id %s is taken by %s", m.Meta.ID, other.where())
			}
		}
		ms.extras = append(ms.extras, m)
	}
}

// readModule reads and parses a module file.
func readModule(rel string, src Source, file string) *Module {
	info, err := os.Stat(file)
	if err == nil && (!info.Mode().IsRegular() || info.Size() > MaxModuleBytes) {
		err = fmt.Errorf("not a regular file of at most %d bytes", MaxModuleBytes)
	}
	var data []byte
	if err == nil {
		data, err = os.ReadFile(file)
	}
	if err != nil {
		return &Module{Path: rel, Source: src, File: file, Err: err}
	}

	return newModule(rel, src, file, data)
}

// extra is the usable extra module with the id, or nil.
func (ms *Modules) extra(id string) *Module {
	for _, m := range ms.extras {
		if m.Err == nil && m.Meta.ID == id {
			return m
		}
	}

	return nil
}

// where names the module for a message.
func (m *Module) where() string {
	if m.File != "" {
		return m.File
	}

	return "the built-in " + m.Path
}

// All are every module, in the order `uah context` lists them: the
// built-ins, the library and the extras, then the files that are not
// modules.
func (ms *Modules) All() []*Module {
	paths := make([]string, 0, len(ms.builtin))
	for p := range ms.builtin {
		paths = append(paths, p)
	}
	order := sectionOrder()
	sort.SliceStable(paths, func(i, j int) bool {
		a, b := slices.Index(order, paths[i]), slices.Index(order, paths[j])
		if a != b {
			return a < b
		}

		return paths[i] < paths[j]
	})
	out := make([]*Module, 0, len(paths)+len(ms.extras)+len(ms.problems))
	for _, p := range paths {
		out = append(out, ms.builtin[p])
	}
	out = append(out, ms.extras...)

	return append(out, ms.problems...)
}

// Enabled reports whether the module is on: its front matter's enabled,
// true by default; an extra module the front matter turns off is on when
// the configuration names its id.
func (ms *Modules) Enabled(m *Module) bool {
	if m.Meta.Enabled == nil || *m.Meta.Enabled {
		return true
	}

	return !ms.isBlockModule(m) && slices.Contains(ms.src.Enable, m.Meta.ID)
}

// isBlockModule reports whether the module belongs to a built-in block,
// which the configuration's list does not name.
func (ms *Modules) isBlockModule(m *Module) bool {
	_, ok := ms.builtin[m.Path]

	return ok
}

// Trusted reports whether the module may be used: a project's only once
// trusted.
func (ms *Modules) Trusted(m *Module) bool {
	return m.Source != SourceProject || (ms.src.Trusted != nil && ms.src.Trusted(TrustKey(m)))
}

// TrustKey is what trusting a project module records, with the
// workspace: its path and the SHA-256 of its content, so a changed file
// needs trust again.
func TrustKey(m *Module) string {
	sum := sha256.Sum256([]byte(m.Raw))

	return "#context-module " + m.Path + " sha256:" + hex.EncodeToString(sum[:])
}

// ErrNoModule is returned by BuiltinFile for an unknown path.
var ErrNoModule = errors.New("no such built-in module")

// BuiltinFile is the file of the built-in module at path, as uah ships it.
func BuiltinFile(p string) (string, error) {
	for _, m := range Builtins() {
		if m.Path == p {
			return m.Raw, nil
		}
	}

	return "", ErrNoModule
}

// Settings are a setup's module settings, the same for every session:
// the user's prompts folder, the ids the configuration turns on, and the
// trust of project modules.
type Settings struct {
	// UserDir is the user's prompts folder (~/.uah/prompts), or "".
	UserDir string
	// Enable are the ids [context] modules turns on.
	Enable []string
	// Trusted reports whether a project module's TrustKey was trusted in
	// the workspace; nil trusts none.
	Trusted func(workspace, key string) bool
}

// Sources are the settings for a session in workspace, with its checker.
func (s Settings) Sources(workspace string, check Checker) Sources {
	src := Sources{UserDir: s.UserDir, Workspace: workspace, Enable: s.Enable, Check: check}
	if s.Trusted != nil {
		src.Trusted = func(key string) bool { return s.Trusted(workspace, key) }
	}

	return src
}
