package bench

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/viktordanov/uah/internal/usage/cachestats"
)

// Sessions of several user messages, turn by turn: what each user turn
// cost, and a replay of a run's requests under other effort rules with a
// model of the prompt cache. The provider keeps a prompt cache per effort,
// so with adaptive effort a user turn misses twice: its first request, at
// the user's effort, finds only what the last request at that effort sent,
// before the previous turn's tool work; its first follow-up, at the lower
// effort, misses the user's message and one response.

// RunRequests is one run's main and subagent requests and compactions:
// what the per-turn report reads, kept in history beside the results.
type RunRequests struct {
	Key

	Passed      bool         `json:"passed"`
	WallMS      int64        `json:"wall_ms"`
	Requests    []Request    `json:"requests"`
	Compactions []Compaction `json:"compactions,omitempty"`
}

// LoadRunRequests reads the timeline of each uah run in results.
func LoadRunRequests(results []Result) ([]RunRequests, error) {
	var out []RunRequests
	for _, r := range results {
		if r.Harness != HarnessUAH || r.StartedAt.IsZero() {
			continue
		}
		b, err := os.ReadFile(filepath.Join(r.Artifacts, "timeline.json"))
		if err != nil {
			return nil, err
		}
		var tl Timeline
		if err := json.Unmarshal(b, &tl); err != nil {
			return nil, fmt.Errorf("%s: %w", r.Key, err)
		}
		out = append(out, RunRequests{Key: r.Key, Passed: r.Passed, WallMS: r.Metrics.WallMS, Requests: tl.Requests, Compactions: tl.Compactions})
	}

	return out, nil
}

// TurnStat is one user turn of a run's main agent: its requests, from the
// user's message to the next.
type TurnStat struct {
	Turn     int
	Requests int
	Tokens   Tokens
	CostUSD  float64
	// Context is the input of the turn's first request, its opener: the
	// context when the user's message arrived.
	Context int64
	// OpenerEffort is the opener's effort, and OpenerMiss its uncached
	// input.
	OpenerEffort string
	OpenerMiss   int64
	// FollowMiss is the uncached input of the turn's first follow-up, 0
	// in a turn of one request.
	FollowMiss int64
	ModelMS    int64
}

// TurnStats splits the main agent's requests by user turn.
func (rr RunRequests) TurnStats(price Price) []TurnStat {
	var out []TurnStat
	for _, r := range rr.Requests {
		if r.Agent != "" || r.Turn == 0 {
			continue
		}
		if len(out) == 0 || out[len(out)-1].Turn != r.Turn {
			out = append(out, TurnStat{Turn: r.Turn, Context: r.Tokens.Input, OpenerEffort: r.Effort, OpenerMiss: r.Tokens.Input - r.Tokens.Cached})
		} else if out[len(out)-1].Requests == 1 {
			out[len(out)-1].FollowMiss = r.Tokens.Input - r.Tokens.Cached
		}
		t := &out[len(out)-1]
		t.Requests++
		t.Tokens = t.Tokens.Add(r.Tokens)
		t.ModelMS += r.EndMS - r.StartMS
	}
	for i := range out {
		out[i].CostUSD = price.Cost(out[i].Tokens)
	}

	return out
}

// Rule picks whether a replayed main agent request goes at the lowered
// effort. miss is the input it would not find cached at the user's effort.
type Rule struct {
	Name  string
	Lower func(r ReplayRequest, miss int64) bool
}

// ReplayRequest is what a Rule sees of a request.
type ReplayRequest struct {
	Index  int // in the run's Requests
	Turn   int
	Opener bool  // the first request of its user turn
	Input  int64 // its whole input, the context
}

