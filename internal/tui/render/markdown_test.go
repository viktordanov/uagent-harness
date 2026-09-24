package render

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
)

func TestMarkdownLines(t *testing.T) {
	text := "# Plan\n\nRun `go test` and **check** the *output*:\n\n```go\nfunc main() {}\n```\n\n- first item\n  - nested\n1. numbered\n> quoted\n---\nSee [the docs](https://example.com)."
	got := amber.markdownLines(text, 60, "● ", "  ")
	plain := make([]string, len(got))
	for i, l := range got {
		plain[i] = strings.TrimRight(ansi.Strip(l), " ")
	}
	assert.Equal(t, []string{
		"● Plan",
		"",
		"  Run go test and check the output:",
		"",
		"   func main() {}",
		"",
		"  • first item",
		"    • nested",
		"  1. numbered",
		"  │ quoted",
		"  ────────────────────────────────────────",
		"  See the docs (https://example.com).",
	}, plain)
	assert.NotEqual(t, got[4], "   func main() {}", "code is highlighted")
}

func TestMarkdownKeepsSnakeCaseAndStars(t *testing.T) {
	got := ansi.Strip(strings.Join(amber.markdownLines("use snake_case_names and 2 * 3 * 4", 80, "", ""), "\n"))
	assert.Equal(t, "use snake_case_names and 2 * 3 * 4", got)
}

func TestMarkdownWrapsListItemsWithAHangingIndent(t *testing.T) {
	got := amber.markdownLines("- "+strings.Repeat("word ", 12), 30, "", "")
	assert.Greater(t, len(got), 1)
	assert.True(t, strings.HasPrefix(ansi.Strip(got[1]), "  word"), ansi.Strip(got[1]))
}
