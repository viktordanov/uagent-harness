package markdown

import (
	"bytes"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/yuin/goldmark/ast"
)

// alertTitles are GitHub's alert kinds and the titles they are drawn with.
var alertTitles = map[string]string{
	"NOTE": "Note", "TIP": "Tip", "IMPORTANT": "Important", "WARNING": "Warning", "CAUTION": "Caution",
}

// quote draws a block quote indented two cells, its text dim and italic,
// between dim “ ” marks: the opening one before its first line and the
// closing one after its last, or under it when the line leaves no room. A
// GitHub alert (a first line of "[!WARNING]") is drawn by alert instead.
func (r *Renderer) quote(src []byte, n *ast.Blockquote, f frame) []string {
	if kind := alertKind(src, n); kind != "" {
		return r.alert(src, n, kind, f)
	}
	body := r.children(src, n, frame{w: f.w - 2, quoted: true, depth: f.depth}, false)
	if len(body) == 0 {
		return nil
	}
	for i, l := range body {
		switch {
		case i == 0:
			body[i] = r.open + l
		case l != "":
			body[i] = "  " + l
		}
	}
	if last := body[len(body)-1]; ansi.StringWidth(last)+2 <= f.w {
		body[len(body)-1] = last + r.close
	} else {
		body = append(body, "  "+r.closeAlone)
	}

	return body
}

// alert draws a GitHub alert as a quote without marks: a bold title in the
// alert's color ("! Warning"), then its blocks indented two cells in the
// normal text color.
func (r *Renderer) alert(src []byte, n *ast.Blockquote, kind string, f frame) []string {
	style, ok := r.st.Alerts[kind]
	if !ok {
		style = r.st.Bold
	}
	out := []string{style.Render("! " + alertTitles[kind])}
	dropMarker(n)
	for _, l := range r.children(src, n, frame{w: f.w - 2, depth: f.depth}, false) {
		if l != "" {
			l = "  " + l
		}
		out = append(out, l)
	}

	return out
}

// alertKind is the kind of alert a quote is, from a first line such as
// "[!NOTE]" (in any case, as GitHub reads it), or "" for a plain quote.
func alertKind(src []byte, n *ast.Blockquote) string {
	p, ok := n.FirstChild().(*ast.Paragraph)
	if !ok || p.Lines().Len() == 0 {
		return ""
	}
	first := p.Lines().At(0)
	line := bytes.TrimSpace(first.Value(src))
	if len(line) < 4 || !bytes.HasPrefix(line, []byte("[!")) || line[len(line)-1] != ']' {
		return ""
	}
	kind := strings.ToUpper(string(line[2 : len(line)-1]))
	if _, ok := alertTitles[kind]; !ok {
		return ""
	}

	return kind
}

// dropMarker takes an alert's marker line out of its first paragraph, and
// the paragraph out of the quote when nothing else is in it.
func dropMarker(n *ast.Blockquote) {
	p := n.FirstChild()
	end := p.Lines().At(0).Stop
	for c := p.FirstChild(); c != nil; {
		t, ok := c.(*ast.Text)
		if !ok || t.Segment.Start >= end {
			break
		}
		next := c.NextSibling()
		p.RemoveChild(p, c)
		c = next
	}
	if p.ChildCount() == 0 {
		n.RemoveChild(n, p)
	}
}
