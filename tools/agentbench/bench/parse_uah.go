package bench

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/viktordanov/uah/internal/sessionfile"
)

// uahEvent is the part of a `uah exec --json` event the parser reads.
type uahEvent struct {
	Type       string    `json:"type"`
	At         time.Time `json:"at"`
	ID         string    `json:"id"`
	CallID     string    `json:"call_id"`
	Name       string    `json:"name"`
	Arguments  string    `json:"arguments"`
	OK         bool      `json:"ok"`
	Detail     string    `json:"detail"`
	DurationMS int64     `json:"duration_ms"`
	Text       string    `json:"text"`
	Final      bool      `json:"final"`
	Message    string    `json:"message"`
	Stop       string    `json:"stop"`
	Mode       string    `json:"mode"`
	Effort     string    `json:"effort"`
	Trigger    string    `json:"trigger"`
	Tokens     int64     `json:"tokens"`
	Error      string    `json:"error"`
	Usage      struct {
		Input     int64 `json:"input"`
		Cached    int64 `json:"cached_input"`
		Output    int64 `json:"output"`
		Reasoning int64 `json:"reasoning"`
	} `json:"usage"`
}

// ParseUAH builds a timeline from a `uah exec --json` stream (one event per
// line, each optionally prefixed by the time the harness read it and a tab),
// the subagents' session files under stateDir, and the first-byte times in
// the runs' stderr.log diagnostics. stateDir may be empty.
func ParseUAH(stream io.Reader, start time.Time, stateDir string) (*Timeline, error) {
	p := &uahParser{tl: &Timeline{Harness: HarnessUAH, Start: start}, calls: map[string]int{}, pendingFirst: -1}
	tl := p.tl
	err := eachLine(stream, func(_ time.Time, line []byte) error {
		var e uahEvent
		if err := json.Unmarshal(line, &e); err != nil {
			return fmt.Errorf("bad uah event: %w", err)
		}
		if e.At.After(tl.End) {
			tl.End = e.At
		}
		if !p.model(e) && !p.compaction(e) {
			p.tool(e)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}
	// A request that never answered (the run was stopped) ends with the run.
	tl.Requests = closeRequests(tl.Requests, ms(start, tl.End))
	closeCalls(tl.Calls, ms(start, tl.End))
	for i := range tl.Compactions {
		if tl.Compactions[i].EndMS < 0 {
			tl.Compactions[i].EndMS = ms(start, tl.End)
		}
	}
	if stateDir != "" {
		if err := addSubagents(tl, stateDir, p.session); err != nil {
			return nil, err
		}
		addFirstBytes(tl, stateDir)
	}

	return tl, nil
}

type uahParser struct {
	tl      *Timeline
	calls   map[string]int
	session string
	// pendingFirst is the index the open request will take while its first
	// byte is not seen yet, else -1.
	pendingFirst int
	turnStart    time.Time
	effort       string // the effort of the next request
}

// model reads the session's and the model's events, reporting whether e
// was one.
func (p *uahParser) model(e uahEvent) bool {
	tl, start := p.tl, p.tl.Start
	switch e.Type {
	case "session_opened":
		p.session = e.ID
	case "run_started":
		p.effort = e.Effort
	case "control_input":
		if e.Mode == "settings" && e.Effort != "" {
			p.effort = e.Effort
		}
	case "user_message":
		// Adaptive effort's primed first turn is a message of uah's, not the
		// user's: its requests go with the prompt that follows it.
		if !strings.HasPrefix(e.Text, "<workspace_context>") {
			tl.Turns++
		}
	case "turn_started":
		p.turnStart = e.At
		p.pendingFirst = len(tl.Requests)
	case "text_delta", "reasoning_delta", "reasoning_summary_delta":
		if p.pendingFirst == len(tl.Requests) && !p.turnStart.IsZero() {
			tl.Requests = append(tl.Requests, Request{Turn: tl.Turns, StartMS: ms(start, p.turnStart), FirstMS: ms(start, e.At)})
			p.pendingFirst = -1
		}
	case "model_responded":
		end := e.At
		begin := end.Add(-time.Duration(e.DurationMS) * time.Millisecond)
		tok := Tokens{Input: e.Usage.Input, Cached: e.Usage.Cached, Output: e.Usage.Output, Reasoning: e.Usage.Reasoning}
		if n := len(tl.Requests); p.pendingFirst != -1 || n == 0 || tl.Requests[n-1].EndMS != 0 {
			tl.Requests = append(tl.Requests, Request{})
		}
		r := &tl.Requests[len(tl.Requests)-1]
		r.StartMS, r.EndMS, r.Tokens, r.Stop, r.Effort, r.Turn = ms(start, begin), ms(start, end), tok, e.Stop, p.effort, tl.Turns
		p.pendingFirst = -1
		tl.Tokens = tl.Tokens.Add(tok)
	case "assistant_message":
		if e.Final {
			tl.Answer = e.Text
		}
		if n := len(tl.Requests); n > 0 {
			tl.Requests[n-1].TextBytes += len(e.Text)
		}
	case eventError, "run_failed":
		tl.Errors = append(tl.Errors, cmpOr(e.Message, e.Text, e.Detail))
	default:
		return false
	}

	return true
}

// compaction reads the compactions' events, reporting whether e was one.
func (p *uahParser) compaction(e uahEvent) bool {
	tl := p.tl
	switch e.Type {
	case "compaction_started":
		tl.Compactions = append(tl.Compactions, Compaction{StartMS: ms(tl.Start, e.At), EndMS: -1, Trigger: e.Trigger, Tokens: e.Tokens})
	case "compacted":
		if n := len(tl.Compactions); n > 0 && tl.Compactions[n-1].EndMS < 0 {
			tl.Compactions[n-1].EndMS, tl.Compactions[n-1].Error = ms(tl.Start, e.At), e.Error
		}
	default:
		return false
	}

	return true
}

// tool reads the tool calls' events.
func (p *uahParser) tool(e uahEvent) {
	tl, start := p.tl, p.tl.Start
	switch e.Type {
	case "tool_called":
		p.calls[e.CallID] = len(tl.Calls)
		at := ms(start, e.At)
		c := newCall(e.CallID, e.Name, e.Arguments, at, len(tl.Requests)-1)
		c.EndMS = -1
		tl.Calls = append(tl.Calls, c)
		if c.Request >= 0 {
			tl.Requests[c.Request].ToolCalls++
		}
	case "tool_started":
		if i, ok := p.calls[e.CallID]; ok {
			tl.Calls[i].StartMS = ms(start, e.At)
		}
	case "tool_finished":
		if i, ok := p.calls[e.CallID]; ok {
			tl.Calls[i].EndMS, tl.Calls[i].OK, tl.Calls[i].Detail = ms(start, e.At), e.OK, e.Detail
		}
	}
}

// newCall is a call issued at at by request req, started at once.
func newCall(id, name, args string, at int64, req int) Call {
	return Call{
		ID: id, Name: name, Kind: callKind(name), Args: summarize(args), IssuedMS: at, StartMS: at, EndMS: at,
		Request: req, ArgsBytes: len(args), Escalated: strings.Contains(args, `"require_escalated"`),
	}
}

// eventError is the error event's type in both harnesses' streams.
const eventError = "error"

func cmpOr(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}

	return "error"
}

func closeRequests(rs []Request, end int64) []Request {
	for i := range rs {
		if rs[i].EndMS == 0 {
			rs[i].EndMS = end
		}
	}

	return rs
}

func closeCalls(cs []Call, end int64) {
	for i := range cs {
		if cs[i].EndMS < 0 {
			cs[i].EndMS = end
		}
	}
}

// eachLine calls fn with each non-empty line of r, split from the time the
// harness stamped on it (zero when unstamped).
func eachLine(r io.Reader, fn func(at time.Time, line []byte) error) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1<<16), 64<<20)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var at time.Time
		if stamp, rest, ok := bytes.Cut(line, []byte("\t")); ok && len(stamp) > 0 && stamp[0] != '{' {
			t, err := time.Parse(time.RFC3339Nano, string(stamp))
			if err != nil {
				return fmt.Errorf("bad stamp %q: %w", stamp, err)
			}
			at, line = t, rest
		}
		if len(line) == 0 || line[0] != '{' {
			continue // a non-JSON line (a warning printed to stdout)
		}
		if err := fn(at, line); err != nil {
			return err
		}
	}

	return sc.Err()
}

