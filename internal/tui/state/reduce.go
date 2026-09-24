package state

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uagent-harness/internal/session"
)

const (
	// confirmWindow is how long a first Esc or Ctrl+C waits for the second.
	confirmWindow = 2 * time.Second
)

// Reduce applies an event or intent. It takes ownership of s: callers keep
// only the returned state, which lets items update in place.
func Reduce(s State, ev any) (State, []Effect) {
	if s.index == nil {
		s.index = map[string]int{}
	}
	if s.View != nil {
		if effects, ok := s.onAgentView(ev); ok {
			return s, effects
		}
	}
	switch e := ev.(type) {
	case Tick:
		s.Now = e.Now
		s.expireConfirmations()
		if s.View != nil {
			s.View.St.Now = e.Now
			s.View.St.expireConfirmations()
		}

		return s, nil
	case SwitchAgent:
		return s, s.switchAgent(e.Delta)
	case AgentViewOpened:
		s.openAgentView(e)

		return s, nil
	case AgentEvents:
		return s, nil // a view that closed meanwhile
	case session.ApprovalRequested:
		s.requestApproval(e)

		return s, nil
	case session.ApprovalResolved:
		s.resolveApproval(e)

		return s, nil
	case Answer:
		return s.answer(e)
	case ContextShown:
		s.onContextView(e)

		return s, nil
	case core.Event:
		s.onEvent(e)

		return s, nil
	}
	if effects, ok := s.onConfig(ev); ok {
		return s, effects
	}
	if effects, ok := s.onMenu(ev); ok {
		return s, effects
	}

	return s.onIntent(ev)
}

func (s *State) onEvent(ev core.Event) {
	switch e := ev.(type) {
	case session.SessionOpened:
		if e.ID != s.SessionID {
			s.resetTranscript()
			s.View = nil
		}
		s.SessionID, s.Resumed, s.Engine, s.Settings = e.ID, e.Resumed, e.Engine, e.Settings
		s.Queue, s.Live, s.Busy, s.Quitting, s.Approvals = nil, nil, false, false, nil
	case session.InstructionsLoaded:
		s.Files = e.Files
	case session.InputQueued:
		s.Queue = append(s.Queue, Queued{ID: e.Input.ID, Text: e.Input.Text})
		s.Busy = true
	case session.InputSent:
		for _, id := range e.IDs {
			i := slices.IndexFunc(s.Queue, func(q Queued) bool { return q.ID == id })
			if i < 0 {
				continue
			}
			q := s.Queue[i]
			s.Queue = slices.Delete(s.Queue, i, i+1)
			s.put(Item{Kind: KindUser, Key: "msg:" + q.ID, Text: q.Text, Input: InputSent})
		}
	case session.InputDelivered:
		s.update("msg:"+e.ID, func(it *Item) { it.Input = InputDelivered })
	case session.InputFailed:
		for _, id := range e.IDs {
			if !s.update("msg:"+id, func(it *Item) { it.Input = InputFailed }) {
				s.Queue = slices.DeleteFunc(s.Queue, func(q Queued) bool { return q.ID == id })
			}
		}
		s.notice(session.LevelError, "not delivered: "+e.Reason)
	case session.InputWithdrawn:
		s.Queue = slices.DeleteFunc(s.Queue, func(q Queued) bool { return q.ID == e.ID })
	case session.SettingsChanged:
		s.settingsChanged(e)
	case session.HookRan:
		switch e.Outcome {
		case "ok":
			s.notice(LevelDebug, fmt.Sprintf("hook %s · %s · %s", e.Event, e.Command, e.Duration.Round(time.Millisecond)))
		case "blocked":
			s.notice(session.LevelWarning, fmt.Sprintf("%s hook blocked: %s", e.Event, e.Reason))
		default:
			s.notice(session.LevelWarning, fmt.Sprintf("%s hook %s: %s", e.Event, e.Outcome, e.Reason))
		}
	case session.Idle:
		s.Busy, s.Live = false, nil
	case session.Notice:
		s.notice(e.Level, e.Message)
	default:
		if !s.onEngineEvent(ev) {
			s.onRunEvent(ev)
		}
	}
}

