// Package compaction rewrites a model request the way Codex compacts a
// thread: the user messages stay verbatim and in order (the newest 20,000
// tokens of them), and everything else before a point is replaced by one
// summary message. It holds the rewrite, the summary call over any
// llm.Adapter, the token estimates, and the compaction log; the embedded
// engine decides when to compact and emits the events. See README.md.
//
// The prompts in prompts/ are Codex's (rust-v0.156.1,
// codex-rs/prompts/templates/compact), Apache License 2.0, Copyright 2025
// OpenAI; see prompts/LICENSE-codex.
package compaction

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/unreallabsai/unreal-agent/harness/llm"
)

var (
	//go:embed prompts/prompt.md
	promptFile string
	//go:embed prompts/summary_prefix.md
	prefixFile string
)

// Prompt asks the model for the handoff summary.
var Prompt = strings.TrimSpace(promptFile)

// SummaryPrefix starts the message that carries the summary.
var SummaryPrefix = strings.TrimSpace(prefixFile)

// Trigger says what started a compaction.
type Trigger string

const (
	TriggerManual Trigger = "manual"
	TriggerAuto   Trigger = "auto"
	// TriggerClear is /clear: the covered items are dropped with no summary,
	// so the model starts fresh in the same session.
	TriggerClear Trigger = "clear"
)

// Verb names what the trigger does, for messages: "compaction", or "clear".
func (t Trigger) Verb() string {
	if t == TriggerClear {
		return "clear"
	}

	return "compaction"
}

// ErrMismatch means the request's history is not the one the record covers.
var ErrMismatch = errors.New("the history does not match the compaction")

// Record is one compaction. It covers the first Covered items after the
// system message of every later request the context builder produces.
type Record struct {
	Covered int `json:"covered"`
	// Floor is how many of the covered items a /clear dropped: they are
	// left out entirely, with none of their user messages kept. A clear's
	// Floor is its Covered; a later compaction carries the floor forward.
	Floor   int       `json:"floor,omitempty"`
	Hash    string    `json:"hash"`
	Summary string    `json:"summary"`
	Trigger Trigger   `json:"trigger"`
	Model   string    `json:"model,omitempty"`
	At      time.Time `json:"at"`
}

// Hash fingerprints items, so a record applies only to the history it covers.
func Hash(items []llm.Item) (string, error) {
	sum := sha256.New()
	for _, item := range items {
		b, err := json.Marshal(item)
		if err != nil {
			return "", fmt.Errorf("failed to encode a history item: %w", err)
		}
		_, _ = sum.Write(b)
		_, _ = sum.Write([]byte{'\n'})
	}

	return hex.EncodeToString(sum.Sum(nil)), nil
}

// Coverable is how many items after input's system message a compaction
// covers: all of them except the user messages at the end, which are new
// input that goes after the summary, as Codex compacts before recording it.
func Coverable(input []llm.Item) int {
	n := len(input) - 1
	for n > 0 && IsUserMessage(input[n]) {
		n--
	}

	return max(n, 0)
}

// NewRecord covers the Coverable items of input with the summary.
func NewRecord(input []llm.Item, summary string, trigger Trigger, model string, at time.Time) (Record, error) {
	covered := Coverable(input)
	hash, err := Hash(input[min(1, len(input)) : 1+covered])
	if err != nil {
		return Record{}, err
	}

	return Record{Covered: covered, Hash: hash, Summary: summary, Trigger: trigger, Model: model, At: at}, nil
}

// Apply rewrites input, whose first item is the system message, with the
// record: the system message, the covered user messages that Kept keeps,
// the summary, and the items after the covered ones. A tool result whose
// call was covered becomes a user-role note, so no output lacks its call.
func Apply(input []llm.Item, rec Record) ([]llm.Item, error) {
	if rec.Covered <= 0 || len(input) == 0 {
		return input, nil
	}
	if len(input) < 1+rec.Covered {
		return nil, fmt.Errorf("%w: it covers %d items, the history has %d", ErrMismatch, rec.Covered, len(input)-1)
	}
	covered, tail := input[1:1+rec.Covered], input[1+rec.Covered:]
	hash, err := Hash(covered)
	if err != nil {
		return nil, err
	}
	if hash != rec.Hash {
		return nil, ErrMismatch
	}
	kept := Kept(covered[min(rec.Floor, len(covered)):], UserMessageMaxTokens)
	out := make([]llm.Item, 0, 2+len(kept)+len(tail))
	out = append(out, input[0])
	out = append(out, kept...)
	if rec.Floor < rec.Covered {
		out = append(out, SummaryMessage(rec.Summary))
	}

	return append(out, detachOrphans(tail)...), nil
}

// NewClear is a /clear over input: every coverable item is dropped.
func NewClear(input []llm.Item, at time.Time) (Record, error) {
	rec, err := NewRecord(input, "", TriggerClear, "", at)
	rec.Floor = rec.Covered

	return rec, err
}

// IsUserMessage reports whether the item is a user message.
func IsUserMessage(item llm.Item) bool {
	m, ok := item.Data.(llm.Message)

	return ok && item.Type == llm.ItemMessage && m.Role == llm.RoleUser
}

// SummaryMessage is the user message that carries a summary, as in Codex.
func SummaryMessage(summary string) llm.Item {
	if strings.TrimSpace(summary) == "" {
		summary = "(no summary available)"
	}

	return llm.Item{Type: llm.ItemMessage, Data: llm.Message{Role: llm.RoleUser, Text: SummaryPrefix + "\n" + summary}}
}

// detachOrphans turns each tool result whose call is not among items into a
// user-role note. items is not changed.
func detachOrphans(items []llm.Item) []llm.Item {
	calls := map[string]bool{}
	for _, item := range items {
		if c, ok := item.Data.(llm.ToolCall); ok {
			calls[c.CallID] = true
		}
	}
	out := make([]llm.Item, 0, len(items))
	for _, item := range items {
		if r, ok := item.Data.(llm.ToolResult); ok && !calls[r.CallID] {
			item = orphanNote(r)
		}
		out = append(out, item)
	}

	return out
}

// orphanNote carries the output of a tool call that the summary covers. A
// message carries text only, so an image is named, not sent.
func orphanNote(r llm.ToolResult) llm.Item {
	var text []string
	for _, o := range r.Output {
		switch o.Kind {
		case llm.ToolResultText:
			text = append(text, o.Value)
		case llm.ToolResultImage:
			text = append(text, "[image omitted]")
		}
	}

	return llm.Item{Type: llm.ItemMessage, Data: llm.Message{
		Role: llm.RoleUser,
		Text: fmt.Sprintf("Output of the earlier tool call %s, which the summary covers:\n%s", r.CallID, strings.Join(text, "\n")),
	}}
}
