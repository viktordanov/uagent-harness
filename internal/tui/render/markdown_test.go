package render

import (
	"image/color"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMarkdownLines(t *testing.T) {
	text := "# Plan\n\nRun `go test` and **check** the *output*:\n\n```go\nfunc main() {}\n```\n\n- first item\n  - nested\n1. numbered\n> quoted\n---\nSee [the docs](https://example.com)."
	got := amber.markdownLines(text, 60, "● ", "  ")
	plain := make([]string, len(got))
	for i, l := range got {
		plain[i] = strings.TrimRight(ansi.Strip(l), " ")
	}
	assert.Equal(t, []string{
		"● PLAN",
		"",
		"  Run go test and check the output:",
		"",
		"   func main() {}                                        go",
		"",
		"  • first item",
		"    ◦ nested",
		"",
		"  1. numbered",
		"",
		"  “ quoted ”",
		"",
		"  ────────────────────────────────────────",
		"",
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

func TestMarkdownFollowsTheTheme(t *testing.T) {
	code := "```go\nfunc main() {}\n```"
	dark, light := NewStyles(Amber).markdownLines(code, 40, "", ""), NewStyles(AmberLight).markdownLines(code, 40, "", "")
	assert.Equal(t, ansi.Strip(dark[0]), ansi.Strip(light[0]))
	assert.NotEqual(t, dark[0], light[0], "each theme's cache has its own colors")
}

func TestMarkdownStylesFollowTheTheme(t *testing.T) {
	src := "## Results\n\n| a | b |\n| - | - |\n| 1 | 2 |\n\n```diff\n-old\n+new\n```\n\n> [!NOTE]\n> n\n\n> [!TIP]\n> t\n\n> [!IMPORTANT]\n> i\n\n> [!WARNING]\n> w\n\n> [!CAUTION]\n> c"
	for _, theme := range []Theme{Amber, AmberLight} {
		st := NewStyles(theme)
		fg := func(c color.Color) string { return "38" + strings.TrimSuffix(backgroundOn(c)[4:], "m") + "m" }
		got := st.markdownLines(src, 40, "", "")
		plain := make([]string, len(got))
		for i, l := range got {
			plain[i] = strings.TrimRight(ansi.Strip(l), " ")
		}
		line := func(s string) string {
			i := slices.Index(plain, s)
			require.GreaterOrEqual(t, i, 0, "%q in %q", s, plain)

			return got[i]
		}
		assert.Contains(t, line("Results"), fg(theme.Accent), "H2 in the accent")
		assert.Contains(t, line(" a      b"), fg(theme.Accent), "the table header in the accent")
		assert.True(t, strings.HasPrefix(line(" 1      2"), st.bandOn), "the first row on the band")
		assert.True(t, strings.HasPrefix(line(" -old                              diff"), st.delOn), "a removed line on DiffDel")
		assert.True(t, strings.HasPrefix(line(" +new"), st.addOn), "an added line on DiffAdd")
		for title, c := range map[string]color.Color{
			"! Note": theme.Info, "! Tip": theme.Good, "! Important": theme.Extra, "! Warning": theme.Warn, "! Caution": theme.Bad,
		} {
			assert.Contains(t, line(title), fg(c), title)
		}
	}
}
