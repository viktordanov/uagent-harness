package render

import (
	"os"
	"testing"
)

// The Markdown benchmarks of docs/design/markdown.md, on a 215-line answer
// with every construct, drawn with the Amber theme at width 100:
//
//	go test -run '^$' -bench Markdown -benchmem ./internal/tui/render

func benchAnswer(b *testing.B) string {
	b.Helper()
	src, err := os.ReadFile("testdata/markdown/answer.md")
	if err != nil {
		b.Fatal(err)
	}

	return string(src)
}

// BenchmarkMarkdownCold draws the answer with nothing cached.
func BenchmarkMarkdownCold(b *testing.B) {
	src := benchAnswer(b)
	b.ReportAllocs()
	for b.Loop() {
		b.StopTimer()
		st := NewStyles(Amber)
		b.StartTimer()
		st.markdownLines(src, 100, "• ", "  ")
	}
}

// BenchmarkMarkdownWarm draws the same answer again.
func BenchmarkMarkdownWarm(b *testing.B) {
	src := benchAnswer(b)
	st := NewStyles(Amber)
	st.markdownLines(src, 100, "• ", "  ")
	b.ReportAllocs()
	for b.Loop() {
		st.markdownLines(src, 100, "• ", "  ")
	}
}

// BenchmarkMarkdownStream draws the answer as it streams in, 12 bytes at a
// time; ns/update is the average cost of one delta.
func BenchmarkMarkdownStream(b *testing.B) {
	src := benchAnswer(b)
	b.ReportAllocs()
	updates := 0
	for b.Loop() {
		b.StopTimer()
		st := NewStyles(Amber)
		b.StartTimer()
		for i := 12; i < len(src)+12; i += 12 {
			st.markdownLines(src[:min(i, len(src))], 100, "• ", "  ")
			updates++
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(updates), "ns/update")
}

// BenchmarkMarkdownResize draws the answer at a width it was not drawn at
// lately, with its code already highlighted.
func BenchmarkMarkdownResize(b *testing.B) {
	src := benchAnswer(b)
	st := NewStyles(Amber)
	i := 0
	b.ReportAllocs()
	for b.Loop() {
		i++
		st.markdownLines(src, 60+i%80, "• ", "  ")
	}
}
