// Package config reads uah's configuration: a user file and, for workspaces
// the user trusts, a project file. Unknown keys are errors, so typos surface.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/BurntSushi/toml"
)

// Config holds defaults below flags, the environment, and a resumed session.
type Config struct {
	Provider string `toml:"provider"`
	Model    string `toml:"model"`
	Effort   string `toml:"effort"`
	Timeout  string `toml:"timeout"`
	MaxDisk  string `toml:"max_disk"`
	// Engine is "embedded" (the default) or "process".
	Engine string `toml:"engine"`
	// Fast asks for priority processing on the embedded engine.
	Fast bool `toml:"fast"`

	Instructions Instructions `toml:"instructions"`
	TUI          TUI          `toml:"tui"`

	// Projects are keyed by absolute workspace path.
	Projects map[string]Project `toml:"projects"`
}

// Instructions configure instruction files.
type Instructions struct {
	// Enabled defaults to true.
	Enabled  *bool `toml:"enabled"`
	MaxBytes int   `toml:"max_bytes"`
}

// TUI configures the terminal UI.
type TUI struct {
	// Details starts in the detailed view (ctrl+t toggles it).
	Details bool `toml:"details"`
}

// Project is per-workspace configuration from the user file.
type Project struct {
	// Trusted allows <workspace>/.uagent/config.toml to apply.
	Trusted bool `toml:"trusted"`
}

// InstructionsEnabled reports whether instruction files should be loaded.
func (c Config) InstructionsEnabled() bool {
	return c.Instructions.Enabled == nil || *c.Instructions.Enabled
}

// TimeoutValue parses Timeout; ok is false when it is unset.
func (c Config) TimeoutValue() (d time.Duration, ok bool, err error) {
	if c.Timeout == "" {
		return 0, false, nil
	}
	d, err = time.ParseDuration(c.Timeout)
	if err != nil {
		return 0, false, fmt.Errorf("invalid timeout %q: %w", c.Timeout, err)
	}

	return d, true, nil
}

// Dir is $XDG_CONFIG_HOME/uagent or ~/.config/uagent.
func Dir() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return filepath.Join(dir, "uagent")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".", ".uagent-config")
	}

	return filepath.Join(home, ".config", "uagent")
}

// UserFile is the user configuration file in Dir.
func UserFile() string { return filepath.Join(Dir(), "config.toml") }

// ProjectFile is a workspace's project configuration file.
func ProjectFile(workspace string) string {
	return filepath.Join(workspace, ".uagent", "config.toml")
}

// Load reads the user file at userPath and, when the user file trusts the
// workspace, its project file, which overrides the user file. It returns the
// files it read. Missing files are not errors.
func Load(userPath, workspace string) (Config, []string, error) {
	var cfg Config
	var loaded []string
	found, err := decode(userPath, &cfg)
	if err != nil {
		return Config{}, nil, err
	}
	if found {
		loaded = append(loaded, userPath)
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return Config{}, nil, fmt.Errorf("failed to resolve workspace: %w", err)
	}
	if !cfg.Projects[abs].Trusted {
		return cfg, loaded, nil
	}
	var project Config
	path := ProjectFile(abs)
	found, err = decode(path, &project)
	if err != nil {
		return Config{}, nil, err
	}
	if !found {
		return cfg, loaded, nil
	}
	if len(project.Projects) > 0 {
		return Config{}, nil, fmt.Errorf("%s: [projects] belongs in the user file only", path)
	}

	return merge(cfg, project), append(loaded, path), nil
}

func decode(path string, into *Config) (bool, error) {
	meta, err := toml.DecodeFile(path, into)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("failed to read %s: %w", path, err)
	}
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		return false, fmt.Errorf("%s: unknown key %q", path, undecoded[0].String())
	}

	return true, nil
}

// merge returns base with every value set in over replacing it.
func merge(base, over Config) Config {
	set := func(dst *string, v string) {
		if v != "" {
			*dst = v
		}
	}
	set(&base.Provider, over.Provider)
	set(&base.Model, over.Model)
	set(&base.Effort, over.Effort)
	set(&base.Timeout, over.Timeout)
	set(&base.MaxDisk, over.MaxDisk)
	if over.Instructions.Enabled != nil {
		base.Instructions.Enabled = over.Instructions.Enabled
	}
	if over.Instructions.MaxBytes != 0 {
		base.Instructions.MaxBytes = over.Instructions.MaxBytes
	}

	return base
}
