package perf

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/viktordanov/uah/testing/fakellm"
)

// A fixture is a resumable session of a known size. Building one by
// running thousands of turns would take minutes (every model request
// carries the whole history), so the harness records two real turns once
// per process (a seed and one workload turn, see workload.go) on the real
// stack, and the fixture repeats the workload turn's records: the session
// file's items and operations, the operations' output files, and the run
// record, with fresh IDs, sequence numbers, times, and paths for each
// copy. What uah reads back is what it wrote.

// Size is a fixture size: the number of records (items and operation
// updates) in the session file.
type Size struct {
	Name    string
	Records int
}

// The sizes the scenarios run on.
var (
	Small  = Size{Name: "small", Records: 100}
	Medium = Size{Name: "medium", Records: 2_000}
	Large  = Size{Name: "large", Records: 10_000}
)

// Sizes are the fixture sizes by name.
var Sizes = []Size{Small, Medium, Large}

// copyGap is the time between the runs of a fixture.
const copyGap = 2 * time.Minute

// Template is the recorded seed and workload turn a fixture repeats.
type Template struct {
	root, sid string
	header    []byte
	// seed are the session file's records before the workload turn, and
	// unit the workload turn's; their first and last sequence numbers.
	seed, unit             [][]token
	unitFirst, unitLast    int
	seedLastTurn, unitTurn string
	seedRun, unitRun       runFiles
	// ops are the workload turn's operations' output files, by
	// operation ID and file name.
	ops     map[string]map[string][]byte
	sidecar map[string]any
}

// runFiles are a run record's files, by name.
type runFiles struct {
	id    string
	files map[string][][]token
}

// Record runs the seed and the workload turn in a new environment under
// root and keeps what they wrote.
func Record(ctx context.Context, root string) (*Template, error) {
	e, err := NewEnv(root) //nolint:contextcheck,nolintlint // on Linux, the sandbox probes bwrap once per process, with its own timeout; not on darwin
	if err != nil {
		return nil, err
	}
	defer e.Close()
	if err := fillWorkspace(e.Workspace); err != nil {
		return nil, err
	}
	s, _, err := e.Open(ctx, "", false)
	if err != nil {
		return nil, err
	}
	e.LLM.Script(fakellm.Reply{Text: "Ready. What should I look at?"})
	_, seedErr := Turn(s, "Look at the request handler and its tests.")
	e.LLM.Script(turnReplies("u1")...)
	if seedErr == nil {
		_, seedErr = Turn(s, "Check how the handler validates requests and note it down.")
	}
	id := s.ID()
	if err := errors.Join(seedErr, s.Close()); err != nil {
		return nil, fmt.Errorf("failed to record the workload: %w", err)
	}

	return readTemplate(root, e.Home, id)
}

