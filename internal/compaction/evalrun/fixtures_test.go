package evalrun_test

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/viktordanov/uagent/core"

	"github.com/viktordanov/uah/internal/engine/embedded"
	"github.com/viktordanov/uah/internal/session"
	"github.com/viktordanov/uah/testing/fakellm"
	"github.com/viktordanov/uah/testing/harnesstest"
)

// update records the fixture sessions in testdata again:
//
//	go test ./internal/compaction/evalrun/ -run TestRecordFixtures -update
var update = flag.Bool("update", false, "record the fixture sessions in testdata")

// fixtureDir holds synthetic sessions the embedded engine recorded with
// fakellm: no real transcript.
const fixtureDir = "testdata/sessions"

// TestRecordFixtures records the fixture sessions: a build that fails and
// gets fixed with apply_patch, big reads, a skill, a /compact with a focus,
// and more work after it, with usage that crosses the evaluation's
// thresholds.
func TestRecordFixtures(t *testing.T) {
	if !*update {
		t.Skip("run with -update to record the fixtures")
	}
	require.NoError(t, os.RemoveAll(fixtureDir))
	require.NoError(t, os.MkdirAll(fixtureDir, 0o700))
	recordFixture(t, fixtureScript())
}

func bashReply(cmds ...string) fakellm.Reply { return fakellm.Reply{Commands: cmds} }

func patchReply(body string) fakellm.Reply {
	args, _ := json.Marshal(map[string]string{"input": "*** Begin Patch\n" + body + "\n*** End Patch"}) //nolint:errchkjson // strings encode

	return fakellm.Reply{Calls: []fakellm.Call{{Name: "apply_patch", Args: string(args)}}}
}

// big prints n bytes of a repeated line, as a long file or log does.
func big(name string, n int) string {
	return fmt.Sprintf("yes 'line of %s' | head -c %d", name, n)
}

type fixture struct {
	first, second []fakellm.Reply
	summary       string
}

func fixtureScript() fixture {
	first := []fakellm.Reply{
		bashReply("cat main.go docs/notes.md"),
		bashReply("echo 'main.go:3:2: undefined: helper' >&2; exit 1"),
		{Calls: []fakellm.Call{{Name: "SkillUse", Args: `{"name":"go-build"}`}}},
		patchReply("*** Update File: main.go\n@@\n package main\n \n-func main() { helper() }\n+func main() {}\n*** Add File: helper.go\n+package main\n+\n+func helper() {}"),
		bashReply("echo '--- FAIL: TestHelper (0.00s)' >&2; echo 'helper_test.go:9: want 2, got 1' >&2; exit 1"),
	}
	for i := range 8 {
		size := 700
		if i == 5 {
			size = 9000 // over 2,000 tokens
		}
		first = append(first, bashReply(big(fmt.Sprintf("pkg/part%d.go", i), size)+fmt.Sprintf(" # sed -n 1,400p pkg/part%d.go", i)))
	}
	first[len(first)-1].InputTokens = 60_000
	first = append(first, fakellm.Reply{Text: "The build passes; TestHelper still fails."})
	second := []fakellm.Reply{
		bashReply("cat main.go"),
		bashReply(big("pkg/part3.go", 700) + " # sed -n 1,400p pkg/part3.go"),
		bashReply("echo ok # go test ./..."),
	}
	for i := range 5 {
		second = append(second, bashReply(big(fmt.Sprintf("internal/x%d.go", i), 600)+fmt.Sprintf(" # cat internal/x%d.go", i)))
	}
	second[len(second)-1].InputTokens = 110_000
	second = append(second, fakellm.Reply{Text: "Done."})

	return fixture{first: first, second: second, summary: "Fixed the undefined helper in main.go by adding helper.go. TestHelper fails: want 2, got 1."}
}

func recordFixture(t *testing.T, f fixture) {
	t.Helper()
	env := harnesstest.NewEnv(t)
	require.NoError(t, os.WriteFile(filepath.Join(env.Workspace, "main.go"), []byte("package main\n\nfunc main() { helper() }\n"), 0o600))
	skill := filepath.Join(env.Workspace, ".agents", "skills", "go-build")
	require.NoError(t, os.MkdirAll(skill, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(skill, "SKILL.md"), []byte("---\nname: go-build\ndescription: Build Go code\n---\n\n"+strings.Repeat("Run go build, then go vet, then go test.\n", 60)), 0o600))
	replies := append(append(append([]fakellm.Reply{}, f.first...), fakellm.Reply{Text: f.summary}), f.second...)
	llm := fakellm.New(t, replies...)
	getenv := func(k string) string {
		switch k {
		case "OPENAI_API_KEY":
			return "test-key"
		case "SHELL":
			return "/bin/sh"
		}

		return env.Getenv(k)
	}
	eng := embedded.New(embedded.Config{StateDir: env.StateDir, Provider: "openai", Getenv: getenv})
	s, err := session.Open(context.Background(), eng, session.Options{Settings: session.Settings{
		Provider: "openai", Model: "gpt-test", Effort: "high", Workspace: env.Workspace, BaseURL: llm.URL,
	}})
	require.NoError(t, err)
	defer s.Close()
	say(t, s, "fix the build; keep the public API as it is")
	require.NoError(t, s.CompactWith("keep the failing test"))
	say(t, s, "now make TestHelper pass")
	sessions := filepath.Join(env.StateDir, "sessions")
	for _, suffix := range []string{".session.jsonl", ".compaction.jsonl"} {
		data, err := os.ReadFile(filepath.Join(sessions, s.ID()+suffix))
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(fixtureDir, s.ID()+suffix), data, 0o600))
	}
}

// say sends a message and waits for the session to be idle again.
func say(t *testing.T, s *session.Session, text string) {
	t.Helper()
	_, err := s.Submit(text)
	require.NoError(t, err)
	deadline := time.After(time.Minute)
	finished := false
	for {
		select {
		case e := <-s.Events():
			switch e.(type) {
			case core.RunFinished:
				finished = true
			case session.Idle:
				if finished {
					return
				}
			}
		case <-deadline:
			t.Fatal("timed out waiting for the run")
		}
	}
}
