package markdown

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var update = flag.Bool("update", false, "rewrite golden files")

// tag is a style drawn with an SGR code of its own, which goldens show as
// <name>…</>: widths, wrapping, and resets stay those of real styles.
type tag struct{ name, code string }

func (t tag) Render(s ...string) string {
	return "\x1b[" + t.code + "m" + strings.Join(s, " ") + "\x1b[m"
}

var (
	tagged = []tag{{"b", "1"}, {"i", "3"}, {"s", "9"}, {"c", "36"}, {"d", "2"}, {"h", "4"}, {"th", "7"}, {"strong", "21"}}
	shows  = func() *strings.Replacer {
		pairs := []string{"\x1b[m", "</>"}
		for _, t := range tagged {
			pairs = append(pairs, "\x1b["+t.code+"m", "<"+t.name+">")
		}

		return strings.NewReplacer(pairs...)
	}()
)

// shown is a line as goldens show it: tags readable, other escapes gone.
func shown(line string) string {
	return ansi.Strip(shows.Replace(line))
}

// testStyles draw with tags, and the band as ░ up to the width.
func testStyles() Styles {
	return Styles{
		Bold: tagged[0], Italic: tagged[1], Strike: tagged[2], Code: tagged[3], Dim: tagged[4],
		Heading: tagged[5], TableHeader: tagged[6],
		Band: func(line string, w int) string {
			return ansi.Truncate(line, w, "") + strings.Repeat("░", max(w-ansi.StringWidth(line), 0))
		},
	}
}

// fixtures are the documents in testdata, by name.
var fixtures = []string{"inline", "headings", "lists", "quotes", "code", "table", "html", "refs"}

func fixture(t testing.TB, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name+".md"))
	require.NoError(t, err)

	return string(b)
}

func TestGoldens(t *testing.T) {
	widths := map[string][]int{"table": {80, 50, 34, 20}}
	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			var b strings.Builder
			for _, w := range cmpOr(widths[name], []int{80, 40, 20}) {
				fmt.Fprintf(&b, "== width %d ==\n", w)
				for _, l := range New(testStyles()).Render(fixture(t, name), w, "● ", "  ") {
					b.WriteString(strings.TrimRight(shown(l), " ") + "\n")
				}
			}
			golden(t, name, b.String())
		})
	}
}

func cmpOr(a, b []int) []int {
	if a != nil {
		return a
	}

	return b
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden")
	if *update {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o600))
	}
	want, err := os.ReadFile(path)
	require.NoError(t, err, "run go test ./internal/tui/render/markdown -update")
	assert.Equal(t, string(want), got)
}

func TestLinesFitTheWidth(t *testing.T) {
	for _, name := range fixtures {
		for _, w := range []int{80, 40, 24} {
			for _, l := range New(testStyles()).Render(fixture(t, name), w, "", "") {
				assert.LessOrEqual(t, ansi.StringWidth(l), w, "%s at %d: %q", name, w, shown(l))
			}
		}
	}
}

func TestEmptyTextIsTheFirstPrefix(t *testing.T) {
	assert.Equal(t, []string{"● "}, New(testStyles()).Render("", 40, "● ", "  "))
	assert.Equal(t, []string{"● "}, New(testStyles()).Render("\n\n", 40, "● ", "  "))
}

func TestHighlightsKnownLanguagesOnly(t *testing.T) {
	st := testStyles()
	st.CodeStyle = codeStyle(t)
	r := New(st)
	gofence := r.Render("```go\nfunc main() {}\n```", 40, "", "")
	assert.Contains(t, gofence[0], "\x1b[", "go is highlighted")
	for _, src := range []string{"```\nfunc main() {}\n```", "```nosuchlanguage\nfunc main() {}\n```"} {
		got := r.Render(src, 40, "", "")
		assert.Equal(t, " <c>func main() {}</>", strings.TrimRight(shown(got[0]), "░"), src)
	}
}

func TestHighlightingIsCached(t *testing.T) {
	st := testStyles()
	st.CodeStyle = codeStyle(t)
	r := New(st)
	code := "```go\nfunc main() {}\n```"
	r.Render(code, 40, "", "")
	r.Render(code, 60, "", "")
	r.Render("text\n\n"+code, 40, "", "")
	assert.Len(t, r.code.lines, 1, "one block, highlighted once for every width and place")
	assert.Len(t, r.code.lexers, 1)
}

func TestAnOpenFenceHighlightsItsCompleteLinesOnce(t *testing.T) {
	st := testStyles()
	st.CodeStyle = codeStyle(t)
	r := New(st)
	head := "```go\nfunc main() {\n\tx := 1\n"
	for _, tail := range []string{"p", "pr", "pri", "prin"} {
		r.Render(head+tail, 40, "", "")
	}
	_, ok := r.code.lines[codeKey{"go", "func main() {\n\tx := 1"}]
	assert.True(t, ok, "the complete lines are one cached piece")
	assert.Len(t, r.code.lines, 5, "the head once, and each partial line")
}