// Rules are the effort rules the per-turn report replays: adaptive effort
// as it is (R0), off, and the alternatives that keep a user turn's
// requests on one cache.
func Rules() []Rule {
	r0 := func(r ReplayRequest, _ int64) bool { return !r.Opener }
	rules := []Rule{
		{"off", func(ReplayRequest, int64) bool { return false }},
		{"R0", r0},
		{"R0, later messages lowered too", func(r ReplayRequest, _ int64) bool { return !r.Opener || r.Turn > 1 }},
	}
	for _, t := range []int64{4_000, 8_000, 16_000} {
		rules = append(rules, Rule{fmt.Sprintf("R0, a message lowered when its miss > %dk", t/1000), func(r ReplayRequest, miss int64) bool {
			return !r.Opener || r.Turn > 1 && miss > t
		}})
	}
	for _, s := range []int64{32_000, 64_000, 128_000} {
		rules = append(rules, Rule{fmt.Sprintf("R0 below %dk context, off above", s/1000), func(r ReplayRequest, miss int64) bool {
			return r.Input <= s && r0(r, miss)
		}})
	}

	return rules
}

// Replay is the main agent's tokens had its requests gone at the efforts
// rule picks, with the user's effort high and the lowered one low. The
// cache model is internal/cachestats.Cache: each effort keeps the longest
// prompt sent at it, and a request finds cached the part of its input that
// prompt covers, in whole blocks; a compaction leaves every effort only the
// cross-session prefix (the first request's cached input). A request moved
// to the other effort has its output, reasoning included, scaled by ratio,
// the output at high per output at low. The subagents' tokens are not
// included.
func (rr RunRequests) Replay(rule Rule, high, low string, ratio float64) Tokens {
	var total Tokens
	var cache *cachestats.Cache
	compaction := 0
	for i, r := range rr.Requests {
		if r.Agent != "" {
			continue
		}
		if cache == nil {
			cache = cachestats.NewCache(r.Tokens.Cached)
		}
		for compaction < len(rr.Compactions) && rr.Compactions[compaction].StartMS <= r.StartMS {
			compaction++
			cache.Reset()
		}
		req := ReplayRequest{Index: i, Turn: r.Turn, Opener: rr.opener(i), Input: r.Tokens.Input}
		e := high
		if rule.Lower(req, r.Tokens.Input-cache.Hit(high, r.Tokens.Input)) {
			e = low
		}
		tok := r.Tokens
		tok.Cached = cache.Hit(e, r.Tokens.Input)
		switch {
		case r.Effort == low && e == high:
			tok.Output, tok.Reasoning = int64(float64(tok.Output)*ratio), int64(float64(tok.Reasoning)*ratio)
		case r.Effort == high && e == low:
			tok.Output, tok.Reasoning = int64(float64(tok.Output)/ratio), int64(float64(tok.Reasoning)/ratio)
		}
		cache.Sent(e, r.Tokens.Input)
		total = total.Add(tok)
	}

	return total
}

// opener says whether main agent request i is the first of its turn.
func (rr RunRequests) opener(i int) bool {
	for j := i - 1; j >= 0; j-- {
		if rr.Requests[j].Agent == "" {
			return rr.Requests[j].Turn != rr.Requests[i].Turn
		}
	}

	return true
}

// ruleRecorded names the rule of Recorded.
const ruleRecorded = "recorded"

// Recorded is a rule that keeps each request's recorded effort, to check
// the cache model against the run's own cached tokens.
func (rr RunRequests) Recorded(low string) Rule {
	return Rule{ruleRecorded, func(r ReplayRequest, _ int64) bool { return rr.Requests[r.Index].Effort == low }}
}

// lowered is the effort the run's main agent lowered to: the most common
// request effort other than the session's, "" when none.
func (rr RunRequests) lowered() string {
	n := map[string]int{}
	for _, r := range rr.Requests {
		if r.Agent == "" && r.Effort != "" && r.Effort != rr.Effort {
			n[r.Effort]++
		}
	}
	best := ""
	for e, c := range n {
		if c > n[best] || c == n[best] && e < best {
			best = e
		}
	}

	return best
}

// group is a run's column in the per-turn report: its harness label, with
// the effort when it is not high.
func (rr RunRequests) group() string {
	if rr.Effort == "high" {
		return rr.Label()
	}

	return rr.Label() + " @" + rr.Effort
}

