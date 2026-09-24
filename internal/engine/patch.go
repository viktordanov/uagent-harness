package engine

import (
	"bytes"
	"encoding/json"
	"time"

	"github.com/viktordanov/uagent-harness/internal/patch"
)

// PatchPlanType is the remote job plan an apply_patch call runs as. Its
// completed job keeps the diff in the job's handle, which the session file
// stores untruncated.
const PatchPlanType = "uah.apply_patch"

// PatchApplied is the diff of an applied apply_patch call, computed from
// the files when it was applied. The embedded engine adds it to the run's
// stream when the session stores the call's result, and session.Load adds
// it to a loaded transcript, from the same stored result.
type PatchApplied struct {
	At     time.Time
	CallID string
	Files  []patch.FileDiff
}

func (e PatchApplied) OccurredAt() time.Time { return e.At }

// PatchHandle is the handle of a completed apply_patch job.
type PatchHandle struct {
	Files []patch.FileDiff `json:"files"`
}

// PatchFromItem reads a session item, one line of the session file as the
// runner writes it, and returns the diff when the item completes an
// apply_patch call.
func PatchFromItem(line []byte) (PatchApplied, bool) {
	if !bytes.Contains(line, []byte(PatchPlanType)) {
		return PatchApplied{}, false
	}
	var item struct {
		RecordedAt time.Time
		Kind       string
		Data       struct {
			CallID     string
			Operations []struct {
				Type, Status string
				State        struct {
					Plan   struct{ Type string }
					Handle json.RawMessage
				}
			}
		}
	}
	if json.Unmarshal(line, &item) != nil || item.Kind != "tool_call_status" {
		return PatchApplied{}, false
	}
	for _, op := range item.Data.Operations {
		if op.Type != "remote_job" || op.Status != "completed" || op.State.Plan.Type != PatchPlanType || len(op.State.Handle) == 0 {
			continue
		}
		var h PatchHandle
		if json.Unmarshal(op.State.Handle, &h) == nil {
			return PatchApplied{At: item.RecordedAt, CallID: item.Data.CallID, Files: h.Files}, true
		}
	}

	return PatchApplied{}, false
}
