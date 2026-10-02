// Package perf is uah's performance harness: it builds sessions of known
// sizes, opens and drives them as `uah` and the TUI do, against a scripted
// fake model (testing/fakellm), and measures what each scenario costs
// outside the model: wall and CPU time, allocations, the heap, goroutines,
// connections, and disk writes. tools/perf runs it from the command line;
// its test runs the small sizes with generous bounds.
package perf

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uah/internal/app"
	"github.com/viktordanov/uah/internal/approval"
	"github.com/viktordanov/uah/internal/home"
	"github.com/viktordanov/uah/internal/sandbox"
	"github.com/viktordanov/uah/internal/session"
	"github.com/viktordanov/uah/testing/fakellm"
)

// Provider and Model are what every session of the harness asks for: the
// openai provider at the fake model's URL, and a model the bundled catalog
// gives apply_patch.
const (
	Provider = "openai"
	Model    = "gpt-6-sol"
	Effort   = "high"
)

// waitTimeout bounds every wait for a session event.
const waitTimeout = 2 * time.Minute

// Env is one isolated uah: a home (the state directory), a workspace, a
// fake model, and the process environment pointing at them, so nothing
// reads the user's ~/.uah, ~/.codex, or configuration.
type Env struct {
	Root      string
	Home      string
	Workspace string
	LLM       *fakellm.Server
	// Yolo runs without a sandbox: set where uah has none (Sandboxed).
	Yolo bool
}

// Sandboxed reports whether commands run in uah's default sandbox
// (workspace-write) here; where they cannot, the harness runs in yolo
// mode, as Linux without bubblewrap must.
func Sandboxed(workspace string) bool {
	_, err := sandbox.Policy{Mode: sandbox.WorkspaceWrite, Workspace: workspace}.Wrap([]string{"/bin/sh"}) // on Linux, Wrap probes bwrap once per process, with its own timeout

	return err == nil
}

// NewEnv makes an environment under root (which it creates) and starts the
// fake model. It sets the process environment: HOME, UAH_HOME, CODEX_HOME,
// and the XDG directories move under root, and OPENAI_API_KEY is a dummy.
func NewEnv(root string) (*Env, error) {
	e := &Env{Root: root, Home: filepath.Join(root, "home", ".uah"), Workspace: filepath.Join(root, "workspace")}
	for _, dir := range []string{e.Home, e.Workspace, filepath.Join(root, "home", ".codex"), filepath.Join(root, "xdg")} {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("failed to make %s: %w", dir, err)
		}
	}
	vars := map[string]string{
		"HOME": filepath.Join(root, "home"), "UAH_HOME": e.Home, "CODEX_HOME": filepath.Join(root, "home", ".codex"),
		"XDG_CONFIG_HOME": filepath.Join(root, "xdg", "config"), "XDG_STATE_HOME": filepath.Join(root, "xdg", "state"),
		"XDG_CACHE_HOME": filepath.Join(root, "xdg", "cache"), "OPENAI_API_KEY": "perf-key", "SHELL": "/bin/sh",
	}
	for k, v := range vars {
		if err := os.Setenv(k, v); err != nil {
			return nil, fmt.Errorf("failed to set %s: %w", k, err)
		}
	}
	for _, k := range home.Variables { // they would move uah's files or bring the user's settings in
		if err := os.Unsetenv(k); err != nil {
			return nil, fmt.Errorf("failed to unset %s: %w", k, err)
		}
	}
	e.LLM = fakellm.Start()
	e.Yolo = !Sandboxed(e.Workspace)

	return e, nil
}

// Close stops the fake model.
func (e *Env) Close() { e.LLM.Close() }

// Open opens a session as `uah` does (app.Setup, then session.Open): a new
// one when id is empty, otherwise it resumes id. Interactive sessions
// stream text, as the TUI's do.
func (e *Env) Open(ctx context.Context, id string, interactive bool) (*session.Session, *app.Result, error) {
	st, err := app.Setup(ctx, app.Inputs{
		StateDir: e.Home, SessionRef: id, Provider: Provider, Model: Model, Effort: Effort,
		Workspace: e.Workspace, BaseURL: e.LLM.URL, NoInstructions: true, MaxDisk: "5G", LogLevel: "error", Yolo: e.Yolo,
	}, io.Discard)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to set up the session: %w", err)
	}
	st.Options.Source, st.Options.Interactive, st.Options.Stream = session.SourceTUI, interactive, interactive
	s, err := session.Open(ctx, st.Engine, st.Options)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open the session: %w", err)
	}

	return s, &st, nil
}

// Turn submits text and waits until the session is idle again, approving
// whatever it asks. It returns the events of the turn.
func Turn(s *session.Session, text string) ([]core.Event, error) {
	if _, err := s.Submit(text); err != nil {
		return nil, fmt.Errorf("failed to submit: %w", err)
	}

	return untilIdle(s)
}

// untilIdle reads events until the run that is starting or running has
// finished and the session is idle.
func untilIdle(s *session.Session) ([]core.Event, error) {
	var all []core.Event
	finished := false
	deadline := time.After(waitTimeout)
	for {
		select {
		case e, ok := <-s.Events():
			if !ok {
				return all, errors.New("the session closed during the turn")
			}
			all = append(all, e)
			switch v := e.(type) {
			case session.ApprovalRequested:
				if err := s.Resolve(v.ID, approval.Approve); err != nil {
					return all, fmt.Errorf("failed to approve: %w", err)
				}
			case core.RunFinished:
				finished = true
				if v.Result.Status != core.StatusOK {
					return all, fmt.Errorf("the run ended %s", v.Result.Status)
				}
			case session.Idle:
				if finished {
					return all, nil
				}
			}
		case <-deadline:
			return all, errors.New("timed out waiting for the turn to end")
		}
	}
}