// TurnReport is the markdown report of multi-message runs: per task and
// user turn, each group's medians; the turns summed over the tasks; and
// each lowered group's runs replayed under Rules.
func TurnReport(runs []RunRequests, price Price) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Per-turn report\n\nGenerated %s from %d uah runs. Values are medians over a task's runs; cost at $%.2f / $%.3f / $%.2f per million input / cached / output tokens, main agent only. A turn's opener is its first request (the user's message); miss is uncached input.\n",
		time.Now().UTC().Format(time.DateTime), len(runs), price.Input, price.Cached, price.Output)
	var groups, tasks []string
	byKey := map[[2]string][]RunRequests{}
	for _, rr := range runs {
		k := [2]string{rr.Task, rr.group()}
		byKey[k] = append(byKey[k], rr)
		if !slices.Contains(groups, rr.group()) {
			groups = append(groups, rr.group())
		}
		if !slices.Contains(tasks, rr.Task) {
			tasks = append(tasks, rr.Task)
		}
	}
	slices.Sort(groups)
	slices.Sort(tasks)
	groupTotals(&b, tasks, groups, byKey, price)
	turnsByTask(&b, tasks, groups, byKey, price)
	turnsSummed(&b, tasks, groups, byKey, price)
	missBySize(&b, tasks, groups, byKey, price)
	replays(&b, tasks, groups, byKey, price)

	return b.String()
}

// turnMedians is a group's median TurnStat per turn over its runs of one
// task; a run without the turn does not count for it.
func turnMedians(rs []RunRequests, price Price) []TurnStat {
	per := map[int][]TurnStat{}
	last := 0
	for _, rr := range rs {
		for _, t := range rr.TurnStats(price) {
			per[t.Turn] = append(per[t.Turn], t)
			last = max(last, t.Turn)
		}
	}
	var out []TurnStat
	for turn := 1; turn <= last; turn++ {
		ts := per[turn]
		if len(ts) == 0 {
			continue
		}
		med := func(f func(TurnStat) float64) float64 {
			v := make([]float64, len(ts))
			for i, t := range ts {
				v[i] = f(t)
			}

			return medianOf(v)
		}
		out = append(out, TurnStat{
			Turn:         turn,
			Requests:     int(med(func(t TurnStat) float64 { return float64(t.Requests) })),
			Context:      int64(med(func(t TurnStat) float64 { return float64(t.Context) })),
			OpenerEffort: ts[0].OpenerEffort,
			OpenerMiss:   int64(med(func(t TurnStat) float64 { return float64(t.OpenerMiss) })),
			FollowMiss:   int64(med(func(t TurnStat) float64 { return float64(t.FollowMiss) })),
			Tokens: Tokens{
				Input:     int64(med(func(t TurnStat) float64 { return float64(t.Tokens.Input) })),
				Cached:    int64(med(func(t TurnStat) float64 { return float64(t.Tokens.Cached) })),
				Output:    int64(med(func(t TurnStat) float64 { return float64(t.Tokens.Output) })),
				Reasoning: int64(med(func(t TurnStat) float64 { return float64(t.Tokens.Reasoning) })),
			},
			CostUSD: med(func(t TurnStat) float64 { return t.CostUSD }),
			ModelMS: int64(med(func(t TurnStat) float64 { return float64(t.ModelMS) })),
		})
	}

	return out
}

func medianOf(v []float64) float64 {
	if len(v) == 0 {
		return 0
	}
	v = slices.Clone(v)
	slices.Sort(v)
	if n := len(v); n%2 == 0 {
		return (v[n/2-1] + v[n/2]) / 2
	}

	return v[len(v)/2]
}

func k(n int64) string { return fmt.Sprintf("%.1fk", float64(n)/1000) }

