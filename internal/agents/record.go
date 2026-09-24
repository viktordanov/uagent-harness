package agents

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// record is what resume_agent needs of a child that the session's sidecar
// does not keep: sessions/<id>.agent.json.
type record struct {
	Nickname string `json:"nickname"`
	Role     string `json:"role,omitempty"`
	// Model and Effort are the spawn call's overrides.
	Model  string `json:"model,omitempty"`
	Effort string `json:"effort,omitempty"`
}

func recordPath(sessionsDir, id string) string {
	return filepath.Join(sessionsDir, id+".agent.json")
}

func writeRecord(sessionsDir, id string, r record) error {
	data, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("failed to encode the agent record: %w", err)
	}
	if err := os.WriteFile(recordPath(sessionsDir, id), append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("failed to write the agent record: %w", err)
	}

	return nil
}

// readRecord returns the child's record; a missing or broken one is empty,
// and the child resumes as a default agent with a new nickname.
func readRecord(sessionsDir, id string) (record, error) {
	var r record
	data, err := os.ReadFile(recordPath(sessionsDir, id))
	if err != nil {
		return r, fmt.Errorf("failed to read the agent record: %w", err)
	}
	if err := json.Unmarshal(data, &r); err != nil {
		return record{}, fmt.Errorf("failed to parse the agent record: %w", err)
	}

	return r, nil
}
