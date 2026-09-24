package app

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/viktordanov/uagent-harness/internal/compaction"
	"github.com/viktordanov/uagent-harness/internal/config"
	"github.com/viktordanov/uagent-harness/internal/models"
	"github.com/viktordanov/uagent-harness/internal/session"
)

// Environment variables behind flags.
const (
	EnvProvider = "UNREAL_HARNESS_LLM_PROVIDER"
	EnvModel    = "UNREAL_HARNESS_LLM_MODEL"
	EnvEngine   = "UAH_ENGINE"
	EnvSandbox  = "UAH_SANDBOX"
	EnvAsk      = "UAH_ASK"
	// EnvMaxAttempts is the runner's variable for the attempt limit.
	EnvMaxAttempts = "UNREAL_HARNESS_LLM_MAX_ATTEMPTS"
)

// Source is where an effective setting came from.
type Source string

// Sources, from the strongest to the weakest.
const (
	FromFlag    Source = "flag"
	FromEnv     Source = "env"
	FromSession Source = "session"
	FromProject Source = "project file"
	FromUser    Source = "user file"
	FromDefault Source = "default"
)

// Setting is one effective value and where it came from: one source, or
// several for keys whose files add up.
type Setting struct {
	Key     string   `json:"key"`
	Value   any      `json:"value"`
	Sources []Source `json:"sources"`
}

// File is a configuration file and what became of it: read, not found, or
// not trusted.
type File struct {
	Path  string `json:"path"`
	State string `json:"state"`
}

// Report is the effective configuration for a workspace.
type Report struct {
	Workspace string `json:"workspace"`
	// WorkspaceSource is the workspace's source: a flag, the resumed
	// session, or the default (the current directory).
	WorkspaceSource Source    `json:"workspace_source"`
	UserFile        File      `json:"user_file"`
	ProjectFile     File      `json:"project_file"`
	Settings        []Setting `json:"settings"`
}

// Origins are what Explain weighs besides the inputs.
type Origins struct {
	// Env holds the environment variables behind flags, by name. A flag
	// value equal to its variable's counts as from the environment.
	Env     map[string]string
	Resumed session.Info
	Layers  config.Layers
	// Dir is the current directory, the default workspace.
	Dir string
}

// Inspect loads what Setup loads, the resumed session and the configuration
// files, and explains the effective configuration. It builds no engine.
func Inspect(ctx context.Context, in Inputs) (Report, error) {
	o := Origins{Env: map[string]string{}}
	for _, name := range []string{EnvProvider, EnvModel, EnvEngine, EnvSandbox, EnvAsk, EnvMaxAttempts} {
		o.Env[name] = os.Getenv(name)
	}
	var err error
	if o.Dir, err = os.Getwd(); err != nil {
		return Report{}, fmt.Errorf("failed to find the current directory: %w", err)
	}
	if in.SessionRef != "" {
		stateDir, err := filepath.Abs(in.StateDir)
		if err != nil {
			return Report{}, fmt.Errorf("failed to resolve state dir: %w", err)
		}
		if o.Resumed, err = FindSession(ctx, stateDir, in.SessionRef); err != nil {
			return Report{}, err
		}
	}
	if o.Layers, err = config.LoadLayers(in.ConfigPath, workspaceIn(in, o)); err != nil {
		return Report{}, usage(err)
	}

	return Explain(in, o)
}

