// Command mcpserver is a stdio MCP server for tests, built with the official
// Go SDK. Its tools: echo, image, structured, fail, sleep, crash (the process
// exits), big (n bytes of text), stderr (writes the text to standard error),
// env (the named variable's value), overlap (sleeps and reports the most
// calls it saw running at once), and add_tool (adds a tool named by the
// text, so the server sends notifications/tools/list_changed).
// MCPSERVER_START_DELAY (a duration) delays its start; MCPSERVER_EXTRA_TOOLS
// (a count) adds that many tools named tool_0000 and up.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// png is a 1x1 PNG.
var png = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89" +
	"\x00\x00\x00\rIDATx\x9cc\xf8\x0f\x00\x00\x01\x01\x00\x05\x18\xd8N\x00\x00\x00\x00IEND\xaeB`\x82")

type args struct {
	Text string `json:"text"`
	Ms   int    `json:"ms"`
	N    int    `json:"n"`
}

// overlap counts calls to the overlap tool running at once.
var overlap struct {
	mu        sync.Mutex
	now, most int
}

func main() {
	if d, err := time.ParseDuration(os.Getenv("MCPSERVER_START_DELAY")); err == nil {
		time.Sleep(d)
	}
	s := sdk.NewServer(&sdk.Implementation{Name: "mcpserver", Version: "1"}, nil)
	add(s, "echo", "Echo the text.", func(a args) *sdk.CallToolResult {
		return text("echo: " + a.Text + " env=" + os.Getenv("MCPSERVER_GREETING"))
	})
	add(s, "image", "Return a 1x1 image.", func(args) *sdk.CallToolResult {
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "a pixel"}, &sdk.ImageContent{Data: png, MIMEType: "image/png"}}}
	})
	add(s, "structured", "Return structured content.", func(args) *sdk.CallToolResult {
		return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: "ignored"}}, StructuredContent: map[string]any{"n": 42}}
	})
	add(s, "fail", "Fail as a tool.", func(args) *sdk.CallToolResult {
		r := text("it failed")
		r.IsError = true

		return r
	})
	add(s, "sleep", "Sleep for ms milliseconds.", func(a args) *sdk.CallToolResult {
		time.Sleep(time.Duration(a.Ms) * time.Millisecond)

		return text("slept")
	})
	add(s, "crash", "Exit the server.", func(args) *sdk.CallToolResult {
		os.Exit(3)

		return nil
	})
	add(s, "big", "Return n bytes of text.", func(a args) *sdk.CallToolResult {
		return text(strings.Repeat("x", a.N))
	})
	add(s, "stderr", "Write the text to standard error.", func(a args) *sdk.CallToolResult {
		fmt.Fprintln(os.Stderr, a.Text)

		return text("logged")
	})
	add(s, "env", "Return the named environment variable.", func(a args) *sdk.CallToolResult {
		v, ok := os.LookupEnv(a.Text)
		if !ok {
			return text("unset")
		}

		return text(v)
	})
	add(s, "overlap", "Sleep ms milliseconds; report the most calls running at once.", func(a args) *sdk.CallToolResult {
		overlap.mu.Lock()
		overlap.now++
		overlap.most = max(overlap.most, overlap.now)
		overlap.mu.Unlock()
		time.Sleep(time.Duration(a.Ms) * time.Millisecond)
		overlap.mu.Lock()
		defer overlap.mu.Unlock()
		overlap.now--

		return text(strconv.Itoa(overlap.most))
	})
	add(s, "add_tool", "Add a tool named by the text.", func(a args) *sdk.CallToolResult {
		add(s, a.Text, "Added at run time.", func(args) *sdk.CallToolResult { return text("new") })

		return text("added")
	})
	extra, _ := strconv.Atoi(os.Getenv("MCPSERVER_EXTRA_TOOLS"))
	for i := range extra {
		add(s, fmt.Sprintf("tool_%04d", i), "An extra tool.", func(args) *sdk.CallToolResult { return text("extra") })
	}
	if err := s.Run(context.Background(), &sdk.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func add(s *sdk.Server, name, description string, fn func(args) *sdk.CallToolResult) {
	const typ = "type"
	schema := map[string]any{typ: "object", "properties": map[string]any{
		"text": map[string]any{typ: "string"}, "ms": map[string]any{typ: "integer"}, "n": map[string]any{typ: "integer"},
	}}
	// Read-only, so approval_mode auto runs them without asking, as Codex does.
	annotations := &sdk.ToolAnnotations{ReadOnlyHint: true}
	s.AddTool(&sdk.Tool{Name: name, Description: description, InputSchema: schema, Annotations: annotations}, func(_ context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		var a args
		if len(req.Params.Arguments) > 0 {
			if err := json.Unmarshal(req.Params.Arguments, &a); err != nil {
				return nil, fmt.Errorf("bad arguments: %w", err)
			}
		}

		return fn(a), nil
	})
}

func text(s string) *sdk.CallToolResult {
	return &sdk.CallToolResult{Content: []sdk.Content{&sdk.TextContent{Text: s}}}
}
