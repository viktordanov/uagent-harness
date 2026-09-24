// Package contextusage breaks a model request into what fills the context
// window, as Claude Code's /context does: the system prompt, instruction
// files, skills, tools, MCP tools, and the conversation, with the free space
// and the auto-compaction buffer. It is pure: callers pass the request the
// engine last sent and the tokens the provider reported for it.
package contextusage

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"

	"github.com/unreallabsai/unreal-agent/harness/llm"
)

// Category names, in the order /context lists them.
const (
	SystemPrompt = "System prompt"
	Instructions = "Instructions"
	Skills       = "Skills"
	Tools        = "Tools"
	MCPTools     = "MCP tools"
	UserMessages = "Your messages"
	Assistant    = "Agent messages"
	ToolResults  = "Tool calls and results"
)

// Order lists the categories in /context's order.
var Order = []string{SystemPrompt, Instructions, Skills, Tools, MCPTools, UserMessages, Assistant, ToolResults}

// Item is one named part of a category, such as a tool or a file.
type Item struct {
	Name   string
	Tokens int64
}

// Category is a share of the context.
type Category struct {
	Name   string
	Tokens int64
	Items  []Item
}

// Usage is the context of the last request.
type Usage struct {
	Model  string
	Window int64
	// Used is the input tokens the provider reported, or the estimate.
	Used       int64
	Categories []Category
	// Buffer is the space auto-compaction keeps free (0 when it is off).
	Buffer int64
	// Estimated means the provider reported no token count, so Used and
	// every category are estimates.
	Estimated bool
}

// Free is the space left before the window, less the buffer.
func (u Usage) Free() int64 { return max(u.Window-u.Used-u.Buffer, 0) }

// Markers the runner's context builder and uah's host prompt put in the
// system message.
const (
	skillsMarker       = "The following skills provide specialized instructions"
	instructionsMarker = "\n# Project instructions\n"
)

var skillTag = regexp.MustCompile(`(?s)<skill><name>(.*?)</name>.*?</skill>`)

// Analyze splits the request into categories. reported is the provider's
// input token count for it (0 when unknown); estimates are scaled to it so
// the categories add up to what the provider counted. autoLimit is the
// tokens at which automatic compaction starts (0: off); the rest of the
// window is the buffer. files are the instruction files the host
// prompt holds, in order, as "## <path>" headers name them.
func Analyze(req llm.Request, reported, window, autoLimit int64, files []string) Usage {
	cats := map[string]*Category{}
	add := func(cat, item, text string) {
		c, ok := cats[cat]
		if !ok {
			c = &Category{Name: cat}
			cats[cat] = c
		}
		n := estimate(text)
		c.Tokens += n
		if item != "" {
			c.Items = append(c.Items, Item{Name: item, Tokens: n})
		}
	}
	for _, it := range req.Input {
		addItem(add, it, files)
	}
	for _, t := range req.Tools {
		// A schema that does not encode counts as empty: this is an estimate.
		schema, err := json.Marshal(t.Parameters)
		if err != nil {
			schema = nil
		}
		cat := Tools
		if strings.HasPrefix(t.Name, "mcp__") {
			cat = MCPTools
		}
		add(cat, t.Name, t.Name+t.Description+string(schema))
	}
	u := Usage{Model: req.Model.ID, Window: window}
	for _, name := range Order {
		if c, ok := cats[name]; ok {
			u.Categories = append(u.Categories, *c)
		}
	}
	u.Used, u.Estimated = scale(u.Categories, reported)
	if autoLimit > 0 && window > 0 {
		u.Buffer = max(window-autoLimit, 0)
	}

	return u
}

func addItem(add func(cat, item, text string), it llm.Item, files []string) {
	switch d := it.Data.(type) {
	case llm.Message:
		switch d.Role {
		case llm.RoleSystem:
			splitSystem(add, d.Text, files)
		case llm.RoleUser:
			add(UserMessages, "", d.Text)
		case llm.RoleAssistant:
			add(Assistant, "", d.Text)
		}
	case llm.ToolCall:
		add(ToolResults, "", d.Name+d.Arguments)
	case llm.ToolResult:
		var b strings.Builder
		for _, o := range d.Output {
			b.WriteString(o.Value)
		}
		add(ToolResults, "", b.String())
	case llm.Reasoning:
		add(Assistant, "", strings.Join(d.Summary, "")+string(d.Raw))
	}
}

// splitSystem splits the system message into the host prompt, the skills
// block (one item per skill), and the instruction files (one item each,
// found by the "## <path>" headers uah wrote, so a file's own headings do not
// split it).
func splitSystem(add func(cat, item, text string), text string, files []string) {
	host, instructions, _ := strings.Cut(text, instructionsMarker)
	if i := strings.Index(host, skillsMarker); i >= 0 {
		skills := host[i:]
		host = host[:i]
		found := skillTag.FindAllStringSubmatch(skills, -1)
		for _, m := range found {
			add(Skills, m[1], m[0])
		}
		if len(found) == 0 {
			add(Skills, "", skills)
		}
	}
	add(SystemPrompt, "", host)
	rest := instructions
	for i, f := range files {
		start := strings.Index(rest, "## "+f+"\n")
		if start < 0 {
			continue
		}
		add(SystemPrompt, "", rest[:start]) // the introduction before the first file
		body := rest[start:]
		end := len(body)
		if i+1 < len(files) {
			if next := strings.Index(body, "\n## "+files[i+1]+"\n"); next >= 0 {
				end = next
			}
		}
		add(Instructions, f, body[:end])
		rest = body[end:]
	}
	if len(files) == 0 {
		add(Instructions, "", instructions)
	}
}

// estimate is Codex's approximation: about four bytes per token.
func estimate(text string) int64 { return int64((len(text) + 3) / 4) }

// scale makes the categories add up to the reported count, keeping their
// proportions; it returns the total and whether it is only an estimate.
func scale(cats []Category, reported int64) (total int64, estimated bool) {
	var sum int64
	for _, c := range cats {
		sum += c.Tokens
	}
	for i := range cats {
		slices.SortStableFunc(cats[i].Items, func(a, b Item) int { return int(b.Tokens - a.Tokens) })
	}
	if reported <= 0 || sum == 0 {
		return sum, true
	}
	var scaled int64
	largest := 0
	for i := range cats {
		if cats[i].Tokens > cats[largest].Tokens {
			largest = i
		}
		cats[i].Tokens = cats[i].Tokens * reported / sum
		scaled += cats[i].Tokens
		for j := range cats[i].Items {
			cats[i].Items[j].Tokens = cats[i].Items[j].Tokens * reported / sum
		}
	}
	// Rounding down loses a few tokens; the largest category takes them.
	cats[largest].Tokens += reported - scaled

	return reported, false
}