// Explain reports the effective configuration, as Resolve decides it, with
// each value's source. It does no I/O.
func Explain(in Inputs, o Origins) (Report, error) {
	wsSource := pick(input(in.Workspace, "", nil), sessionValue(o.Resumed.Workspace), FromDefault)
	in.Workspace = workspaceIn(in, o)
	cfg := o.Layers.Merged()
	r, err := Resolve(in, o.Resumed, cfg)
	if err != nil {
		return Report{}, err
	}
	rep := Report{Workspace: in.Workspace, WorkspaceSource: wsSource, UserFile: File{Path: in.ConfigPath, State: "read"}, ProjectFile: File{Path: config.ProjectFile(in.Workspace), State: "read"}}
	if o.Layers.UserFile == "" {
		rep.UserFile.State = "not found"
	}
	switch {
	case !o.Layers.Trusted:
		rep.ProjectFile.State = "not trusted"
	case o.Layers.ProjectFile == "":
		rep.ProjectFile.State = "not found"
	}
	rep.Settings = slices.Concat(sessionSettings(in, o, r, cfg), fileSettings(in.Workspace, o.Layers, r, cfg))

	return rep, nil
}

// workspaceIn is the absolute workspace: the flag, the resumed session's,
// or the current directory.
func workspaceIn(in Inputs, o Origins) string {
	ws := first(in.Workspace, o.Resumed.Workspace, o.Dir)
	if !filepath.IsAbs(ws) {
		ws = filepath.Join(o.Dir, ws)
	}

	return filepath.Clean(ws)
}

// sessionSettings are the settings a flag, the environment, or the resumed
// session can set, besides the workspace.
func sessionSettings(in Inputs, o Origins, r Resolved, cfg config.Config) []Setting {
	l, env, resumed := o.Layers, o.Env, o.Resumed
	s := r.Settings
	modelSource := pick(input(in.Model, EnvModel, env), sessionValue(resumed.Model), overrides(l, func(c config.Config) any { return c.Model }), FromDefault)
	if providerChanged(in, resumed, cfg) {
		modelSource = pick(input(in.Model, EnvModel, env), FromDefault)
	}
	maxDisk := in.MaxDisk
	if !in.MaxDiskSet && cfg.MaxDisk != "" {
		maxDisk = cfg.MaxDisk
	}

	return []Setting{
		one("provider", s.Provider, pick(input(in.Provider, EnvProvider, env), sessionValue(resumed.Provider), overrides(l, func(c config.Config) any { return c.Provider }), FromDefault)),
		one("model", s.Model, modelSource),
		one("effort", s.Effort, pick(input(in.Effort, "", nil), sessionValue(resumed.Effort), overrides(l, func(c config.Config) any { return c.Effort }), FromDefault)),
		one("timeout", s.Timeout.String(), pick(given(in.TimeoutSet), overrides(l, func(c config.Config) any { return c.Timeout }), FromDefault)),
		one("request_max_attempts", s.MaxAttempts, pick(attemptsInput(in, env), overrides(l, func(c config.Config) any { return c.RequestMaxAttempts }), FromDefault)),
		one("max_disk", maxDisk, pick(given(in.MaxDiskSet), overrides(l, func(c config.Config) any { return c.MaxDisk }), FromDefault)),
		one("engine", r.Engine, pick(input(in.Engine, EnvEngine, env), overrides(l, func(c config.Config) any { return c.Engine }), FromDefault)),
		{Key: "fast", Value: s.ServiceTier != "", Sources: fastSources(in, o, cfg)},
		one("permission_mode", string(s.Mode), modeSource(in, o)),
		one("sandbox_mode", string(r.Sandbox.Mode), modeSource(in, o)),
		one("approval_policy", string(r.Approval), pick(input(in.Ask, EnvAsk, env), overrides(l, func(c config.Config) any { return c.ApprovalPolicy }), FromDefault)),
		one("model_context_window", compaction.ContextWindow(s.Model, s.ContextWindow, models.BundledWindow), pick(overrides(l, func(c config.Config) any { return c.ModelContextWindow }), FromDefault)),
		{Key: "instructions.enabled", Value: r.Instructions, Sources: orSources(given(in.NoInstructions), []Source{overrides(l, func(c config.Config) any { return c.Instructions.Enabled })})},
	}
}

