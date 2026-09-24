package embedded

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
)

// killOrphans kills the process groups of the session's operations that are
// still recorded as running. They outlived a uah that stopped without
// cleaning up (a crash, or SIGKILL): uagent kills a run's live groups when
// the run ends, but a crash skips that, and on resume the runner's session
// store marks such operations failed ("interrupted before an exit status was
// recorded") without stopping their processes, so nothing would kill them.
// The harness holds the session lock, so no other run owns these groups.
func killOrphans(sessionsDir, id string) {
	for _, g := range liveGroups(filepath.Join(sessionsDir, id+".session.jsonl")) {
		_ = syscall.Kill(-g, syscall.SIGKILL) // ESRCH: the group is already gone
	}
}

// liveGroups reads the latest record of each operation in a session file and
// returns the process groups of those not in a terminal status.
func liveGroups(path string) []int {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	type state struct {
		status string
		pgid   int
	}
	latest := map[string]state{}
	br := bufio.NewReaderSize(f, 1<<20)
	for {
		line, err := br.ReadBytes('\n')
		var rec struct {
			Type string `json:"type"`
			Data struct {
				Operation struct {
					ID     string
					Status string
					State  struct{ ProcessGroupID int }
				}
			} `json:"data"`
		}
		if json.Unmarshal(line, &rec) == nil && rec.Type == "operation" {
			op := rec.Data.Operation
			s := latest[op.ID]
			s.status = op.Status
			if op.State.ProcessGroupID != 0 {
				s.pgid = op.State.ProcessGroupID
			}
			latest[op.ID] = s
		}
		if err != nil { // io.EOF after the last line
			break
		}
	}
	var groups []int
	for _, s := range latest {
		switch s.status {
		case "completed", "failed", "canceled":
		default:
			if s.pgid > 1 {
				groups = append(groups, s.pgid)
			}
		}
	}

	return groups
}
