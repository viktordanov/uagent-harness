package mcp

import (
	"crypto/sha1" //nolint:gosec // a name suffix, as Codex's, not security
	"encoding/hex"
	"fmt"
	"strings"
)

// Prefix starts every MCP tool name the model sees.
const Prefix = "mcp__"

// MaxNameLength bounds a qualified name. Codex allows 128 bytes because it
// sends MCP tools in Responses API namespaces; uah sends flat function
// names, which the API limits to 64 characters.
const MaxNameLength = 64

const hashLength = 12

// Sanitize replaces every character outside [A-Za-z0-9_] with "_", as
// Codex's sanitize_responses_api_tool_name does.
func Sanitize(name string) string {
	var b strings.Builder
	for _, c := range name {
		if c < 128 && (c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
			b.WriteRune(c)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "_"
	}

	return b.String()
}

// namer hands out unique qualified names, mcp__<server>__<tool>. A name that
// is too long or already taken keeps its head and gets "_" and 12 hex digits
// of a SHA-1 of the raw server and tool names, as in Codex.
type namer struct{ used map[string]bool }

func newNamer() *namer { return &namer{used: map[string]bool{}} }

func (n *namer) name(server, tool string) string {
	base := Prefix + Sanitize(server) + "__" + Sanitize(tool)
	if len(base) <= MaxNameLength && !n.used[base] {
		n.used[base] = true

		return base
	}
	identity := server + "\x00" + tool
	for attempt := 0; ; attempt++ {
		input := identity
		if attempt > 0 {
			input = fmt.Sprintf("%s\x00%d", identity, attempt)
		}
		sum := sha1.Sum([]byte(input)) //nolint:gosec // see the import
		suffix := "_" + hex.EncodeToString(sum[:])[:hashLength]
		name := base[:min(len(base), MaxNameLength-len(suffix))] + suffix
		if !n.used[name] {
			n.used[name] = true

			return name
		}
	}
}