// The session file records the parser reads for subagents.
type (
	sfResponse struct {
		TurnID   string
		Response struct {
			Stop   string
			Output []struct {
				Type string
				Data json.RawMessage
			}
			Usage struct {
				InputTokens, CachedInputTokens, OutputTokens, ReasoningTokens int64
			}
		}
	}
	sfToolCall struct {
		CallID, Name, Arguments string
	}
	sfStatus struct {
		CallID string
		Status struct {
			Error      string
			WaitingFor []string
		}
	}
)

// addSubagents adds each subagent session's requests, calls, and tokens.
// The runner records a turn when its request starts and the response when
// it ends; a tool call starts when the response that issued it ends and
// finishes at its last status.
func addSubagents(tl *Timeline, stateDir, mainSession string) error {
	files, err := filepath.Glob(filepath.Join(stateDir, "sessions", "*.session.jsonl"))
	if err != nil {
		return err
	}
	for _, f := range files {
		id := strings.TrimSuffix(filepath.Base(f), ".session.jsonl")
		if id == mainSession {
			continue
		}
		_, page, err := sessionfile.Read(f, sessionfile.BeforeFirst, 0)
		if err != nil {
			return fmt.Errorf("subagent %s: %w", id, err)
		}
		addSession(tl, id, page.Items)
	}

	return nil
}

