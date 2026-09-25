package markdown

import (
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
)

// frame is where a block is drawn: its width, and whether it sits in a
// quote, whose text is dim.
type frame struct {
	w      int
	quoted bool
}

// base is the style a frame's text starts with.
func (r *Renderer) base(f frame) []Style {
	if f.quoted {
		return []Style{r.st.Dim}
	}

	return nil
}

// block draws one block node at the frame's width.
func (r *Renderer) block(src []byte, n ast.Node, f frame) []string {
	switch n := n.(type) {
	case *ast.Paragraph, *ast.TextBlock:
		return wrap(r.inline(src, n, r.base(f)), f.w)
	case *ast.Heading:
		return wrap(r.inline(src, n, append(r.base(f), r.st.Heading)), f.w)
	case *ast.ThematicBreak:
		return []string{r.st.Dim.Render(strings.Repeat("─", min(f.w, 40)))}
	case *ast.FencedCodeBlock:
		return r.codeBlock(string(n.Language(src)), lines(src, n), f.w)
	case *ast.CodeBlock:
		return r.codeBlock("", lines(src, n), f.w)
	case *ast.Blockquote:
		body := r.children(src, n, frame{w: f.w - 2, quoted: true}, false)
		for i, l := range body {
			body[i] = r.quoteBar + l
		}

		return body
	case *ast.List:
		return r.list(src, n, f)
	case *ast.HTMLBlock:
		html := lines(src, n)
		if n.HasClosure() {
			html += string(n.ClosureLine.Value(src))
		}

		return wrap(untab(strings.TrimRight(html, "\n")), f.w)
	case *east.Table:
		return r.table(src, n, f)
	}

	return r.children(src, n, f, false)
}

// children draws a container's blocks, with a blank line between them
// unless tight.
func (r *Renderer) children(src []byte, n ast.Node, f frame, tight bool) []string {
	var out []string
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		lines := r.block(src, c, f)
		if len(lines) == 0 {
			continue
		}
		if len(out) > 0 && !tight {
			out = append(out, "")
		}
		out = append(out, lines...)
	}

	return out
}

// list draws a list: a dim "•" or number before each item, and its lines
// under the item's first with a hanging indent.
func (r *Renderer) list(src []byte, l *ast.List, f frame) []string {
	var out []string
	i := 0
	for item := l.FirstChild(); item != nil; item = item.NextSibling() {
		marker := "•"
		if l.IsOrdered() {
			marker = strconv.Itoa(l.Start+i) + string(l.Marker)
		}
		hang := strings.Repeat(" ", ansi.StringWidth(marker)+1)
		body := r.children(src, item, frame{w: f.w - len(hang), quoted: f.quoted}, l.IsTight)
		if len(body) == 0 {
			body = []string{""}
		}
		if i > 0 && !l.IsTight {
			out = append(out, "")
		}
		out = append(out, r.st.Dim.Render(marker)+" "+body[0])
		for _, b := range body[1:] {
			if b != "" {
				b = hang + b
			}
			out = append(out, b)
		}
		i++
	}

	return out
}

// codeBlock draws code on the band, highlighted and not wrapped, as Codex
// does. While a fence is still open its last line is usually incomplete:
// the complete lines are highlighted as one piece, which stays cached until
// the next newline, and the partial line on its own.
func (r *Renderer) codeBlock(lang, code string, w int) []string {
	var hl []string
	if i := strings.LastIndexByte(code, '\n'); i >= 0 && i < len(code)-1 {
		hl = append(slices.Clip(r.highlight(lang, code[:i])), r.highlight(lang, code[i+1:])...)
	} else {
		hl = r.highlight(lang, strings.TrimSuffix(code, "\n"))
	}
	out := make([]string, len(hl))
	for i, l := range hl {
		out[i] = r.st.Band(" "+l, w)
	}

	return out
}

// lines is the text of a block's source lines. goldmark ends a code
// block's last line with a newline even at the end of the text; lines does
// not, so an open fence's partial line shows as partial.
func lines(src []byte, n ast.Node) string {
	var b strings.Builder
	segs := n.Lines()
	for i := range segs.Len() {
		seg := segs.At(i)
		v := seg.Value(src)
		if seg.ForceNewline && seg.Stop > 0 && src[seg.Stop-1] != '\n' {
			v = v[:len(v)-1]
		}
		b.Write(v)
	}

	return b.String()
}

// wrap word-wraps styled text to width w; its newlines stay line breaks.
func wrap(s string, w int) []string {
	return carry(strings.Split(ansi.Wrap(s, max(w, 10), ""), "\n"))
}

// carry ends each line's styles with it and opens them again on the next,
// so a style that wraps survives what is drawn before the next line, such
// as a quote bar that resets the terminal's style.
func carry(lines []string) []string {
	if len(lines) < 2 {
		return lines
	}
	var open []string
	for i, l := range lines {
		reopen := strings.Join(open, "")
		for _, seq := range sgr.FindAllString(l, -1) {
			if seq == "\x1b[m" || seq == "\x1b[0m" {
				open = open[:0]
			} else {
				open = append(open, seq)
			}
		}
		if len(open) > 0 {
			l += "\x1b[m"
		}
		lines[i] = reopen + l
	}

	return lines
}

// sgr matches one SGR escape sequence.
var sgr = regexp.MustCompile("\x1b\\[[0-9;:]*m")

func untab(s string) string { return strings.ReplaceAll(s, "\t", "    ") }
