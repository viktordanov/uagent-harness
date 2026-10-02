package bench

import (
	"regexp"
	"slices"
)

// Behavior counts what the mining of the owner's sessions found costs
// time: the model is most of the wall time, so the gains are in fewer and
// cheaper requests, not in the tools.
type Behavior struct {
	// The output tokens split by what they wrote: reasoning as reported,
	// and the rest by the bytes of the patches, of the other tools'
	// arguments, and of the text. Codex reports per turn only and its
	// patch events hold paths, not hunks, so its split is rough.
	OutputReasoning int64 `json:"output_reasoning"`
	OutputPatch     int64 `json:"output_patch"`
	OutputToolArgs  int64 `json:"output_tool_args"`
	OutputText      int64 `json:"output_text"`
	// Efforts counts the requests at each effort.
	Efforts map[string]int `json:"efforts,omitempty"`
	// RitualRequests are the main agent's first requests that only load a
	// skill or read an instructions file (AGENTS.md, RTK.md, CLAUDE.md);
	// RitualMS is the time from the first to the next real request.
	RitualRequests int   `json:"ritual_requests"`
	RitualMS       int64 `json:"ritual_ms"`
	// PatchThenVerify counts requests that only patched, followed by one
	// that runs a command: a build or a test that could have gone with the
	// patch.
	PatchThenVerify int `json:"patch_then_verify"`
	// Escalations are calls that asked to run outside the sandbox;
	// ReviewMS is the time between issuing and starting them, the
	// approval's latency (uah only: Codex's events do not show it).
	Escalations        int   `json:"escalations"`
	EscalationsRefused int   `json:"escalations_refused"`
	ReviewMS           int64 `json:"review_ms"`
	ReviewMedianMS     int64 `json:"review_median_ms"`
	// ApprovalWaits counts every call that started 300 ms or more after
	// it was issued (an escalation, or a patch outside the workspace,
	// waiting for its approval), and ApprovalWaitMS their total wait.
	ApprovalWaits  int   `json:"approval_waits"`
	ApprovalWaitMS int64 `json:"approval_wait_ms"`
	// Aborted counts requests that did not complete (canceled, failed, or
	// cut off), and AbortedMS the model time they took.
	Aborted   int   `json:"aborted"`
	AbortedMS int64 `json:"aborted_ms"`
	// Compactions counts the context compactions (uah only), and
	// CompactionMS the time their summary calls took.
	Compactions  int   `json:"compactions"`
	CompactionMS int64 `json:"compaction_ms"`
	// The prompt cache split by effort: the main agent's requests after
	// its first, at the same effort as the request before them or at
	// another, with their input and cached input tokens and the cached
	// share (0 without such requests). Adaptive effort changes the effort per
	// request; these show whether the cache follows.
	SameEffortRequests      int     `json:"same_effort_requests"`
	SameEffortInput         int64   `json:"same_effort_input"`
	SameEffortCached        int64   `json:"same_effort_cached"`
	SameEffortCacheRatio    float64 `json:"same_effort_cache_ratio"`
	ChangedEffortRequests   int     `json:"changed_effort_requests"`
	ChangedEffortInput      int64   `json:"changed_effort_input"`
	ChangedEffortCached     int64   `json:"changed_effort_cached"`
	ChangedEffortCacheRatio float64 `json:"changed_effort_cache_ratio"`
}

// approvalWait is the shortest wait between issuing and starting a call
// that counts as waiting for an approval.
const approvalWait = 300

// Tool names by what they do.
var (
	patchTools = []string{"apply_patch", "Edit", "Write", "MultiEdit"}
	shellTools = []string{"Bash", "shell", "exec_command"}
	ritualArgs = regexp.MustCompile(`(AGENTS|RTK|CLAUDE)\.md`)
	// compound is a command that does more than read a file.
	compound = regexp.MustCompile(`;|&&|\|\||\| |\brg\b|\bls\b|\bfind\b|\bgit\b`)
)

