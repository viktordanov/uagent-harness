package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Where a session was started. Codex hides scripted sessions from its resume
// picker by default; uah does the same with SourceRun.
const (
	SourceTUI = "tui"
	SourceRun = "run"
)

// Sidecar is what uah knows about a session that the runner and uagent do not
// record: sessions/<id>.uah.json. The file is the source of truth; see
// docs/design/state.md.
type Sidecar struct {
	Source  string    `json:"source"`
	Created time.Time `json:"created"`
}

func sidecarPath(sessionsDir, id string) string {
	return filepath.Join(sessionsDir, id+".uah.json")
}

// ReadSidecar returns the session's sidecar, or found=false when it has none.
func ReadSidecar(sessionsDir, id string) (sc Sidecar, found bool, err error) {
	data, err := os.ReadFile(sidecarPath(sessionsDir, id))
	if errors.Is(err, fs.ErrNotExist) {
		return Sidecar{}, false, nil
	}
	if err != nil {
		return Sidecar{}, false, fmt.Errorf("failed to read the session sidecar: %w", err)
	}
	if err := json.Unmarshal(data, &sc); err != nil {
		return Sidecar{}, false, fmt.Errorf("failed to parse the session sidecar: %w", err)
	}

	return sc, true, nil
}

// writeSidecar creates the sidecar unless one exists: the first writer, the
// command that started the session, decides its source.
func writeSidecar(sessionsDir, id string, sc Sidecar) error {
	if err := os.MkdirAll(sessionsDir, 0o700); err != nil {
		return fmt.Errorf("failed to create %s: %w", sessionsDir, err)
	}
	data, err := json.Marshal(sc)
	if err != nil {
		return fmt.Errorf("failed to encode the session sidecar: %w", err)
	}
	f, err := os.OpenFile(sidecarPath(sessionsDir, id), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to create the session sidecar: %w", err)
	}
	_, werr := f.Write(append(data, '\n'))

	return errors.Join(werr, f.Close())
}

// Interactive drops sessions started by `uah run`, as Codex's picker drops
// `codex exec` sessions. Sessions with no sidecar (older ones, or uagent's)
// stay.
func Interactive(infos []Info) []Info {
	out := make([]Info, 0, len(infos))
	for _, in := range infos {
		if in.Source != SourceRun {
			out = append(out, in)
		}
	}

	return out
}
