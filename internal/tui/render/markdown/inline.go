package markdown

import (
	"bytes"
	"slices"
	"strings"

	"github.com/yuin/goldmark/ast"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/util"
)

// inline draws a block's inline content as one styled string: `code`,
// bold, italic, strikethrough, and links with their URL dim after them.
// Soft and hard line breaks are newlines, as in Codex. Every piece of text
// is drawn with all the styles around it, innermost first, so a style
// ends with its text and never cuts the one around it short.
func (r *Renderer) inline(src []byte, n ast.Node, styles []Style) string {
	var b runs
	r.inlines(&b, src, n, styles)

	return b.String()
}

func (r *Renderer) inlines(b *runs, src []byte, parent ast.Node, styles []Style) {
	for n := parent.FirstChild(); n != nil; n = n.NextSibling() {
		r.inlineNode(b, src, n, styles)
	}
}

func (r *Renderer) inlineNode(b *runs, src []byte, n ast.Node, styles []Style) {
	switch n := n.(type) {
	case *ast.Text:
		v := n.Segment.Value(src)
		if !n.IsRaw() {
			v = unescape(v)
		}
		b.put(string(v), styles)
		if n.SoftLineBreak() || n.HardLineBreak() {
			b.put("\n", nil)
		}
	case *ast.String:
		b.put(string(n.Value), styles)
	case *ast.CodeSpan:
		var code runs
		r.inlines(&code, src, n, nil)
		b.put(strings.ReplaceAll(code.String(), "\n", " "), with(styles, r.st.Code))
	case *ast.Emphasis:
		style := r.st.Italic
		if n.Level > 1 {
			style = r.st.Bold
		}
		r.inlines(b, src, n, with(styles, style))
	case *east.Strikethrough:
		r.inlines(b, src, n, with(styles, r.st.Strike))
	case *ast.Link, *ast.Image, *ast.AutoLink:
		r.link(b, src, n, styles)
	case *ast.RawHTML:
		for i := range n.Segments.Len() {
			seg := n.Segments.At(i)
			b.put(string(seg.Value(src)), styles)
		}
	case *east.TaskCheckBox:
		box := "[ ]"
		if n.IsChecked {
			box = "[x]"
		}
		b.put(box, with(styles, r.st.Dim))
		b.put(" ", styles)
	default:
		r.inlines(b, src, n, styles)
	}
}

// link draws a link's text and its URL dim in parentheses, an image's
// description and its URL, and an autolink as its URL.
func (r *Renderer) link(b *runs, src []byte, n ast.Node, styles []Style) {
	var url string
	switch n := n.(type) {
	case *ast.AutoLink:
		b.put(string(n.Label(src)), styles)

		return
	case *ast.Link:
		url = string(n.Destination)
	case *ast.Image:
		url = string(n.Destination)
	}
	r.inlines(b, src, n, styles)
	if url != "" && url != plainText(src, n) {
		b.put(" ", styles)
		b.put("("+url+")", with(styles, r.st.Dim))
	}
}

// runs builds styled text. Text in a row with the same styles (the same
// slice, as siblings share) is drawn as one run.
type runs struct {
	b      strings.Builder
	run    strings.Builder
	styles []Style
}

// put adds s drawn with styles, the outermost first in the list.
func (b *runs) put(s string, styles []Style) {
	if s == "" {
		return
	}
	if len(styles) != len(b.styles) || len(styles) > 0 && &styles[0] != &b.styles[0] {
		b.flush()
		b.styles = styles
	}
	b.run.WriteString(untab(s))
}

func (b *runs) flush() {
	s := b.run.String()
	b.run.Reset()
	if s == "" {
		return
	}
	for _, style := range slices.Backward(b.styles) {
		s = style.Render(s)
	}
	b.b.WriteString(s)
}

func (b *runs) String() string {
	b.flush()

	return b.b.String()
}

// with is styles and one more inside them, without sharing the array.
func with(styles []Style, s Style) []Style {
	return append(slices.Clip(styles), s)
}

// unescape resolves backslash escapes and character references, as
// goldmark's HTML renderer does.
func unescape(v []byte) []byte {
	if bytes.IndexByte(v, '\\') >= 0 {
		v = util.UnescapePunctuations(v)
	}
	if bytes.IndexByte(v, '&') >= 0 {
		v = util.ResolveEntityNames(util.ResolveNumericReferences(v))
	}

	return v
}

// plainText is an inline node's text without styles.
func plainText(src []byte, n ast.Node) string {
	var b strings.Builder
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if t, ok := c.(*ast.Text); ok {
			b.Write(t.Segment.Value(src))
		} else {
			b.WriteString(plainText(src, c))
		}
	}

	return b.String()
}
