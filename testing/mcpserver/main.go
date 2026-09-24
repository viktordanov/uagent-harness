// Command mcpserver is a stdio MCP server for tests, built with the official
// Go SDK. Its tools: echo, image, structured, fail, sleep, and crash (the
// process exits). MCPSERVER_START_DELAY (a duration) delays its start.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// png is a 1x1 PNG.
var png = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89" +
	"\x00\x00\x00\rIDATx\x9cc\xf8\x0f\x00\x00\x01\x01\x00\x05\x18\xd8N\x00\x00\x00\x00IEND\xaeB`\x82")

type args struct {
	Text string `json:"text"`
	Ms   int    `json:"ms"`
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
	if err := s.Run(context.Background(), &sdk.StdioTransport{}); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func add(s *sdk.Server, name, description string, fn func(args) *sdk.CallToolResult) {
	const typ = "type"
	schema := map[string]any{typ: "object", "properties": map[string]any{
		"text": map[string]any{typ: "string"}, "ms": map[string]any{typ: "integer"},
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
