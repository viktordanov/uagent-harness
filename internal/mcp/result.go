package mcp

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Result is a tool call's output for the model.
type Result struct {
	Text string
	// Images are data: URLs.
	Images []string
	// IsError is the server's isError: the tool ran and failed.
	IsError bool
}

// convert turns a CallToolResult into text and images as Codex does:
// structured content, when present, replaces the content as JSON text.
func convert(r *sdk.CallToolResult) Result {
	out := Result{IsError: r.IsError}
	if r.StructuredContent != nil {
		b, err := json.Marshal(r.StructuredContent)
		if err != nil {
			return Result{Text: fmt.Sprintf("failed to encode the structured content: %v", err), IsError: true}
		}
		out.Text = string(b)

		return out
	}
	var texts []string
	for _, c := range r.Content {
		switch c := c.(type) {
		case *sdk.TextContent:
			texts = append(texts, c.Text)
		case *sdk.ImageContent:
			out.Images = append(out.Images, dataURL(c.MIMEType, c.Data))
		case *sdk.AudioContent:
			texts = append(texts, fmt.Sprintf("[%s audio omitted]", c.MIMEType))
		case *sdk.ResourceLink:
			texts = append(texts, fmt.Sprintf("[resource %s: %s]", c.Name, c.URI))
		case *sdk.EmbeddedResource:
			texts = append(texts, embedded(c.Resource))
		}
	}
	out.Text = strings.Join(texts, "\n")

	return out
}

func dataURL(mime string, data []byte) string {
	if mime == "" {
		mime = "application/octet-stream"
	}

	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data)
}

func embedded(r *sdk.ResourceContents) string {
	if r == nil {
		return ""
	}
	if r.Text != "" {
		return r.Text
	}

	return fmt.Sprintf("[resource %s (%s) omitted]", r.URI, r.MIMEType)
}
