// Package llmcall makes one model call outside the agent loop: items in, text
// out. It runs over any runner llm.Adapter, so every provider the embedded
// engine supports works, and each call picks its model, effort, and timeout.
// Compaction summaries use it, and so can other one-shot calls such as a
// command reviewer.
package llmcall

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/unreallabsai/unreal-agent/harness/llm"
)

// DefaultTimeout bounds a call whose Request sets no timeout.
const DefaultTimeout = 5 * time.Minute

// ErrNoText means the model answered without any text.
var ErrNoText = errors.New("the model answered without text")

// Request is one call.
type Request struct {
	// Model is the model ID; the adapter may fill in its own when empty.
	Model  string
	Effort llm.ReasoningEffort
	// Instructions, when set, goes first as the system message.
	Instructions string
	// Input is the conversation: messages, and optionally tool calls,
	// results, and reasoning from an earlier exchange.
	Input []llm.Item
	// Timeout bounds the call (DefaultTimeout when zero).
	Timeout time.Duration
	// CacheKey is passed to the provider for prompt caching.
	CacheKey string
}

// Result is the model's answer.
type Result struct {
	// Text is the assistant's messages, joined by blank lines.
	Text  string
	Usage llm.Usage
}

// Call sends the request without tools and returns the assistant's text.
func Call(ctx context.Context, adapter llm.Adapter, req Request) (Result, error) {
	timeout := req.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	input := make([]llm.Item, 0, len(req.Input)+1)
	if req.Instructions != "" {
		input = append(input, Message(llm.RoleSystem, req.Instructions))
	}
	input = append(input, req.Input...)
	resp, err := adapter.Respond(ctx, llm.Request{
		Model: llm.Model{ID: req.Model, ReasoningEffort: req.Effort},
		Input: input,
	}, llm.RequestOptions{CacheKey: req.CacheKey})
	if err != nil {
		return Result{}, fmt.Errorf("failed to call the model: %w", err)
	}
	if resp.Failure != nil {
		return Result{}, fmt.Errorf("failed to call the model: %s: %s", resp.Failure.Code, resp.Failure.Message)
	}
	text := Text(resp)
	if text == "" {
		return Result{Usage: resp.Usage}, ErrNoText
	}

	return Result{Text: text, Usage: resp.Usage}, nil
}

// Text joins the assistant messages of a response.
func Text(resp llm.Response) string {
	var parts []string
	for _, item := range resp.Output {
		if m, ok := item.Data.(llm.Message); ok && m.Role == llm.RoleAssistant && strings.TrimSpace(m.Text) != "" {
			parts = append(parts, strings.TrimSpace(m.Text))
		}
	}

	return strings.Join(parts, "\n\n")
}

// Message is a message item.
func Message(role llm.Role, text string) llm.Item {
	return llm.Item{Type: llm.ItemMessage, Data: llm.Message{Role: role, Text: text}}
}