func turnsByTask(b *strings.Builder, tasks, groups []string, byKey map[[2]string][]RunRequests, price Price) {
	b.WriteString("\n## Per task and turn\n")
	for _, task := range tasks {
		fmt.Fprintf(b, "\n### %s\n\n| Turn | Group | Passed | Context | Requests | Opener miss | 1st follow-up miss | Uncached | Reasoning | Output | Model s | Cost |\n| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n", task)
		meds := map[string][]TurnStat{}
		last := 0
		for _, g := range groups {
			meds[g] = turnMedians(byKey[[2]string{task, g}], price)
			if n := len(meds[g]); n > 0 {
				last = max(last, meds[g][n-1].Turn)
			}
		}
		for turn := 1; turn <= last; turn++ {
			for _, g := range groups {
				for _, t := range meds[g] {
					if t.Turn != turn {
						continue
					}
					rs := byKey[[2]string{task, g}]
					fmt.Fprintf(b, "| %d | %s | %d/%d | %s | %d | %s | %s | %s | %s | %s | %.0f | $%.4f |\n", turn, g, passedOf(rs), len(rs), k(t.Context), t.Requests,
						k(t.OpenerMiss), k(t.FollowMiss), k(t.Tokens.Input-t.Tokens.Cached), k(t.Tokens.Reasoning), k(t.Tokens.Output), s(t.ModelMS), t.CostUSD)
				}
			}
		}
	}
}

func passedOf(rs []RunRequests) int {
	n := 0
	for _, r := range rs {
		if r.Passed {
			n++
		}
	}

	return n
}

// turnsSummed sums each turn's medians over the tasks: how the cost of a
// turn grows with the session.
func turnsSummed(b *strings.Builder, tasks, groups []string, byKey map[[2]string][]RunRequests, price Price) {
	b.WriteString("\n## Turns summed over the tasks\n\nEach cell sums the task medians of that turn.\n\n| Turn | Group | Tasks | Context | Opener miss | 1st follow-up miss | Reasoning | Output | Model s | Cost |\n| ---: | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	type sum struct {
		n                                           int
		ctx, open, follow, reasoning, output, model int64
		cost                                        float64
	}
	sums := map[int]map[string]*sum{}
	last := 0
	for _, task := range tasks {
		for _, g := range groups {
			for _, t := range turnMedians(byKey[[2]string{task, g}], price) {
				if sums[t.Turn] == nil {
					sums[t.Turn] = map[string]*sum{}
				}
				if sums[t.Turn][g] == nil {
					sums[t.Turn][g] = &sum{}
				}
				x := sums[t.Turn][g]
				x.n++
				x.ctx += t.Context
				x.open += t.OpenerMiss
				x.follow += t.FollowMiss
				x.reasoning += t.Tokens.Reasoning
				x.output += t.Tokens.Output
				x.model += t.ModelMS
				x.cost += t.CostUSD
				last = max(last, t.Turn)
			}
		}
	}
	for turn := 1; turn <= last; turn++ {
		for _, g := range groups {
			if x := sums[turn][g]; x != nil {
				fmt.Fprintf(b, "| %d | %s | %d | %s | %s | %s | %s | %s | %.0f | $%.3f |\n", turn, g, x.n, k(x.ctx), k(x.open), k(x.follow), k(x.reasoning), k(x.output), s(x.model), x.cost)
			}
		}
	}
}

// outputPerRequest is the mean output of the main agent's requests at
// effort e, its openers' or its follow-ups'.
func outputPerRequest(rs []RunRequests, e string, openers bool) float64 {
	var sum, n int64
	for _, rr := range rs {
		for i, r := range rr.Requests {
			if r.Agent == "" && r.Effort == e && rr.opener(i) == openers {
				sum += r.Tokens.Output
				n++
			}
		}
	}
	if n == 0 {
		return 0
	}

	return float64(sum) / float64(n)
}

