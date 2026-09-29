// Package evalrun runs the compaction evaluation (internal/compaction/eval)
// on recorded sessions. The runner's session file is append-only, so any
// prefix of it is a valid session: evalrun copies a session up to an item
// into a scratch home, opens it on the embedded engine with a provider that
// captures the next model request, and applies each strategy to that
// request. Summaries come from a cache, the session's own compaction log,
// or a stub, so a run never calls a model unless asked to.
package evalrun

import (
	"bufio"
	"bytes"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/viktordanov/uagent-harness/internal/compaction"
	"github.com/viktordanov/uagent-harness/internal/sessionfile"
)

// Point is where a case cuts a session: after the item Seq.
type Point struct {
	Seq uint64
	At  time.Time
	// Label says why: "recorded" for a compaction the session made, or the
	// input size the next request reached, such as "≥100k".
	Label string
	// Focus is the recorded compaction's /compact focus.
	Focus string
}

// Thresholds are the input sizes at which Points cuts a session: before
// the first request that reached each.
var Thresholds = []int64{50_000, 100_000, 150_000, 200_000}

// Points are the cuts of a session: its recorded compactions, and the
// requests that first reached each threshold.
func Points(sessionsDir, id string) ([]Point, error) {
	_, page, err := sessionfile.Read(filepath.Join(sessionsDir, id+".session.jsonl"), 0, 0)
	if err != nil {
		return nil, fmt.Errorf("failed to read session %s: %w", id, err)
	}
	items := page.Items
	records, _, err := compaction.OpenLog(sessionsDir, id).Records()
	if err != nil {
		return nil, err
	}
	var out []Point
	for _, rec := range records {
		if rec.Trigger == compaction.TriggerClear || rec.Summary == "" || rec.Covered == 0 {
			continue
		}
		if seq, at, ok := lastBefore(items, rec.At); ok {
			out = append(out, Point{Seq: seq, At: at, Label: "recorded", Focus: rec.Focus})
		}
	}

	return append(out, thresholdPoints(items)...), nil
}

// lastBefore is the last item recorded at or before t.
func lastBefore(items []sessionfile.Item, t time.Time) (uint64, time.Time, bool) {
	var seq uint64
	var at time.Time
	for _, it := range items {
		if it.RecordedAt.After(t) {
			break
		}
		seq, at = it.Sequence, it.RecordedAt
	}

	return seq, at, seq > 0
}

// thresholdPoints cut before the turn of the first response whose input
// reached each threshold.
func thresholdPoints(items []sessionfile.Item) []Point {
	var out []Point
	next := 0
	turnAt := map[string]int{} // turn ID -> index of its turn item
	for i, it := range items {
		switch it.Kind {
		case sessionfile.KindTurn:
			var t sessionfile.Turn
			if it.Decode(&t) == nil {
				turnAt[t.ID] = i
			}
		case sessionfile.KindModelResponse:
			var r sessionfile.ModelResponse
			if it.Decode(&r) != nil || next == len(Thresholds) || r.Response.Usage.InputTokens < Thresholds[next] {
				continue
			}
			ti, ok := turnAt[r.TurnID]
			for next < len(Thresholds) && r.Response.Usage.InputTokens >= Thresholds[next] {
				if ok && ti > 0 {
					out = append(out, Point{Seq: items[ti-1].Sequence, At: items[ti-1].RecordedAt, Label: fmt.Sprintf("≥%dk", Thresholds[next]/1000)})
				}
				next++
			}
		}
	}

	return out
}

// processGroup is an operation's process group in the session file. A cut
// copy zeroes it: uagent kills the groups a session file records as live
// when a run starts (harness.liveOperationGroups), and a copy must never
// signal a process.
var processGroup = regexp.MustCompile(`"ProcessGroupID":\s*\d+`)

// Cut writes the session file at src up to and including the item seq to
// dst, with every process group zeroed.
func Cut(src, dst string, seq uint64) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open the session file: %w", err)
	}
	defer in.Close()
	var out bytes.Buffer
	r := bufio.NewReaderSize(in, 1<<20)
	for {
		line, rerr := r.ReadBytes('\n')
		if len(line) > 0 && line[len(line)-1] == '\n' {
			if past(line, seq) {
				break
			}
			out.Write(processGroup.ReplaceAll(line, []byte(`"ProcessGroupID":0`)))
		}
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			return fmt.Errorf("failed to read the session file: %w", rerr)
		}
	}
	if err := os.WriteFile(dst, out.Bytes(), 0o600); err != nil {
		return fmt.Errorf("failed to write the cut session: %w", err)
	}

	return nil
}

// past reports whether the line is an item after seq.
func past(line []byte, seq uint64) bool {
	if !bytes.HasPrefix(line, []byte(`{"type":"item"`)) {
		return false
	}
	var rec struct {
		Data struct {
			Item struct{ Sequence uint64 }
		} `json:"data"`
	}

	return json.Unmarshal(line, &rec) == nil && rec.Data.Item.Sequence > seq
}

// Sessions are the IDs of the session files in dir, or the one file named.
func Sessions(target string) (dir string, ids []string, err error) {
	info, err := os.Stat(target)
	if err != nil {
		return "", nil, fmt.Errorf("failed to read %s: %w", target, err)
	}
	if !info.IsDir() {
		id, ok := strings.CutSuffix(filepath.Base(target), ".session.jsonl")
		if !ok {
			return "", nil, fmt.Errorf("%s is not a session file (<id>.session.jsonl)", target)
		}

		return filepath.Dir(target), []string{id}, nil
	}
	entries, err := os.ReadDir(target)
	if err != nil {
		return "", nil, fmt.Errorf("failed to list %s: %w", target, err)
	}
	for _, e := range entries {
		if id, ok := strings.CutSuffix(e.Name(), ".session.jsonl"); ok {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)

	return target, ids, nil
}