func (tl *Timeline) behavior() Behavior {
	b := Behavior{Efforts: map[string]int{}}
	byReq := make([][]Call, len(tl.Requests))
	var unattached []Call
	for _, c := range tl.Calls {
		if c.Request >= 0 && c.Request < len(byReq) {
			byReq[c.Request] = append(byReq[c.Request], c)
		} else {
			unattached = append(unattached, c)
		}
		if d := c.StartMS - c.IssuedMS; d >= approvalWait {
			b.ApprovalWaits++
			b.ApprovalWaitMS += d
		}
		if c.Escalated {
			b.Escalations++
			if !c.OK {
				b.EscalationsRefused++
			}
		}
	}
	b.ReviewMS, b.ReviewMedianMS = reviewTimes(tl.Calls)
	tl.splitOutput(&b, byReq, unattached)
	last := map[string]int{} // each agent's previous request
	ritual := true
	for i, r := range tl.Requests {
		if r.Effort != "" {
			b.Efforts[r.Effort]++
		}
		if r.Stop != "complete" {
			b.Aborted++
			b.AbortedMS += r.EndMS - r.StartMS
		}
		prev, seen := last[r.Agent]
		last[r.Agent] = i
		if seen && onlyPatches(byReq[prev]) && anyShell(byReq[i]) {
			b.PatchThenVerify++
		}
		if r.Agent != "" {
			continue
		}
		if ritual && isRitual(byReq[i]) {
			b.RitualRequests++
			b.RitualMS += tl.nextStart(i) - r.StartMS
		} else {
			ritual = false
		}
	}
	for _, c := range tl.Compactions {
		b.Compactions++
		b.CompactionMS += c.EndMS - c.StartMS
	}
	if len(b.Efforts) == 0 {
		b.Efforts = nil
	}
	tl.cacheByEffort(&b)

	return b
}

// cacheByEffort splits the main agent's cached input by whether each
// request's effort is its previous request's.
func (tl *Timeline) cacheByEffort(b *Behavior) {
	prev := ""
	for _, r := range tl.Requests {
		if r.Agent != "" {
			continue
		}
		switch {
		case prev == "" || r.Effort == "":
		case r.Effort == prev:
			b.SameEffortRequests++
			b.SameEffortInput += r.Tokens.Input
			b.SameEffortCached += r.Tokens.Cached
		default:
			b.ChangedEffortRequests++
			b.ChangedEffortInput += r.Tokens.Input
			b.ChangedEffortCached += r.Tokens.Cached
		}
		prev = r.Effort
	}
	if b.SameEffortInput > 0 {
		b.SameEffortCacheRatio = float64(b.SameEffortCached) / float64(b.SameEffortInput)
	}
	if b.ChangedEffortInput > 0 {
		b.ChangedEffortCacheRatio = float64(b.ChangedEffortCached) / float64(b.ChangedEffortInput)
	}
}

// nextStart is when the main agent's next request after i starts, else
// when request i ends.
func (tl *Timeline) nextStart(i int) int64 {
	for _, r := range tl.Requests[i+1:] {
		if r.Agent == "" {
			return r.StartMS
		}
	}

	return tl.Requests[i].EndMS
}

func isRitual(calls []Call) bool {
	if len(calls) == 0 {
		return false
	}
	for _, c := range calls {
		if c.Name != "SkillUse" && (!ritualArgs.MatchString(c.Args) || compound.MatchString(c.Args)) {
			return false
		}
	}

	return true
}

func onlyPatches(calls []Call) bool {
	if len(calls) == 0 {
		return false
	}
	for _, c := range calls {
		if !slices.Contains(patchTools, c.Name) {
			return false
		}
	}

	return true
}

func anyShell(calls []Call) bool {
	return slices.ContainsFunc(calls, func(c Call) bool { return slices.Contains(shellTools, c.Name) })
}

func reviewTimes(calls []Call) (total, median int64) {
	var ds []int64
	for _, c := range calls {
		if c.Escalated {
			ds = append(ds, c.StartMS-c.IssuedMS)
			total += c.StartMS - c.IssuedMS
		}
	}
	if len(ds) == 0 {
		return 0, 0
	}
	slices.Sort(ds)

	return total, ds[len(ds)/2]
}

// splitOutput splits each request's output tokens by the bytes it wrote;
// when the requests have no tokens (Codex), the run's totals are split by
// all the bytes.
func (tl *Timeline) splitOutput(b *Behavior, byReq [][]Call, unattached []Call) {
	perRequest := false
	for _, r := range tl.Requests {
		if r.Tokens.Output > 0 {
			perRequest = true

			break
		}
	}
	if !perRequest {
		var all []Call
		text := 0
		for i, r := range tl.Requests {
			all = append(all, byReq[i]...)
			text += r.TextBytes
		}
		addSplit(b, tl.Tokens, append(all, unattached...), text)

		return
	}
	for i, r := range tl.Requests {
		addSplit(b, r.Tokens, byReq[i], r.TextBytes)
	}
}

func addSplit(b *Behavior, tok Tokens, calls []Call, text int) {
	b.OutputReasoning += tok.Reasoning
	rest := tok.Output - tok.Reasoning
	var patch, args int
	for _, c := range calls {
		if slices.Contains(patchTools, c.Name) {
			patch += c.ArgsBytes
		} else {
			args += c.ArgsBytes
		}
	}
	all := patch + args + text
	if all == 0 {
		b.OutputText += rest

		return
	}
	p := rest * int64(patch) / int64(all)
	a := rest * int64(args) / int64(all)
	b.OutputPatch += p
	b.OutputToolArgs += a
	b.OutputText += rest - p - a
}
