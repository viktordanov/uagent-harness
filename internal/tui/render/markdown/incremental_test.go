package markdown

import (
	"math/rand/v2"
	"os"
	"testing"

	"github.com/alecthomas/chroma/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/text"
)

func codeStyle(t testing.TB) *chroma.Style {
	t.Helper()
	style, err := chroma.NewStyle("test", chroma.StyleEntries{chroma.Keyword: "bold #ffc400", chroma.LiteralString: "#9be564"})
	require.NoError(t, err)

	return style
}

// fresh renders text with nothing kept from earlier documents.
func fresh(r *Renderer, text string, w int) []string {
	r.docs = nil

	return r.Render(text, w, "● ", "  ")
}

// answer is the benchmark fixture: a long answer with every construct.
func answer(t testing.TB) string {
	t.Helper()
	b, err := os.ReadFile("../testdata/markdown/answer.md")
	require.NoError(t, err)

	return string(b)
}

func TestIncrementalEqualsFullForEveryPrefix(t *testing.T) {
	st := testStyles()
	st.CodeStyle = codeStyle(t)
	for _, name := range fixtures {
		src := fixture(t, name)
		for _, w := range []int{80, 30} {
			stream, full := New(st), New(st)
			for i := range len(src) + 1 {
				got := stream.Render(src[:i], w, "● ", "  ")
				require.Equal(t, fresh(full, src[:i], w), got, "%s at width %d, prefix %d: %q", name, w, i, src[:i])
			}
		}
	}
}

func TestIncrementalEqualsFullWhileStreaming(t *testing.T) {
	st := testStyles()
	st.CodeStyle = codeStyle(t)
	src := answer(t)
	stream, full := New(st), New(st)
	rnd := rand.New(rand.NewPCG(1, 2))
	for i := 0; i < len(src); i += 1 + rnd.IntN(20) {
		require.Equal(t, fresh(full, src[:i], 100), stream.Render(src[:i], 100, "● ", "  "), "prefix %d", i)
	}
	require.Equal(t, fresh(full, src, 100), stream.Render(src, 100, "● ", "  "))
}

func TestWidthChangesRenderAgain(t *testing.T) {
	src := answer(t)
	r, full := New(testStyles()), New(testStyles())
	for i := 0; i < len(src)/2; i += 50 {
		r.Render(src[:i], 100, "", "")
	}
	for _, w := range []int{60, 100, 30} {
		assert.Equal(t, fresh(full, src, w), r.Render(src, w, "● ", "  "), "width %d", w)
	}
}

func TestTwoTextsWithTheSameStartKeepTheirOwnBlocks(t *testing.T) {
	r, full := New(testStyles()), New(testStyles())
	a, b := "Sure.\n\nFirst answer, with more.\n\nDone.", "Sure.\n\nSecond answer.\n\n- a list"
	for i := range max(len(a), len(b)) + 1 {
		assert.Equal(t, fresh(full, a[:min(i, len(a))], 40), r.Render(a[:min(i, len(a))], 40, "● ", "  "))
		assert.Equal(t, fresh(full, b[:min(i, len(b))], 40), r.Render(b[:min(i, len(b))], 40, "● ", "  "))
	}
}

func TestStylesAreTheRenderersOwn(t *testing.T) {
	src := "```go\nfunc main() {}\n```\n\n**bold**"
	light := testStyles()
	light.Bold = tagged[7]
	a, b := New(testStyles()), New(light)
	assert.NotEqual(t, a.Render(src, 40, "", ""), b.Render(src, 40, "", ""))
	assert.Equal(t, New(testStyles()).Render(src, 40, "", ""), a.Render(src, 40, "", ""), "nothing leaks from the other renderer")
}

// lastBlockStart is where the line of text's last top-level block starts,
// from a parse of the whole text.
func lastBlockStart(text string) int {
	src := []byte(text)
	root := goldmark.New(goldmark.WithExtensions(extension.GFM)).Parser().Parse(textReader(src))
	if root.LastChild() == nil {
		return 0
	}

	return max(lineStart(src, root.LastChild().Pos()), 0)
}

func textReader(src []byte) text.Reader { return text.NewReader(src) }

// The gate: while a text grows, each update parses only what follows the
// start of the previous text's last block, never the whole text.
func TestAnUpdateParsesOnlyTheLastBlock(t *testing.T) {
	docs := map[string]string{"answer": answer(t)}
	for _, name := range fixtures {
		if name != "refs" { // a link definition draws the whole text, as in Codex
			docs[name] = fixture(t, name)
		}
	}
	for name, src := range docs {
		r := New(testStyles())
		rnd := rand.New(rand.NewPCG(3, 4))
		prev := ""
		for i := 1; i <= len(src); i += 1 + rnd.IntN(20) {
			before := r.stats.parsed
			r.Render(src[:i], 80, "", "")
			bound := i - lastBlockStart(prev)
			require.LessOrEqual(t, r.stats.parsed-before, bound, "%s, prefix %d", name, i)
			prev = src[:i]
		}
	}
}

func TestTheSameTextAgainAllocatesOnlyItsCopy(t *testing.T) {
	src := answer(t)
	r := New(testStyles())
	r.Render(src, 100, "● ", "  ")
	allocs := testing.AllocsPerRun(20, func() { r.Render(src, 100, "● ", "  ") })
	assert.LessOrEqual(t, allocs, 1.0)
}