func addSession(tl *Timeline, agent string, items []sessionfile.Item) {
	var turnAt time.Time
	calls := map[string]int{}
	for _, it := range items {
		at := ms(tl.Start, it.RecordedAt)
		switch it.Kind {
		case sessionfile.KindTurn:
			turnAt = it.RecordedAt
		case sessionfile.KindModelResponse:
			var r sfResponse
			if it.Decode(&r) != nil {
				continue
			}
			u := r.Response.Usage
			tok := Tokens{Input: u.InputTokens, Cached: u.CachedInputTokens, Output: u.OutputTokens, Reasoning: u.ReasoningTokens}
			req := Request{Agent: agent, StartMS: ms(tl.Start, turnAt), EndMS: at, Tokens: tok, Stop: r.Response.Stop}
			for _, o := range r.Response.Output {
				var c sfToolCall
				var m struct{ Text string }
				switch {
				case o.Type == "message" && json.Unmarshal(o.Data, &m) == nil:
					req.TextBytes += len(m.Text)
				case o.Type == "tool_call" && json.Unmarshal(o.Data, &c) == nil:
					req.ToolCalls++
					calls[c.CallID] = len(tl.Calls)
					call := newCall(c.CallID, c.Name, c.Arguments, at, len(tl.Requests))
					call.Agent, call.OK = agent, true
					tl.Calls = append(tl.Calls, call)
				}
			}
			tl.Requests = append(tl.Requests, req)
			tl.Tokens = tl.Tokens.Add(tok)
		case sessionfile.KindToolCallStatus:
			var s sfStatus
			if it.Decode(&s) != nil {
				continue
			}
			if i, ok := calls[s.CallID]; ok {
				tl.Calls[i].EndMS = at
				tl.Calls[i].OK = s.Status.Error == ""
				tl.Calls[i].Detail = s.Status.Error
			}
		}
	}
}

// diagAttempt is a model_attempt line of a run's stderr.log.
type diagAttempt struct {
	Diag        string    `json:"diag"`
	At          time.Time `json:"at"`
	Kind        string    `json:"kind"`
	FirstByteMS int64     `json:"first_byte_ms"`
	Result      string    `json:"result"`
	// Effort and EffortReason are the request's effort and, with adaptive effort,
	// why.
	Effort       string `json:"effort"`
	EffortReason string `json:"effort_reason"`
}

// addFirstBytes sets the first byte of each request from the runner's
// model_attempt diagnostics, the attempt that started nearest the request's
// start within a second; a request without one keeps its first streamed
// text, which comes later. The attempt's effort, when it has one, replaces
// the session's: with adaptive effort a request's effort is its own.
func addFirstBytes(tl *Timeline, stateDir string) {
	logs, _ := filepath.Glob(filepath.Join(stateDir, "runs", "*", "stderr.log"))
	var attempts []diagAttempt
	for _, l := range logs {
		f, err := os.Open(l)
		if err != nil {
			continue
		}
		_ = eachLine(f, func(_ time.Time, line []byte) error {
			var a diagAttempt
			if json.Unmarshal(line, &a) == nil && a.Diag == "model_attempt" && a.Result == "ok" && a.FirstByteMS > 0 {
				attempts = append(attempts, a)
			}

			return nil
		})
		_ = f.Close()
	}
	for i := range tl.Requests {
		r := &tl.Requests[i]
		best, bestD := -1, int64(math.MaxInt64)
		for j, a := range attempts {
			d := ms(tl.Start, a.At) - r.StartMS
			if d < 0 {
				d = -d
			}
			if d < bestD && d <= 1000 {
				best, bestD = j, d
			}
		}
		if best >= 0 {
			a := attempts[best]
			r.FirstMS = ms(tl.Start, a.At) + a.FirstByteMS
			if a.Effort != "" {
				r.Effort, r.EffortReason = a.Effort, a.EffortReason
			}
		}
	}
}
