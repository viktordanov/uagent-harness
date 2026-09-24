package render

import (
	"regexp"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/x/ansi"
)

// The Markdown the model writes, drawn the way Codex draws it: code blocks
// highlighted without their fences, `code` in color, **bold**, headings,
// lists, and quotes. It is line-based rather than a full CommonMark parser:
// answers are short, and a partly odd layout beats a slow or heavy renderer.

var (
	codeSpan  = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	heading   = lipgloss.NewStyle().Bold(true)
	quoteBar  = dim.Render("│ ")
	codeStyle = styles.Get("monokai")

	inlineCode = regexp.MustCompile("`([^`]+)`")
	strong     = regexp.MustCompile(`\*\*([^*]+)\*\*|__([^_]+)__`)
	emphasis   = regexp.MustCompile(`(^|[^*\w])\*([^*\s][^*]*)\*|(^|[^_\w])_([^_\s][^_]*)_`)
	link       = regexp.MustCompile(`\[([^\]]+)\]\(([^)\s]+)\)`)
	listItem   = regexp.MustCompile(`^(\s*)([-*+]|\d+[.)])\s+(.*)$`)
	headingRe  = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*#*\s*$`)
	rule       = regexp.MustCompile(`^\s*([-*_])(\s*[-*_]){2,}\s*$`)
)

// markdownLines renders text at width w. The first line starts with first,
// the others with rest (both already styled and of equal width).
func markdownLines(text string, w int, first, rest string) []string {
	width := max(w-ansi.StringWidth(rest), 10)
	var body []string
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		trimmed := strings.TrimSpace(line)
		if fence, lang, ok := openFence(trimmed); ok {
			var code []string
			for i++; i < len(lines) && !strings.HasPrefix(strings.TrimSpace(lines[i]), fence); i++ {
				code = append(code, lines[i])
			}
			body = append(body, highlight(strings.Join(code, "\n"), lang)...)

			continue
		}
		switch {
		case trimmed == "":
			body = append(body, "")
		case rule.MatchString(line):
			body = append(body, dim.Render(strings.Repeat("─", min(width, 40))))
		case headingRe.MatchString(line):
			m := headingRe.FindStringSubmatch(line)
			body = append(body, wrap(heading.Render(inline(m[2])), width, "", "")...)
		case strings.HasPrefix(trimmed, ">"):
			quoted := strings.TrimSpace(strings.TrimPrefix(trimmed, ">"))
			body = append(body, wrap(dim.Render(inline(quoted)), width, quoteBar, quoteBar)...)
		case listItem.MatchString(line):
			m := listItem.FindStringSubmatch(line)
			indent := strings.Repeat(" ", min(len(m[1]), 8))
			marker := m[2]
			if !strings.ContainsAny(marker[:1], "0123456789") {
				marker = "•"
			}
			hang := indent + strings.Repeat(" ", ansi.StringWidth(marker)+1)
			body = append(body, wrap(inline(m[3]), width, indent+dim.Render(marker)+" ", hang)...)
		default:
			body = append(body, wrap(inline(trimmed), width, "", "")...)
		}
	}
	out := make([]string, 0, len(body))
	for i, l := range body {
		prefix := rest
		if i == 0 {
			prefix = first
		}
		out = append(out, prefix+l)
	}
	if len(out) == 0 {
		out = []string{first}
	}

	return out
}

// openFence reports whether a line opens a fenced code block, with its
// fence and language.
func openFence(line string) (fence, lang string, ok bool) {
	for _, f := range []string{"```", "~~~"} {
		if after, found := strings.CutPrefix(line, f); found {
			return f, strings.TrimSpace(after), true
		}
	}

	return "", "", false
}

// highlight colors code with chroma; lines are not wrapped, as in Codex.
func highlight(code, lang string) []string {
	lexer := lexers.Get(lang)
	if lexer == nil {
		lexer = lexers.Analyse(code)
	}
	if lexer == nil {
		return styleLines(strings.Split(code, "\n"), codeSpan)
	}
	it, err := chroma.Coalesce(lexer).Tokenise(nil, code)
	if err != nil {
		return strings.Split(code, "\n")
	}
	var b strings.Builder
	if err := formatters.TTY256.Format(&b, codeStyle, it); err != nil {
		return strings.Split(code, "\n")
	}

	return strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
}

// inline styles `code`, **bold**, *italic*, and [links](url).
func inline(s string) string {
	// Code spans first, and hide them from the other rules.
	var spans []string
	s = inlineCode.ReplaceAllStringFunc(s, func(m string) string {
		spans = append(spans, codeSpan.Render(m[1:len(m)-1]))

		return "\x00" + string(rune('0'+len(spans)-1)) + "\x00"
	})
	s = link.ReplaceAllString(s, "$1 "+dim.Render("($2)"))
	s = strong.ReplaceAllStringFunc(s, func(m string) string { return bold.Render(strings.Trim(m, "*_")) })
	s = emphasis.ReplaceAllStringFunc(s, func(m string) string {
		sub := emphasis.FindStringSubmatch(m)
		lead, text := sub[1], sub[2]
		if text == "" {
			lead, text = sub[3], sub[4]
		}

		return lead + lipgloss.NewStyle().Italic(true).Render(text)
	})
	for i, span := range spans {
		s = strings.Replace(s, "\x00"+string(rune('0'+i))+"\x00", span, 1)
	}

	return s
}

// wrap word-wraps styled text, starting lines with first and then rest.
func wrap(s string, width int, first, rest string) []string {
	var out []string
	for i, l := range strings.Split(ansi.Wrap(s, max(width-ansi.StringWidth(rest), 10), ""), "\n") {
		prefix := rest
		if i == 0 {
			prefix = first
		}
		out = append(out, prefix+l)
	}

	return out
}
