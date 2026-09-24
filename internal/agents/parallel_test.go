package agents_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent-harness/internal/agents"
	"github.com/viktordanov/uagent-harness/testing/fakellm"
)

// childRequests counts the requests whose user messages start with prefix.
func childRequests(e *env, prefix string) int {
	n := 0
	for _, r := range e.llm.Requests() {
		if slices.ContainsFunc(r.UserTexts, func(u string) bool { return strings.HasPrefix(u, prefix) }) {
			n++
		}
	}

	return n
}

// awaitRequests blocks until the fake model has seen n requests whose user
// messages start with prefix, waking on each request, never polling.
func awaitRequests(t *testing.T, e *env, prefix string, n int) {
	t.Helper()
	deadline := time.After(waitTimeout)
	for childRequests(e, prefix) < n {
		select {
		case <-e.llm.Seen():
		case <-deadline:
			t.Fatalf("saw %d of %d %s requests", childRequests(e, prefix), n, prefix)
		}
	}
}

// TestAgents_ParallelCalls runs the agent calls of one model turn at the
// same time, each as its own remote job: three spawns start three
// children that all ask the model before any answers, and of two waits in
// one turn, the second returns while the first still blocks.
func TestAgents_ParallelCalls(t *testing.T) {
	gates := map[string]chan struct{}{"CHILD-P0": make(chan struct{}), "CHILD-P1": make(chan struct{}), "CHILD-P2": make(chan struct{})}
	waited := make(chan []string, 1)
	var early string
	e := newEnv(t, agents.Config{MaxThreads: 3},
		fakellm.Reply{Calls: []fakellm.Call{
			call("spawn_agent", `{"message":"CHILD-P0 one"}`),
			call("spawn_agent", `{"message":"CHILD-P1 two"}`),
			call("spawn_agent", `{"message":"CHILD-P2 three"}`),
		}},
		fakellm.Reply{From: func(req fakellm.Request) fakellm.Reply {
			got := ids(req)
			waited <- got[:2]

			return fakellm.Reply{Calls: []fakellm.Call{
				call("wait_agent", `{"targets":["`+got[0]+`"],"timeout_ms":600000}`),
				call("wait_agent", `{"targets":["`+got[1]+`"],"timeout_ms":600000}`),
			}}
		}},
		fakellm.Reply{From: func(req fakellm.Request) fakellm.Reply {
			early = strings.Join(req.ToolOutputs, "\n")

			return fakellm.Reply{Text: "one wait came back"}
		}},
		fakellm.Reply{Text: "both came back"}, // the first wait's result starts one more turn
	)
	for prefix, g := range gates {
		e.llm.Route(prefix, fakellm.Reply{Gate: g, Text: "answer from " + prefix})
	}
	s, ev := e.open(t, false)
	_, err := s.Submit("fan out")
	require.NoError(t, err)

	awaitRequests(t, e, "CHILD-P", 3) // every child asks while all are held
	targets := <-waited
	first, second := taskPrefix(t, e, targets[0]), taskPrefix(t, e, targets[1])
	close(gates[second]) // the second wait's child answers first
	awaitRequests(t, e, "fan out", 3)
	close(gates[first])
	assert.Equal(t, "both came back", ev.finished().Answer)
	for prefix, g := range gates {
		if prefix != first && prefix != second {
			close(g)
		}
	}

	assert.Contains(t, early, `{"completed":"answer from `+second+`"}`, "the second wait returned")
	assert.Contains(t, early, "Tool call is still running", "while the first still waited: the calls do not queue behind each other")
}

// taskPrefix is the CHILD-Pn prefix of the child's task, from its agent
// record.
func taskPrefix(t *testing.T, e *env, id string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(e.sessionsDir(), id+".agent.json"))
	require.NoError(t, err)
	var rec struct {
		Task string `json:"task"`
	}
	require.NoError(t, json.Unmarshal(data, &rec))
	prefix, _, _ := strings.Cut(rec.Task, " ")

	return prefix
}
