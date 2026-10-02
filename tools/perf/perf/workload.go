package perf

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"

	"github.com/viktordanov/uah/testing/fakellm"
)

// The workload is what one user turn of a coding session looks like to
// uah: reasoning summaries, streamed commentary, shell commands that read
// source files and a long log, an apply_patch, and a final answer in
// markdown. Fixtures repeat it; the active-turn scenario runs it live.

// workspaceFiles are the files the turn's commands read, by path, with
// their sizes in bytes.
var workspaceFiles = []struct {
	path string
	size int
}{
	{"internal/server/handler.go", 18_000},
	{"internal/server/routes.go", 9_000},
	{"internal/store/query.go", 24_000},
	{"cmd/app/main.go", 4_000},
	{"logs/test-output.log", 64_000},
}

// fillWorkspace writes the workspace files: Go-like source with comments,
// and a test log, from a fixed seed so every run reads the same bytes.
func fillWorkspace(dir string) error {
	rng := rand.New(rand.NewPCG(1, 2)) // fixture text, not security
	for _, f := range workspaceFiles {
		path := filepath.Join(dir, f.path)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return fmt.Errorf("failed to make %s: %w", filepath.Dir(path), err)
		}
		text := sourceText(rng, f.size)
		if strings.HasSuffix(f.path, ".log") {
			text = logText(rng, f.size)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			return fmt.Errorf("failed to write %s: %w", path, err)
		}
	}

	return nil
}

var words = strings.Fields("request handler store query session context error result value index item page cache " +
	"token stream event record batch window buffer config option client server route path file line count size")

// sourceText is about n bytes of Go-like code.
func sourceText(rng *rand.Rand, n int) string {
	var b strings.Builder
	b.WriteString("package server\n\n")
	for i := 0; b.Len() < n; i++ {
		w1, w2 := words[rng.IntN(len(words))], words[rng.IntN(len(words))]
		fmt.Fprintf(&b, "// %s%d returns the %s of the %s, or an error when the %s is missing.\n", strings.ToUpper(w1[:1])+w1[1:], i, w2, w1, w2)
		fmt.Fprintf(&b, "func %s%d(ctx context.Context, %s string) (%s, error) {\n", w1, i, w2, "int")
		fmt.Fprintf(&b, "\tif %s == \"\" {\n\t\treturn 0, fmt.Errorf(\"missing %s: %%w\", errMissing)\n\t}\n", w2, w2)
		fmt.Fprintf(&b, "\treturn len(%s) * %d, nil\n}\n\n", w2, rng.IntN(1000))
	}

	return b.String()
}

// logText is about n bytes of a go test -v log.
func logText(rng *rand.Rand, n int) string {
	var b strings.Builder
	for i := 0; b.Len() < n; i++ {
		w := words[rng.IntN(len(words))]
		fmt.Fprintf(&b, "=== RUN   Test%s_%d\n--- PASS: Test%s_%d (0.%02ds)\n", strings.ToUpper(w[:1])+w[1:], i, strings.ToUpper(w[:1])+w[1:], i, rng.IntN(100))
		if i%17 == 0 {
			fmt.Fprintf(&b, "    %s_test.go:%d: %s %s took longer than expected\n", w, rng.IntN(500), w, words[rng.IntN(len(words))])
		}
	}

	return b.String()
}

// patchCall is an apply_patch that adds a notes file.
func patchCall(name string) fakellm.Call {
	body := "*** Begin Patch\n*** Add File: notes/" + name + ".md\n+# Notes\n+\n+The handler validates the request before the store query.\n*** End Patch"
	args, err := json.Marshal(map[string]string{"input": body})
	if err != nil {
		panic(err) // strings always encode
	}

	return fakellm.Call{Name: "apply_patch", Args: string(args)}
}

// turnReplies are the model's replies for one turn; name keeps the
// patched file unique per turn.
func turnReplies(name string) []fakellm.Reply {
	return []fakellm.Reply{
		{
			Reasoning: []string{"**Inspecting the handler**\n\nThe request path goes through the router first, ", "then the store query."},
			Deltas:    []string{"I'll read the handler, ", "the routes, and the store query ", "before changing anything."},
			Commands: []string{
				"cat internal/server/handler.go", "cat internal/server/routes.go",
				"cat internal/store/query.go", "ls -la internal/server cmd/app",
			},
		},
		{
			Reasoning: []string{"**Planning the change**\n\nA note records what the handler checks."},
			Deltas:    []string{"Writing a note about the validation order."},
			Calls:     []fakellm.Call{patchCall(name)},
		},
		{
			Deltas:   []string{"Checking the latest test log."},
			Commands: []string{"cat logs/test-output.log", "cat cmd/app/main.go"},
		},
		{
			Reasoning: []string{"**Summarizing**\n\nThe tests pass; the note is in place."},
			Deltas: []string{
				"## Summary\n\nThe handler validates the request ", "before it queries the store:\n\n",
				"- `handler.go` checks the session and the token;\n", "- `query.go` pages the items, 256 at a time.\n\n",
				"```go\nfunc handle(ctx context.Context, r *Request) error {\n\treturn validate(r)\n}\n```\n\n",
				"I added `notes/" + name + ".md`. The test log shows every test passing.",
			},
		},
	}
}