// attemptsInput is the source of --max-attempts: its environment variable
// when the value is the variable's ("" when unset).
func attemptsInput(in Inputs, env map[string]string) Source {
	if in.MaxAttempts == 0 {
		return ""
	}

	return input(strconv.Itoa(in.MaxAttempts), EnvMaxAttempts, env)
}

// fastSources are the fast mode's: the flag, the resumed session, or the
// files, as pickFast decides.
func fastSources(in Inputs, o Origins, cfg config.Config) []Source {
	if !in.FastSet && sessionFast(in, o.Resumed, cfg) {
		return []Source{FromSession}
	}

	return orSources(given(in.FastSet), adds(o.Layers, func(c config.Config) any { return c.Fast }))
}

// modeSource is the permission mode's source, and the sandbox mode's that
// follows from it: --sandbox, the resumed session, permission_mode, or
// sandbox_mode, as pickMode decides.
func modeSource(in Inputs, o Origins) Source {
	l := o.Layers

	return pick(input(in.Sandbox, EnvSandbox, o.Env), sessionValue(string(o.Resumed.Mode)),
		overrides(l, func(c config.Config) any { return c.PermissionMode }),
		overrides(l, func(c config.Config) any { return c.SandboxMode }), FromDefault)
}

// Text is the value as one line of text.
func (s Setting) Text() string {
	switch v := s.Value.(type) {
	case string:
		if v == "" {
			return `""`
		}

		return v
	case []string:
		return "[" + strings.Join(v, ", ") + "]"
	case map[string]string:
		keys := slices.Sorted(maps.Keys(v))
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			parts = append(parts, k+"="+strconv.Quote(v[k]))
		}

		return "{" + strings.Join(parts, ", ") + "}"
	}

	return fmt.Sprint(s.Value)
}

// SourceText is the sources joined with " + ".
func (s Setting) SourceText() string {
	parts := make([]string, 0, len(s.Sources))
	for _, src := range s.Sources {
		parts = append(parts, string(src))
	}

	return strings.Join(parts, " + ")
}

func one(key string, value any, src Source) Setting {
	return Setting{Key: key, Value: value, Sources: []Source{src}}
}

// input is the source of an input value: the environment when the value is
// its variable's, else a flag ("" when unset).
func input(value, envName string, env map[string]string) Source {
	switch {
	case value == "":
		return ""
	case envName != "" && env[envName] == value:
		return FromEnv
	}

	return FromFlag
}

func given(set bool) Source {
	if set {
		return FromFlag
	}

	return ""
}

func sessionValue(v string) Source {
	if v != "" {
		return FromSession
	}

	return ""
}

// pick is the first non-empty source.
func pick(sources ...Source) Source {
	for _, s := range sources {
		if s != "" {
			return s
		}
	}

	return ""
}

// orSources is the flag when given, else the files, else the default.
func orSources(flag Source, files []Source) []Source {
	files = slices.DeleteFunc(files, func(s Source) bool { return s == "" })
	switch {
	case flag != "":
		return []Source{flag}
	case len(files) > 0:
		return files
	}

	return []Source{FromDefault}
}

// overrides is the file whose value wins for a key the project file
// overrides: the project file when it sets it, else the user file when it
// does, else "".
func overrides(l config.Layers, get func(config.Config) any) Source {
	return pick(setIn(l.Project, get, FromProject), setIn(l.User, get, FromUser))
}

// adds is every file that sets a key whose files add up, the user file
// first, or the default when neither does.
func adds(l config.Layers, get func(config.Config) any) []Source {
	var out []Source
	for _, s := range []Source{setIn(l.User, get, FromUser), setIn(l.Project, get, FromProject)} {
		if s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return []Source{FromDefault}
	}

	return out
}

// setIn is src when get's value in c is not the zero value.
func setIn(c config.Config, get func(config.Config) any, src Source) Source {
	v := reflect.ValueOf(get(c))
	if !v.IsValid() || v.IsZero() {
		return ""
	}

	return src
}
