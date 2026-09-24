// Package agents runs subagents: child sessions a session's agent spawns
// with the spawn_agent tool, following Codex's v1 multi-agent tools (see
// docs/design/subagents.md). It implements engine.Subagents.
package agents

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// Role is an agent type from a Codex role file. uah reads the subset of
// Codex's keys that map to its settings.
type Role struct {
	Name               string   `toml:"name"`
	Description        string   `toml:"description"`
	NicknameCandidates []string `toml:"nickname_candidates"`
	// Model and Effort override the parent's for agents of this role.
	Model  string `toml:"model"`
	Effort string `toml:"model_reasoning_effort"`
	// ServiceTier is Codex's service_tier: "priority" (or its legacy name
	// "fast") runs the agent with priority processing, and "default" without
	// it, whatever the parent uses. Empty follows the parent.
	ServiceTier string `toml:"service_tier"`
	// DeveloperInstructions are added to the agent's system prompt.
	DeveloperInstructions string `toml:"developer_instructions"`
	// Path is the file the role came from.
	Path string `toml:"-"`
}

// LoadRoles reads the *.toml role files under each directory, later
// directories replacing earlier roles of the same name, as Codex's config
// layers do. Missing directories are skipped. A malformed file is left out
// with a warning, as Codex does; so are keys uah does not support.
func LoadRoles(dirs ...string) (roles []Role, warnings []string) {
	byName := map[string]Role{}
	for _, dir := range dirs {
		files, err := roleFiles(dir)
		if err != nil {
			warnings = append(warnings, err.Error())

			continue
		}
		for _, path := range files {
			r, ignored, err := readRole(path)
			if err != nil {
				warnings = append(warnings, "ignoring malformed agent role: "+err.Error())

				continue
			}
			if len(ignored) > 0 {
				warnings = append(warnings, fmt.Sprintf("agent role %s: ignoring keys uah does not support: %s", path, strings.Join(ignored, ", ")))
			}
			byName[r.Name] = r
		}
	}
	for _, r := range byName {
		roles = append(roles, r)
	}
	slices.SortFunc(roles, func(a, b Role) int { return strings.Compare(a.Name, b.Name) })

	return roles, warnings
}

// roleFiles lists the .toml files under dir, recursively, sorted.
func roleFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Ext(path) == ".toml" {
			files = append(files, path)
		}

		return nil
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to list agent roles in %s: %w", dir, err)
	}
	sort.Strings(files)

	return files, nil
}

// readRole parses and checks one role file, returning the keys it ignored.
func readRole(path string) (Role, []string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Role{}, nil, fmt.Errorf("failed to read %s: %w", path, err)
	}
	var r Role
	meta, err := toml.Decode(string(data), &r)
	if err != nil {
		return Role{}, nil, fmt.Errorf("failed to parse %s: %w", path, err)
	}
	var ignored []string
	for _, k := range meta.Undecoded() {
		ignored = append(ignored, k[0])
	}
	if tier, ok := serviceTier(r.ServiceTier); ok {
		r.ServiceTier = tier
	} else {
		ignored = append(ignored, fmt.Sprintf("service_tier = %q (want priority, fast, or default)", r.ServiceTier))
		r.ServiceTier = ""
	}
	slices.Sort(ignored)
	r.Path = path
	r.Name, r.Description = strings.TrimSpace(r.Name), strings.TrimSpace(r.Description)
	if err := r.check(); err != nil {
		return Role{}, nil, fmt.Errorf("%s: %w", path, err)
	}

	return r, slices.Compact(ignored), nil
}

// check applies Codex's rules for a role file.
func (r Role) check() error {
	switch {
	case r.Name == "":
		return errors.New("the role must define a non-empty name")
	case r.Description == "":
		return errors.New("the role must define a non-empty description")
	case strings.TrimSpace(r.DeveloperInstructions) == "":
		return errors.New("the role must define developer_instructions")
	}
	seen := map[string]bool{}
	for _, n := range r.NicknameCandidates {
		n = strings.TrimSpace(n)
		valid := n != "" && strings.IndexFunc(n, func(c rune) bool { return !nicknameRune(c) }) < 0
		if !valid || seen[n] {
			return fmt.Errorf("invalid nickname candidate %q (ASCII letters, digits, spaces, hyphens, and underscores, no duplicates)", n)
		}
		seen[n] = true
	}
	if r.NicknameCandidates != nil && len(r.NicknameCandidates) == 0 {
		return errors.New("nickname_candidates must contain at least one name")
	}

	return nil
}

// nicknameRune reports whether c may appear in a nickname: ASCII letters,
// digits, spaces, hyphens, and underscores.
func nicknameRune(c rune) bool {
	return c == ' ' || c == '-' || c == '_' || ('0' <= c && c <= '9') || ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
}

// defaultRole is the agent type of a child spawned without one.
const defaultRole = "default"

// Role service tiers: Codex's request values that uah's providers serve.
const (
	TierPriority = "priority"
	TierDefault  = "default"
)

// serviceTier normalizes a role's service_tier as Codex reads it: "fast"
// is the legacy name of "priority". Codex also sends "flex", which no
// provider of uah's serves, so it is not ok.
func serviceTier(v string) (string, bool) {
	switch strings.TrimSpace(v) {
	case "":
		return "", true
	case "priority", "fast":
		return TierPriority, true
	case TierDefault:
		return TierDefault, true
	}

	return "", false
}