// replays replays each lowered group's runs under every rule. The ratio of
// output between the efforts comes from the follow-ups: the control's at
// high against the group's at its lowered effort.
func replays(b *strings.Builder, tasks, groups []string, byKey map[[2]string][]RunRequests, price Price) {
	var control []RunRequests
	for _, task := range tasks {
		control = append(control, byKey[[2]string{task, HarnessUAH}]...)
	}
	for _, g := range groups {
		var rs []RunRequests
		for _, task := range tasks {
			rs = append(rs, byKey[[2]string{task, g}]...)
		}
		if len(rs) == 0 || rs[0].lowered() == "" || g == HarnessUAH {
			continue
		}
		high, low := rs[0].Effort, rs[0].lowered()
		lowOut := outputPerRequest(rs, low, false)
		ratio := 1.0
		if lowOut > 0 && len(control) > 0 {
			ratio = outputPerRequest(control, high, false) / lowOut
		}
		fmt.Fprintf(b, "\n## Replays of %s\n\nThe main agent's requests of each run replayed under each rule (%s and %s); the output of a request moved between them scaled by %.2f, the control's mean follow-up output at %s over this group's at %s. Cost is the sum over the tasks of the median run's.\n\n| Rule | Cost | Cached | Output | Against off |\n| --- | ---: | ---: | ---: | ---: |\n",
			g, high, low, ratio, high, low)
		rules := append([]Rule{{Name: ruleRecorded}}, Rules()...)
		costs := make([]float64, len(rules))
		cached := make([]float64, len(rules))
		output := make([]float64, len(rules))
		recorded := 0.0
		var actualCached, actualInput float64
		for _, task := range tasks {
			trs := byKey[[2]string{task, g}]
			if len(trs) == 0 {
				continue
			}
			for i, rule := range rules {
				var c, ca, o []float64
				for _, rr := range trs {
					if rule.Name == ruleRecorded {
						rule = rr.Recorded(low)
					}
					tok := rr.Replay(rule, high, low, ratio)
					c = append(c, price.Cost(tok))
					ca = append(ca, float64(tok.Cached)/float64(max(1, tok.Input)))
					o = append(o, float64(tok.Output))
				}
				costs[i] += medianOf(c)
				cached[i] += medianOf(ca)
				output[i] += medianOf(o)
			}
			var ac []float64
			for _, rr := range trs {
				var t Tokens
				for _, r := range rr.Requests {
					if r.Agent == "" {
						t = t.Add(r.Tokens)
					}
				}
				ac = append(ac, price.Cost(t))
				actualCached += float64(t.Cached)
				actualInput += float64(t.Input)
			}
			recorded += medianOf(ac)
		}
		n := float64(len(tasks))
		off := costs[1]
		fmt.Fprintf(b, "| as run (measured) | $%.3f | %.1f%% | | %+.0f%% |\n", recorded, 100*actualCached/max(1, actualInput), 100*(recorded/off-1))
		for i, rule := range rules {
			fmt.Fprintf(b, "| %s | $%.3f | %.1f%% | %s | %+.0f%% |\n", rule.Name, costs[i], 100*cached[i]/n, k(int64(output[i])), 100*(costs[i]/off-1))
		}
	}
}

// WriteRunRequests writes runs as JSON lines.
func WriteRunRequests(path string, runs []RunRequests) error {
	var b []byte
	for _, rr := range runs {
		line, err := json.Marshal(rr)
		if err != nil {
			return err
		}
		b = append(append(b, line...), '\n')
	}

	return os.WriteFile(path, b, 0o644)
}

// ReadRunRequests reads what WriteRunRequests wrote.
func ReadRunRequests(path string) ([]RunRequests, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []RunRequests
	err = eachLine(f, func(_ time.Time, line []byte) error {
		var rr RunRequests
		if err := json.Unmarshal(line, &rr); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		out = append(out, rr)

		return nil
	})

	return out, err
}

