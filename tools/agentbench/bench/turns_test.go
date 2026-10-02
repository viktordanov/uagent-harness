package bench_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uah/tools/agentbench/bench"
)

func TestParseUAHTurns(t *testing.T) {
	stream := `{"v":1,"type":"user_message","at":"2026-10-02T10:00:00Z","text":"<workspace_context>\nprimed\n</workspace_context>"}
{"v":1,"type":"user_message","at":"2026-10-02T10:00:00Z","text":"one"}
{"v":1,"type":"model_responded","at":"2026-10-02T10:00:05Z","duration_ms":5000,"stop":"complete","usage":{"input":1000,"output":10}}
{"v":1,"type":"model_responded","at":"2026-10-02T10:00:09Z","duration_ms":3000,"stop":"complete","usage":{"input":1200,"output":10}}
{"v":1,"type":"user_message","at":"2026-10-02T10:00:10Z","text":"two"}
{"v":1,"type":"model_responded","at":"2026-10-02T10:00:15Z","duration_ms":4000,"stop":"complete","usage":{"input":1500,"output":10}}
`
	tl, err := bench.ParseUAH(strings.NewReader(stream), time.Date(2026, 10, 2, 10, 0, 0, 0, time.UTC), "")
	require.NoError(t, err)
	require.Len(t, tl.Requests, 3)
	assert.Equal(t, []int{1, 1, 2}, []int{tl.Requests[0].Turn, tl.Requests[1].Turn, tl.Requests[2].Turn})
	assert.Equal(t, 2, tl.Turns, "the primed workspace context is not a user turn")
}

// session is two user turns of a main agent at high with adaptive effort
// at one step: each turn's opener at high, its follow-ups at medium, every
// medium request reasoning 100 tokens, and a subagent request the replay
// leaves out.
func session() bench.RunRequests {
	req := func(turn int, effort string, input, cached, reasoning int64) bench.Request {
		return bench.Request{Turn: turn, Effort: effort, StartMS: input, EndMS: input + 1000, Tokens: bench.Tokens{Input: input, Cached: cached, Output: reasoning + 10, Reasoning: reasoning}}
	}

	return bench.RunRequests{
		Key: bench.Key{Task: "t", Harness: bench.HarnessUAH, Variant: "a", Effort: "high", Repeat: 1},
		Requests: []bench.Request{
			req(1, "high", 1280, 0, 300),
			req(1, "medium", 2560, 0, 100),
			{Agent: "sub", Effort: "medium", Tokens: bench.Tokens{Input: 9999, Output: 5}},
			req(1, "medium", 3840, 2560, 100),
			req(2, "high", 5120, 1280, 300),
			req(2, "medium", 6400, 3840, 100),
		},
	}
}

func TestTurnStats(t *testing.T) {
	ts := session().TurnStats(price)
	require.Len(t, ts, 2)
	assert.Equal(t, int64(1280), ts[0].Context)
	assert.Equal(t, int64(1280), ts[0].OpenerMiss)
	assert.Equal(t, int64(2560), ts[0].FollowMiss, "the first follow-up finds the medium cache empty")
	assert.Equal(t, 3, ts[0].Requests)
	assert.Equal(t, int64(5120-1280), ts[1].OpenerMiss, "the second opener re-sends the first turn's tool work")
	assert.Equal(t, int64(6400-3840), ts[1].FollowMiss)
	assert.Equal(t, int64(400), ts[1].Tokens.Reasoning)
}

func TestReplay(t *testing.T) {
	rr := session()
	rules := map[string]bench.Rule{}
	for _, r := range bench.Rules() {
		rules[r.Name] = r
	}
	cached := func(rule bench.Rule) int64 { return rr.Replay(rule, "high", "medium", 2).Cached }

	assert.Equal(t, int64(0+0+2560+1280+3840), cached(rr.Recorded("medium")), "the model reproduces the recorded run")
	assert.Equal(t, int64(0+0+2560+1280+3840), cached(rules["R0"]))
	assert.Equal(t, int64(0+1280+2560+3840+5120), cached(rules["off"]), "one effort: each request finds the one before it")
	assert.Equal(t, int64(0+0+2560+3840+5120), cached(rules["R0, later messages lowered too"]))
	assert.Equal(t, cached(rules["R0"]), cached(rules["R0, a message lowered when its miss > 8k"]), "a miss of 3.8k stays at high")

	off := rr.Replay(rules["off"], "high", "medium", 2)
	assert.Equal(t, int64(300+200+200+300+200), off.Reasoning, "medium's output doubles at high")
	assert.Equal(t, int64(310+220+220+310+220), off.Output)
	assert.Equal(t, int64(1280+2560+3840+5120+6400), off.Input, "the subagent is left out")
}

func TestReplayCompaction(t *testing.T) {
	rr := session()
	rr.Requests[0].Tokens.Cached = 128 // the cross-session prefix
	rr.Compactions = []bench.Compaction{{StartMS: 4000, EndMS: 4500}}
	off := bench.Rules()[0]
	// After the compaction, the opener of turn 2 finds only the prefix.
	assert.Equal(t, int64(128+1280+2560+128+5120), rr.Replay(off, "high", "medium", 2).Cached)
}

func TestTurnReport(t *testing.T) {
	ctl := session()
	ctl.Variant = ""
	for i := range ctl.Requests {
		ctl.Requests[i].Effort = "high"
	}
	runs := []bench.RunRequests{ctl, session()}
	path := filepath.Join(t.TempDir(), "x-requests.jsonl")
	require.NoError(t, bench.WriteRunRequests(path, runs))
	back, err := bench.ReadRunRequests(path)
	require.NoError(t, err)
	require.Equal(t, runs, back)

	md := bench.TurnReport(back, price)
	assert.Contains(t, md, "### t")
	assert.Contains(t, md, "## Replays of uah+a")
	assert.Contains(t, md, "| R0 below 64k context, off above |")
}
