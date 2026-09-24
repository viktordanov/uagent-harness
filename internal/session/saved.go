package session

import (
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/viktordanov/uagent-harness/internal/approval"
)

// Saved are the settings a session keeps in its sidecar whenever they
// change, so a resumed session starts with what it last used: its model
// (with the provider it belongs to), effort, fast mode, and permission
// mode. A flag still wins over them, and they win over the configuration.
type Saved struct {
	Provider string        `json:"provider,omitempty"`
	Model    string        `json:"model,omitempty"`
	Effort   string        `json:"effort,omitempty"`
	Fast     bool          `json:"fast"`
	Mode     approval.Mode `json:"permission_mode,omitempty"`
}

// savedOf is what the sidecar keeps of the settings.
func savedOf(s Settings) Saved {
	return Saved{Provider: s.Provider, Model: s.Model, Effort: s.Effort, Fast: s.ServiceTier != "", Mode: s.Mode}
}

// ApplySidecar adds what the sidecar records to the summary: the source,
// the parent, and the saved settings, which replace the provider, model,
// and effort of the newest run. A session without saved settings (from
// before uah kept them) keeps its newest run's.
func (in *Info) ApplySidecar(sc Sidecar) {
	in.Source, in.Parent = sc.Source, sc.Parent
	if sc.Settings == nil {
		return
	}
	sv := sc.Settings
	in.Saved = true
	in.Provider, in.Model, in.Effort = cmp.Or(sv.Provider, in.Provider), cmp.Or(sv.Model, in.Model), cmp.Or(sv.Effort, in.Effort)
	fast := sv.Fast
	in.Fast, in.Mode = &fast, sv.Mode
}

// saveSettings records the settings in the session's sidecar, creating the
// sidecar when the session has none. It replaces the file whole, so a
// reader never sees half of it.
func saveSettings(sessionsDir, id string, s Settings) error {
	sc, found, err := ReadSidecar(sessionsDir, id)
	if err != nil {
		return err
	}
	saved := savedOf(s)
	if sc.Settings != nil && *sc.Settings == saved {
		return nil
	}
	if !found {
		sc.Created = time.Now().UTC()
	}
	sc.Settings = &saved
	data, err := json.Marshal(sc)
	if err != nil {
		return fmt.Errorf("failed to encode the session sidecar: %w", err)
	}
	if err := os.MkdirAll(sessionsDir, 0o700); err != nil {
		return fmt.Errorf("failed to create %s: %w", sessionsDir, err)
	}
	tmp, err := os.CreateTemp(sessionsDir, "."+id+".uah-*")
	if err != nil {
		return fmt.Errorf("failed to save the session settings: %w", err)
	}
	_, werr := tmp.Write(append(data, '\n'))
	if err := errors.Join(werr, tmp.Close()); err != nil {
		_ = os.Remove(tmp.Name())

		return fmt.Errorf("failed to save the session settings: %w", err)
	}
	if err := os.Rename(tmp.Name(), sidecarPath(sessionsDir, id)); err != nil {
		_ = os.Remove(tmp.Name())

		return fmt.Errorf("failed to save the session settings: %w", err)
	}

	return nil
}

// saveSettings keeps the session's settings in its sidecar, warning when
// it cannot. Sessions without a sessions directory keep nothing.
func (s *Session) saveSettings(settings Settings) {
	if s.sessionsDir == "" {
		return
	}
	if err := saveSettings(s.sessionsDir, s.id, settings); err != nil {
		s.emit(Notice{At: time.Now(), Level: LevelWarning, Message: err.Error()})
	}
}