// readTemplate reads the recorded session id from home.
func readTemplate(root, home, id string) (*Template, error) {
	t := &Template{root: root, sid: id, ops: map[string]map[string][]byte{}}
	runs, err := os.ReadDir(filepath.Join(home, "runs"))
	if err != nil || len(runs) != 2 {
		return nil, fmt.Errorf("want 2 recorded runs: %d, %w", len(runs), err)
	}
	if t.seedRun, err = readRun(filepath.Join(home, "runs", runs[0].Name()), root); err != nil {
		return nil, err
	}
	if t.unitRun, err = readRun(filepath.Join(home, "runs", runs[1].Name()), root); err != nil {
		return nil, err
	}
	events := t.unitRun.files["events.jsonl"]
	if len(events) == 0 {
		return nil, errors.New("the workload run recorded no events")
	}
	t.unitFirst, t.unitLast = firstSeq(events[0]), firstSeq(events[len(events)-1])
	data, err := os.ReadFile(filepath.Join(home, "sessions", id+".session.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("failed to read the recorded session: %w", err)
	}
	lines := bytes.Split(bytes.TrimSuffix(data, []byte("\n")), []byte("\n"))
	t.header = lines[0]
	inUnit := false
	for _, line := range lines[1:] {
		toks := tokenize(line, root, "")
		if !inUnit && bytes.Contains(line, []byte(`"Sequence":`+strconv.Itoa(t.unitFirst)+`,`)) {
			inUnit = true
		}
		if inUnit {
			t.unit = append(t.unit, toks)
		} else {
			t.seed = append(t.seed, toks)
		}
	}
	t.seedLastTurn, t.unitTurn = lastTurn(t.seed), lastTurn(t.unit)
	if t.seedLastTurn == "" || t.unitTurn == "" || len(t.unit) == 0 {
		return nil, errors.New("the recorded session has no turns")
	}
	opsDir := filepath.Join(home, "sessions", "operations", id)
	entries, err := os.ReadDir(opsDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read the recorded operations: %w", err)
	}
	for _, op := range entries {
		files := map[string][]byte{}
		for _, name := range []string{"out", "err"} {
			if b, err := os.ReadFile(filepath.Join(opsDir, op.Name(), name)); err == nil {
				files[name] = b
			}
		}
		t.ops[op.Name()] = files
	}
	sc, err := os.ReadFile(filepath.Join(home, "sessions", id+".uah.json"))
	if err != nil {
		return nil, fmt.Errorf("failed to read the recorded sidecar: %w", err)
	}
	if err := json.Unmarshal(sc, &t.sidecar); err != nil {
		return nil, fmt.Errorf("failed to decode the recorded sidecar: %w", err)
	}

	return t, nil
}

func readRun(dir, root string) (runFiles, error) {
	r := runFiles{id: filepath.Base(dir), files: map[string][][]token{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return r, fmt.Errorf("failed to read run %s: %w", dir, err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return r, fmt.Errorf("failed to read run file: %w", err)
		}
		var lines [][]token
		for line := range bytes.SplitSeq(bytes.TrimSuffix(data, []byte("\n")), []byte("\n")) {
			lines = append(lines, tokenize(line, root, r.id))
		}
		r.files[e.Name()] = lines
	}

	return r, nil
}

// Fixture is a built session.
type Fixture struct {
	Size      Size
	SessionID string
	Records   int
	Runs      int
	// Bytes is the session file's size.
	Bytes int64
	// Marker is text in the transcript's last answer.
	Marker string
}

// Build writes a session of about size.Records records into e.Home, and
// fills e.Workspace with the files the workload reads.
func (t *Template) Build(e *Env, size Size) (Fixture, error) {
	if err := fillWorkspace(e.Workspace); err != nil {
		return Fixture{}, err
	}
	copies := max(1, (size.Records-len(t.seed)+len(t.unit)/2)/len(t.unit))
	fx := Fixture{Size: size, SessionID: t.sid, Runs: 1 + copies, Marker: "test log shows every test passing"}
	b := builder{t: t, root: e.Root, copies: copies, now: time.Now()}
	sessions := filepath.Join(e.Home, "sessions")
	if err := os.MkdirAll(sessions, 0o700); err != nil {
		return fx, fmt.Errorf("failed to make the sessions directory: %w", err)
	}
	path := filepath.Join(sessions, t.sid+".session.jsonl")
	f, err := os.Create(path)
	if err != nil {
		return fx, fmt.Errorf("failed to create the session file: %w", err)
	}
	buf := append(append([]byte(nil), t.header...), '\n')
	for _, line := range t.seed {
		buf = b.appendLine(buf, line, 0)
		fx.Records++
	}
	_, err = f.Write(buf)
	for k := 1; k <= copies && err == nil; k++ {
		buf = buf[:0]
		for _, line := range t.unit {
			buf = b.appendLine(buf, line, k)
			fx.Records++
		}
		_, err = f.Write(buf)
	}
	if err := errors.Join(err, f.Close()); err != nil {
		return fx, fmt.Errorf("failed to write the session file: %w", err)
	}
	if info, err := os.Stat(path); err == nil {
		fx.Bytes = info.Size()
	}
	if err := b.writeRuns(e.Home); err != nil {
		return fx, err
	}
	if err := b.writeOps(filepath.Join(sessions, "operations", t.sid)); err != nil {
		return fx, err
	}

	return fx, b.writeSidecar(sessions)
}

// builder writes copy k of the template's records: copy 0 is the seed,
// copies 1 to copies the workload turn.
type builder struct {
	t      *Template
	root   string
	copies int
	// now is when the last copy ran.
	now time.Time
}

func (b builder) shift(k int) time.Duration { return time.Duration(k-b.copies) * copyGap }

func (b builder) runID(k int) string {
	started := b.now.Add(b.shift(k))

	return started.Local().Format("20060102-150405") + "-" + b.t.sid[:8]
}

// id is the fresh ID of a recorded UUID in copy k.
func (b builder) id(old string, k int) string {
	switch {
	case k == 0 || old == b.t.sid:
		return old
	case old == b.t.seedLastTurn:
		// The workload turn's first turn follows the previous copy's last.
		if k == 1 {
			return old
		}

		return b.id(b.t.unitTurn, k-1)
	}

	return old[:19] + fmt.Sprintf("%04x", k) + old[23:]
}

// appendLine appends copy k of a recorded line, and a newline, to buf.
func (b builder) appendLine(buf []byte, line []token, k int) []byte {
	for _, tok := range line {
		switch tok.kind {
		case tokText:
			buf = append(buf, tok.text...)
		case tokUUID:
			buf = append(buf, b.id(tok.text, k)...)
		case tokModelID:
			if k == 0 {
				buf = append(buf, tok.text...)
			} else {
				prefix, rest, _ := strings.Cut(tok.text, "-")
				buf = append(buf, prefix+"-u"+strconv.Itoa(k)+"-"+rest...)
			}
		case tokSeq:
			n := tok.n
			if k > 0 {
				n += (k - 1) * (b.t.unitLast - b.t.unitFirst + 1)
			}
			buf = strconv.AppendInt(append(buf, `"Sequence":`...), int64(n), 10)
		case tokTime:
			buf = tok.at.Add(b.shift(k)).AppendFormat(buf, time.RFC3339Nano)
		case tokRoot:
			buf = append(buf, b.root...)
		case tokRun:
			buf = append(buf, b.runID(k)...)
		}
	}

	return append(buf, '\n')
}

func (b builder) writeRuns(home string) error {
	for k := 0; k <= b.copies; k++ {
		run := b.t.unitRun
		if k == 0 {
			run = b.t.seedRun
		}
		dir := filepath.Join(home, "runs", b.runID(k))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("failed to make a run record: %w", err)
		}
		for name, lines := range run.files {
			var buf []byte
			for _, line := range lines {
				buf = b.appendLine(buf, line, k)
			}
			if err := os.WriteFile(filepath.Join(dir, name), buf, 0o600); err != nil {
				return fmt.Errorf("failed to write a run record: %w", err)
			}
		}
	}

	return nil
}

func (b builder) writeOps(dir string) error {
	for k := 1; k <= b.copies; k++ {
		for op, files := range b.t.ops {
			opDir := filepath.Join(dir, b.id(op, k))
			if err := os.MkdirAll(opDir, 0o700); err != nil {
				return fmt.Errorf("failed to make an operation directory: %w", err)
			}
			for name, data := range files {
				if err := os.WriteFile(filepath.Join(opDir, name), data, 0o600); err != nil {
					return fmt.Errorf("failed to write an operation's output: %w", err)
				}
			}
		}
	}

	return nil
}

func (b builder) writeSidecar(dir string) error {
	sc := maps.Clone(b.t.sidecar)
	sc["workspace"] = strings.Replace(fmt.Sprint(sc["workspace"]), b.t.root, b.root, 1)
	sc["last_sequence"] = b.t.unitLast + (b.copies-1)*(b.t.unitLast-b.t.unitFirst+1)
	sc["last_activity"] = b.now.UTC().Format(time.RFC3339Nano)
	data, err := json.Marshal(sc)
	if err != nil {
		return fmt.Errorf("failed to encode the sidecar: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, b.t.sid+".uah.json"), data, 0o600); err != nil {
		return fmt.Errorf("failed to write the sidecar: %w", err)
	}

	return nil
}

// A token is a piece of a recorded line: text kept as is, or a value each
// copy changes.
type token struct {
	kind tokenKind
	text string
	n    int
	at   time.Time
}

type tokenKind int

const (
	tokText tokenKind = iota
	tokUUID
	tokModelID // fakellm's resp-1, call-1-0, ...
	tokSeq
	tokTime
	tokRoot // the recording's root directory
	tokRun  // the run's ID
)

var tokenPattern = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}` +
	`|"Sequence":\d+` +
	`|\b(?:resp|call|fc|msg|rs)-\d+(?:-\d+)?\b` +
	`|\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(?:\.\d+)?(?:Z|[+-]\d\d:\d\d)`)

// tokenize splits a recorded line; root and runID (when set) become
// tokens too.
func tokenize(line []byte, root, runID string) []token {
	var toks []token
	for _, part := range splitLiteral(string(line), root, tokRoot) {
		if part.kind != tokText {
			toks = append(toks, part)

			continue
		}
		for _, sub := range splitLiteral(part.text, runID, tokRun) {
			if sub.kind != tokText {
				toks = append(toks, sub)

				continue
			}
			toks = append(toks, splitPattern(sub.text)...)
		}
	}

	return toks
}

func splitLiteral(s, lit string, kind tokenKind) []token {
	if lit == "" {
		return []token{{text: s}}
	}
	var toks []token
	for {
		before, after, found := strings.Cut(s, lit)
		if before != "" {
			toks = append(toks, token{text: before})
		}
		if !found {
			return toks
		}
		toks = append(toks, token{kind: kind})
		s = after
	}
}

func splitPattern(s string) []token {
	var toks []token
	last := 0
	for _, m := range tokenPattern.FindAllStringIndex(s, -1) {
		if m[0] > last {
			toks = append(toks, token{text: s[last:m[0]]})
		}
		toks = append(toks, matchToken(s[m[0]:m[1]]))
		last = m[1]
	}
	if last < len(s) {
		toks = append(toks, token{text: s[last:]})
	}

	return toks
}

func matchToken(m string) token {
	switch {
	case strings.HasPrefix(m, `"Sequence":`):
		n, _ := strconv.Atoi(strings.TrimPrefix(m, `"Sequence":`))

		return token{kind: tokSeq, n: n}
	case len(m) == 36 && m[8] == '-' && m[13] == '-':
		return token{kind: tokUUID, text: m}
	case m[0] >= '0' && m[0] <= '9':
		if at, err := time.Parse(time.RFC3339Nano, m); err == nil {
			return token{kind: tokTime, at: at}
		}
	default:
		return token{kind: tokModelID, text: m}
	}

	return token{text: m}
}

// firstSeq is the first sequence number in a line, or 0.
func firstSeq(line []token) int {
	for _, tok := range line {
		if tok.kind == tokSeq {
			return tok.n
		}
	}

	return 0
}

// lastTurn is the ID of the last turn item in lines.
func lastTurn(lines [][]token) string {
	id := ""
	for _, line := range lines {
		for i, tok := range line {
			if tok.kind == tokText && strings.HasSuffix(tok.text, `"Kind":"turn","Data":{"ID":"`) && i+1 < len(line) {
				id = line[i+1].text
			}
		}
	}

	return id
}