func (s *State) finishRun(r core.Result) {
	s.update("run:"+r.Request.RunID, func(it *Item) {
		it.Status, it.Wall, it.Tokens = r.Status, r.Wall, r.Stats.Tokens.InputTokens+r.Stats.Tokens.OutputTokens
	})
	if !s.update("done:"+r.Request.RunID, func(it *Item) { it.Status, it.Wall = r.Status, r.Wall }) {
		s.put(Item{Kind: KindFinish, Key: "done:" + r.Request.RunID, RunID: r.Request.RunID, Status: r.Status, Started: r.StartedAt, Wall: r.Wall})
	}
	for i := range s.Items {
		it := &s.Items[i]
		if it.Kind == KindTool && (it.Tool == ToolRunning || it.Tool == ToolCalled) {
			it.Tool, it.Detail = ToolStopped, "stopped"
			it.Version++
		}
		if it.Kind == KindTurn && it.Pending {
			it.Pending = false
			it.Version++
		}
	}
	s.Totals.Runs++
	s.Totals.Turns += r.Stats.Turns
	s.Totals.ToolCalls += r.Stats.ToolCalls
	s.Totals.MaxParallel = max(s.Totals.MaxParallel, r.Stats.MaxParallelTools)
	s.Totals.Tokens = s.Totals.Tokens.Add(r.Stats.Tokens)
	s.Totals.Overlap += r.Stats.ToolModelOverlap
	s.Totals.ToolBusy += r.Stats.ToolBusyTime
	s.Live = nil
}

// loadHistory rebuilds the transcript from saved runs.
func (s *State) loadHistory(h HistoryLoaded) {
	s.resetTranscript()
	s.SessionID = h.SessionID
	for _, run := range h.Runs {
		res := run.Record.Result
		s.onRunEvent(core.RunStarted{At: res.StartedAt, RunID: res.Request.RunID, SessionID: res.Request.SessionID})
		for _, e := range run.Events {
			if !s.onEngineEvent(e) {
				s.onRunEvent(e)
			}
		}
		if run.Record.Complete {
			s.onRunEvent(core.RunFinished{At: res.StartedAt.Add(res.Wall), Result: res})
		} else {
			s.finishRun(core.Result{Request: res.Request, Status: core.StatusRunning})
		}
	}
	s.Live = nil
}

func (s *State) onIntent(ev any) (State, []Effect) { //nolint:gocyclo // a dispatch switch over a closed set; see docs/documentation/architecture.md
	switch e := ev.(type) {
	case Submit:
		text := strings.TrimSpace(e.Text)
		if text == "" {
			return *s, nil
		}
		if strings.HasPrefix(text, "/") {
			return s.command(text)
		}
		s.Scroll = 0

		return *s, []Effect{EffSubmit{Text: text}}
	case Steer:
		text := strings.TrimSpace(e.Text)
		if text == "" || strings.HasPrefix(text, "/") {
			return s.onIntent(Submit(e))
		}
		s.Scroll = 0

		return *s, []Effect{EffSteer{Text: text}}
	case Esc:
		if !s.Busy {
			return *s, nil
		}
		if !s.escArmed.IsZero() && s.Now.Sub(s.escArmed) < confirmWindow {
			s.escArmed, s.Status = time.Time{}, ""

			return *s, []Effect{EffInterrupt{}}
		}
		s.escArmed, s.Status = s.Now, "press esc again to interrupt"

		return *s, nil
	case Quit:
		if !s.Busy || (!s.quitArmed.IsZero() && s.Now.Sub(s.quitArmed) < confirmWindow) {
			s.Quitting, s.Status = true, "stopping…"

			return *s, []Effect{EffQuit{}}
		}
		s.quitArmed, s.Status = s.Now, "press ctrl+c again to stop the run and quit"

		return *s, nil
	case EditLastQueued:
		if len(s.Queue) == 0 {
			return *s, nil
		}
		last := s.Queue[len(s.Queue)-1]

		return *s, []Effect{EffWithdraw(last)}
	case ToggleReasoning:
		s.ShowReasoning = !s.ShowReasoning
	case ToggleDetails:
		s.Details = !s.Details
	case ScrollBy:
		s.Scroll = max(0, s.Scroll+e.Lines)
	case ScrollToBottom:
		s.Scroll = 0
	case StepEffort:
		return s.stepEffort(e.Delta)
	case CycleMode:
		return s.cycleMode()
	case OpenPicker:
		return *s, []Effect{EffLoadSessions{}}
	case ActivityLoaded:
		s.notice(session.LevelInfo, Heatmap(e.Counts, s.Now, heatmapWeeks))
	case MCPListed:
		s.showMCP(e)
	case SessionsLoaded:
		s.Mode, s.Picker = ModePicker, Picker{Sessions: e.Sessions, Local: e.Local, All: e.All}
	case PickerToggleAll:
		s.Picker.All, s.Picker.Selected = !s.Picker.All, 0
	case PickerMove:
		n := len(s.Picker.Filtered())
		if n > 0 {
			s.Picker.Selected = (s.Picker.Selected + e.Delta%n + n) % n
		}
	case PickerType:
		if e.Text == "\b" {
			if r := []rune(s.Picker.Filter); len(r) > 0 {
				s.Picker.Filter = string(r[:len(r)-1])
			}
		} else {
			s.Picker.Filter += e.Text
		}
		s.Picker.Selected = 0
	case PickerChoose:
		list := s.Picker.Filtered()
		if len(list) == 0 {
			return *s, nil
		}
		s.Mode = ModeChat
		if list[s.Picker.Selected].ID == s.SessionID {
			return *s, nil
		}

		return *s, []Effect{EffOpenSession{ID: list[s.Picker.Selected].ID}}
	case PickerCancel:
		s.Mode = ModeChat
		if s.SessionID == "" {
			return *s, []Effect{EffOpenSession{}} // nothing chosen at startup: start a new session
		}
	case HistoryLoaded:
		s.loadHistory(e)
	case Failed:
		s.notice(session.LevelError, e.Err.Error())
		s.Quitting = false
	}

	return *s, nil
}