// groupTotals sums each group's task medians: the whole run's wall time,
// and the main agent's model time, tokens, and cost.
func groupTotals(b *strings.Builder, tasks, groups []string, byKey map[[2]string][]RunRequests, price Price) {
	b.WriteString("\n## Groups\n\nSums of the task medians; the tokens and cost are the main agent's.\n\n| Group | Runs | Passed | Wall s | Model s | Requests | Uncached | Cached | Output | Cost |\n| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, g := range groups {
		var runs, passed int
		var wall, model, reqs, uncached, cached, output, cost float64
		for _, task := range tasks {
			rs := byKey[[2]string{task, g}]
			if len(rs) == 0 {
				continue
			}
			runs += len(rs)
			passed += passedOf(rs)
			var w, m, n, u, c, o, d []float64
			for _, rr := range rs {
				var t Tokens
				var ms, count int64
				for _, r := range rr.Requests {
					if r.Agent == "" {
						t = t.Add(r.Tokens)
						ms += r.EndMS - r.StartMS
						count++
					}
				}
				w = append(w, float64(rr.WallMS))
				m = append(m, float64(ms))
				n = append(n, float64(count))
				u = append(u, float64(t.Input-t.Cached))
				c = append(c, float64(t.Cached))
				o = append(o, float64(t.Output))
				d = append(d, price.Cost(t))
			}
			wall += medianOf(w)
			model += medianOf(m)
			reqs += medianOf(n)
			uncached += medianOf(u)
			cached += medianOf(c)
			output += medianOf(o)
			cost += medianOf(d)
		}
		fmt.Fprintf(b, "| %s | %d | %d | %.0f | %.0f | %.0f | %s | %s | %s | $%.3f |\n", g, runs, passed, wall/1000, model/1000, reqs, k(int64(uncached)), k(int64(cached)), k(int64(output)), cost)
	}
}

// sizeBucket is a row of missBySize: the turns whose context is under max.
type sizeBucket struct {
	name string
	max  int64
}

// missBySize pairs each lowered group's turns with the control's, task by
// task and turn by turn (medians), and sums them by the control's context
// at the turn's opener: the extra uncached input of the opener and the
// first follow-up, what it cost, the output the lower effort saved, what
// that saved, and the turns' cost against the control's.
func missBySize(b *strings.Builder, tasks, groups []string, byKey map[[2]string][]RunRequests, price Price) {
	buckets := []sizeBucket{{"turn 1", 0}, {"< 32k", 32_000}, {"32k-64k", 64_000}, {"64k-128k", 128_000}, {"> 128k", 1 << 62}}
	missPrice, outPrice := (price.Input-price.Cached)/1e6, price.Output/1e6
	b.WriteString("\n## Miss against saving, by context\n\nEach task's turns, medians, against the control's same task and turn, summed by the control's context at the turn's opener. Extra miss is the opener's and the first follow-up's uncached input above the control's, at the uncached price less the cached; output saved is the control's output less the group's.\n\n| Group | Context | Turns | Extra opener miss | Extra 1st follow-up miss | Miss cost | Output saved | Saving | Turn cost against control |\n| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |\n")
	for _, g := range groups {
		if g == HarnessUAH {
			continue
		}
		type sum struct {
			n                    int
			open, follow, output int64
			cost, ctlCost        float64
		}
		sums := make([]sum, len(buckets))
		for _, task := range tasks {
			ctl := turnMedians(byKey[[2]string{task, HarnessUAH}], price)
			for _, t := range turnMedians(byKey[[2]string{task, g}], price) {
				i := slices.IndexFunc(ctl, func(c TurnStat) bool { return c.Turn == t.Turn })
				if i < 0 {
					continue
				}
				c := ctl[i]
				bi := 0
				if t.Turn > 1 {
					bi = 1 + slices.IndexFunc(buckets[1:], func(x sizeBucket) bool { return c.Context < x.max })
				}
				x := &sums[bi]
				x.n++
				x.open += t.OpenerMiss - c.OpenerMiss
				x.follow += t.FollowMiss - c.FollowMiss
				x.output += c.Tokens.Output - t.Tokens.Output
				x.cost += t.CostUSD
				x.ctlCost += c.CostUSD
			}
		}
		for i, x := range sums {
			if x.n == 0 {
				continue
			}
			fmt.Fprintf(b, "| %s | %s | %d | %s | %s | $%.3f | %s | $%.3f | %+.0f%% |\n", g, buckets[i].name, x.n, k(x.open), k(x.follow),
				float64(x.open+x.follow)*missPrice, k(x.output), float64(x.output)*outPrice, 100*(x.cost/x.ctlCost-1))
		}
	}
}
