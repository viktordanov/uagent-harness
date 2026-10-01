package bench

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// codexEvent is the part of a `codex exec --json` event the parser reads.
type codexEvent struct {
	Type string `json:"type"`
	Item struct {
		ID       string          `json:"id"`
		Type     string          `json:"type"`
		Command  string          `json:"command"`
		Text     string          `json:"text"`
		Tool     string          `json:"tool"`
		Server   string          `json:"server"`
		Query    string          `json:"query"`
		ExitCode *int            `json:"exit_code"`
		Status   string          `json:"status"`
		Changes  json.RawMessage `json:"changes"`
	} `json:"item"`
	Usage struct {
		Input     int64 `json:"input_tokens"`
		Cached    int64 `json:"cached_input_tokens"`
		Output    int64 `json:"output_tokens"`
		Reasoning int64 `json:"reasoning_output_tokens"`
	} `json:"usage"`
	Message string `json:"message"`
	Error   struct {
		Message string `json:"message"`
	} `json:"error"`
}

// minRequestMS is the shortest gap between Codex's tool calls that counts
// as a model request: a request's first byte alone takes longer.
const minRequestMS = 300

// ParseCodex builds a timeline from a `codex exec --json` stream whose
// lines the harness stamped with the time it read them (Codex's events
// carry no times). Codex reports neither model requests nor per-request
// tokens, so requests are inferred: the model is busy from a turn's start,
// or the end of the last running tool, until the next tool starts or the
// turn completes; a gap shorter than minRequestMS is not a request. A call
// that starts while another runs (Codex hands the model a long command
// before it ends) follows a request from the last event to it. Tokens are
// the turns' totals.
func ParseCodex(stream io.Reader, start time.Time) (*Timeline, error) {
	p := &codexParser{
		tl:    &Timeline{Harness: HarnessCodex, Start: start, Inferred: []string{"requests", "request_tokens"}},
		calls: map[string]int{}, segStart: -1, batch: -1,
	}
	tl := p.tl
	err := eachLine(stream, func(at time.Time, line []byte) error {
		var e codexEvent
		if err := json.Unmarshal(line, &e); err != nil {
			return fmt.Errorf("bad codex event: %w", err)
		}
		if at.IsZero() {
			return fmt.Errorf("codex event without a stamp: %s", line)
		}
		if at.After(tl.End) {
			tl.End = at
		}
		now := ms(start, at)
		switch e.Type {
		case "turn.started":
			tl.Turns++
			p.segStart, p.running, p.turnFirst = now, 0, true
		case "item.started":
			p.started(e, now)
		case "item.completed":
			p.completed(e, now)
		case "turn.completed":
			p.closeSeg(now)
			tl.Tokens = tl.Tokens.Add(Tokens{Input: e.Usage.Input, Cached: e.Usage.Cached, Output: e.Usage.Output, Reasoning: e.Usage.Reasoning})
		case "turn.failed", eventError:
			p.closeSeg(now)
			tl.Errors = append(tl.Errors, cmpOr(e.Error.Message, e.Message))
		}
		if strings.HasPrefix(e.Type, "item.") {
			p.lastItem = now
		}

		return nil
	})
	if err != nil {
		return nil, err
	}
	p.closeSeg(ms(start, tl.End))
	closeCalls(tl.Calls, ms(start, tl.End))

	return tl, nil
}

type codexParser struct {
	tl      *Timeline
	calls   map[string]int
	running int // tool calls started and not completed
	// segStart is the open model segment's start, or -1; turnFirst says
	// it is the turn's first.
	segStart  int64
	turnFirst bool
	batch     int   // the request whose tool calls are starting, or -1
	lastItem  int64 // the last item event
}

// closeSeg ends the open segment at at. A gap too short for a request is
// Codex running the next call of the same response.
func (p *codexParser) closeSeg(at int64) {
	if p.segStart >= 0 && (p.turnFirst || p.batch < 0 || at-p.segStart >= minRequestMS) {
		p.request(p.segStart, at)
	}
	p.segStart, p.turnFirst = -1, false
}

func (p *codexParser) request(start, end int64) {
	p.tl.Requests = append(p.tl.Requests, Request{StartMS: start, EndMS: end})
	p.batch = len(p.tl.Requests) - 1
}

func (p *codexParser) addCall(c Call) {
	p.tl.Calls = append(p.tl.Calls, c)
	if p.batch >= 0 {
		p.tl.Requests[p.batch].ToolCalls++
	}
}

func (p *codexParser) started(e codexEvent, now int64) {
	if !isCodexTool(e.Item.Type) {
		return
	}
	if p.running == 0 {
		p.closeSeg(now)
	} else if now-p.lastItem >= minRequestMS {
		// Codex gave the model a command still running (it yields a long
		// command): a new call while it runs follows a request since the
		// last event.
		p.request(p.lastItem, now)
	}
	p.running++
	p.calls[e.Item.ID] = len(p.tl.Calls)
	p.addCall(codexCall(e, now))
}

func (p *codexParser) completed(e codexEvent, now int64) {
	switch {
	case e.Item.Type == "agent_message":
		p.tl.Answer = e.Item.Text
	case !isCodexTool(e.Item.Type):
	case p.hasStarted(e.Item.ID):
		finishCodexCall(&p.tl.Calls[p.calls[e.Item.ID]], e, now)
		p.running--
		if p.running == 0 {
			p.segStart = now
		}
	default:
		// A tool with no start event (a patch) ran inside the gap: the
		// request that issued it ends here.
		if p.running == 0 {
			p.closeSeg(now)
			p.segStart = now
		}
		p.addCall(codexCall(e, now))
		finishCodexCall(&p.tl.Calls[len(p.tl.Calls)-1], e, now)
	}
}

func (p *codexParser) hasStarted(id string) bool {
	_, ok := p.calls[id]

	return ok
}

func isCodexTool(t string) bool {
	switch t {
	case "command_execution", "file_change", "mcp_tool_call", "web_search", "collab_tool_call":
		return true
	}

	return false
}

func codexCall(e codexEvent, at int64) Call {
	name, args := e.Item.Type, ""
	switch e.Item.Type {
	case "command_execution":
		name, args = "shell", e.Item.Command
	case "file_change":
		name, args = "apply_patch", string(e.Item.Changes)
	case "mcp_tool_call":
		name = e.Item.Server + "." + e.Item.Tool
	case "web_search":
		args = e.Item.Query
	}

	return Call{ID: e.Item.ID, Name: name, Kind: callKind(e.Item.Type), Args: summarize(args), IssuedMS: at, StartMS: at, EndMS: -1}
}

func finishCodexCall(c *Call, e codexEvent, at int64) {
	c.EndMS = at
	c.OK = e.Item.Status == "completed"
	if e.Item.ExitCode != nil {
		c.Detail = fmt.Sprintf("exit %d", *e.Item.ExitCode)
		c.OK = c.OK && *e.Item.ExitCode == 0
	}
}