func (s *State) stepEffort(delta int) (State, []Effect) {
	i := slices.Index(session.Efforts, s.Settings.Effort)
	if i < 0 {
		i = slices.Index(session.Efforts, "high")
	}
	j := min(max(i+delta, 0), len(session.Efforts)-1)
	if j == i {
		return *s, nil
	}
	next := s.Settings
	next.Effort = session.Efforts[j]

	return *s, []Effect{EffSetSettings{Settings: next}}
}

func (s *State) expireConfirmations() {
	if !s.escArmed.IsZero() && s.Now.Sub(s.escArmed) >= confirmWindow {
		s.escArmed, s.Status = time.Time{}, ""
	}
	if !s.quitArmed.IsZero() && s.Now.Sub(s.quitArmed) >= confirmWindow {
		s.quitArmed, s.Status = time.Time{}, ""
	}
}

// put appends an item, or replaces the item with the same key.
func (s *State) put(it Item) {
	if i, ok := s.index[it.Key]; ok {
		it.Version = s.Items[i].Version + 1
		s.Items[i] = it

		return
	}
	s.index[it.Key] = len(s.Items)
	s.Items = append(s.Items, it)
	if s.Scroll > 0 {
		s.Scroll++ // keep the view anchored while the user reads back
	}
}

// update changes the item with key in place and reports whether it exists.
func (s *State) update(key string, fn func(*Item)) bool {
	i, ok := s.index[key]
	if !ok {
		return false
	}
	fn(&s.Items[i])
	s.Items[i].Version++

	return true
}

// LevelDebug notices show only in the detailed view.
const LevelDebug = "debug"

func (s *State) notice(level, text string) {
	s.put(Item{Kind: KindNotice, Key: s.nextKey("notice"), Level: level, Text: text})
}

func (s *State) nextKey(prefix string) string {
	s.nextNoticeN++

	return fmt.Sprintf("%s:%d", prefix, s.nextNoticeN)
}

func (s *State) resetTranscript() {
	s.Items, s.index, s.agentIDs, s.Totals, s.Scroll, s.Files, s.ContextUsed = nil, map[string]int{}, nil, Totals{}, 0, nil, 0
}

func turnKey(live *Live, turn int) string {
	run := ""
	if live != nil {
		run = live.RunID
	}

	return fmt.Sprintf("turn:%s:%d", run, turn)
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}
