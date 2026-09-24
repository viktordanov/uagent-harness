package embedded

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/unreallabsai/unreal-agent/harness/session"
	"github.com/unreallabsai/unreal-agent/harness/sessionstore"

	"github.com/viktordanov/uagent-harness/internal/compaction"
)

// compactionLog is a session's compactions, one JSON line each, in
// sessions/<id>.compaction.jsonl next to the runner's session file. The last
// line applies.
type compactionLog struct{ path string }

func newCompactionLog(sessionsDir string, id session.ID) compactionLog {
	return compactionLog{path: filepath.Join(sessionsDir, string(id)+".compaction.jsonl")}
}

// last returns the latest compaction, if there is one.
func (l compactionLog) last() (*compaction.Record, error) {
	f, err := os.Open(l.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil //nolint:nilnil // no compaction yet
	}
	if err != nil {
		return nil, fmt.Errorf("failed to open the compaction log: %w", err)
	}
	defer f.Close()
	var last *compaction.Record
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	for scanner.Scan() {
		var rec compaction.Record
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			return nil, fmt.Errorf("failed to read the compaction log: %w", err)
		}
		last = &rec
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read the compaction log: %w", err)
	}

	return last, nil
}

func (l compactionLog) append(rec compaction.Record) error {
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("failed to encode the compaction: %w", err)
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return fmt.Errorf("failed to open the compaction log: %w", err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		_ = f.Close()

		return fmt.Errorf("failed to write the compaction log: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to write the compaction log: %w", err)
	}

	return nil
}

// lastUsage is the context the session's last model response used (input
// plus output tokens), so automatic compaction also works on a resumed run.
func lastUsage(ctx context.Context, store sessionstore.Store, id session.ID) (int64, error) {
	var used int64
	after := sessionstore.BeforeFirst
	for {
		page, err := store.Items(ctx, id, after, 512)
		if err != nil {
			return 0, fmt.Errorf("failed to read the session: %w", err)
		}
		for _, item := range page.Items {
			if r, ok := item.Data.(sessionstore.ModelResponse); ok {
				used = r.Response.Usage.InputTokens + r.Response.Usage.OutputTokens
			}
		}
		if !page.More {
			return used, nil
		}
		after = page.NextAfter
	}
}
