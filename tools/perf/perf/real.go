package perf

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/viktordanov/uah/internal/session"
)

// Real sessions are copied, never opened in place: the copy reads the
// session file, its sidecar and operation outputs, and its run records
// from the uah home, rewrites the home's path to the scratch home, and
// writes nothing back. Nothing else in the home is read (no
// configuration, credentials, or index). A session with an operation that
// never finished is skipped: resuming it would carry the operation on, and
// its recorded paths may be outside the copy.

// realTargets are the largest n sessions of home.
func realTargets(home string, n int) ([]target, error) {
	home, err := filepath.Abs(home)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve %s: %w", home, err)
	}
	files, err := filepath.Glob(filepath.Join(home, "sessions", "*.session.jsonl"))
	if err != nil || len(files) == 0 {
		return nil, fmt.Errorf("no sessions in %s", home)
	}
	type sized struct {
		id   string
		size int64
	}
	var all []sized
	for _, f := range files {
		if info, err := os.Stat(f); err == nil {
			all = append(all, sized{id: strings.TrimSuffix(filepath.Base(f), ".session.jsonl"), size: info.Size()})
		}
	}
	slices.SortFunc(all, func(a, b sized) int { return cmp.Compare(b.size, a.size) })
	targets := make([]target, 0, n)
	for _, s := range all {
		if len(targets) == n {
			break
		}
		data, err := os.ReadFile(filepath.Join(home, "sessions", s.id+".session.jsonl"))
		if err != nil || unfinishedOperations(data) {
			continue
		}
		targets = append(targets, target{
			name: fmt.Sprintf("real-%d", len(targets)+1), real: true,
			build: func(e *Env) (Fixture, error) { return copyReal(home, s.id, e) },
		})
	}
	if len(targets) == 0 {
		return nil, fmt.Errorf("no session of %s can be copied safely", home)
	}

	return targets, nil
}

var operationPattern = regexp.MustCompile(`"ID":"([0-9a-f-]{36})","Type":"[^"]*","Version":\d+,"Status":"([a-z]+)"`)

// unfinishedOperations reports whether a session file has an operation
// whose last recorded status is not final.
func unfinishedOperations(data []byte) bool {
	last := map[string]string{}
	for _, m := range operationPattern.FindAllSubmatch(data, -1) {
		last[string(m[1])] = string(m[2])
	}
	for _, status := range last {
		if status != "completed" && status != "failed" && status != "canceled" {
			return true
		}
	}

	return false
}

// copyReal copies session id of home into e's home.
func copyReal(home, id string, e *Env) (Fixture, error) {
	fx := Fixture{Size: Size{Name: "real"}, SessionID: id}
	if err := fillWorkspace(e.Workspace); err != nil {
		return fx, err
	}
	if err := stubSkill(e.Workspace); err != nil {
		return fx, err
	}
	rewrite := func(data []byte) []byte { return bytes.ReplaceAll(data, []byte(home), []byte(e.Home)) }
	sessions := filepath.Join(home, "sessions")
	for _, name := range []string{id + ".session.jsonl", id + ".uah.json"} {
		if err := copyFile(filepath.Join(sessions, name), filepath.Join(e.Home, "sessions", name), rewrite); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fx, err
		}
	}
	ops := filepath.Join(sessions, "operations", id)
	err := filepath.WalkDir(ops, func(path string, d fs.DirEntry, err error) error {
		if err == nil && path == session.TempDir(sessions, id) {
			return filepath.SkipDir // the commands' $TMPDIR, not an operation
		}
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(sessions, path)

		return copyFile(path, filepath.Join(e.Home, "sessions", rel), nil)
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fx, fmt.Errorf("failed to copy the operations: %w", err)
	}
	runs, err := os.ReadDir(filepath.Join(home, "runs"))
	if err != nil {
		return fx, fmt.Errorf("failed to list the runs: %w", err)
	}
	for _, r := range runs {
		dir := filepath.Join(home, "runs", r.Name())
		if !r.IsDir() || runSession(dir) != id {
			continue
		}
		fx.Runs++
		files, _ := os.ReadDir(dir)
		for _, f := range files {
			if err := copyFile(filepath.Join(dir, f.Name()), filepath.Join(e.Home, "runs", r.Name(), f.Name()), rewrite); err != nil {
				return fx, err
			}
		}
	}
	file := sessionFile(e, id)
	fx.Records = lineCount(file) - 1
	if info, err := os.Stat(file); err == nil {
		fx.Bytes = info.Size()
	}

	return fx, nil
}

// runSession is the session of a run record ("" when unreadable).
func runSession(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "summary.json"))
	if err != nil {
		return ""
	}
	var s struct {
		SessionID string `json:"session_id"`
	}
	if json.Unmarshal(data, &s) != nil {
		return ""
	}

	return s.SessionID
}

func copyFile(from, to string, rewrite func([]byte) []byte) error {
	data, err := os.ReadFile(from)
	if err != nil {
		return fmt.Errorf("failed to read %s: %w", from, err)
	}
	if rewrite != nil {
		data = rewrite(data)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o700); err != nil {
		return fmt.Errorf("failed to make %s: %w", filepath.Dir(to), err)
	}
	if err := os.WriteFile(to, data, 0o600); err != nil {
		return fmt.Errorf("failed to write %s: %w", to, err)
	}

	return nil
}

// stubSkill puts one skill in the workspace, so a recorded SkillUse call
// finds its tool when the session is restored (as evalrun does).
func stubSkill(workspace string) error {
	dir := filepath.Join(workspace, ".agents", "skills", "perf-stub")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to make the stub skill: %w", err)
	}
	body := "---\nname: perf-stub\ndescription: Keeps SkillUse available while a copied session is resumed.\n---\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o600); err != nil {
		return fmt.Errorf("failed to write the stub skill: %w", err)
	}

	return nil
}
