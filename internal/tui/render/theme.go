// Package render draws TUI state as screen lines with lipgloss. It knows
// nothing about Bubble Tea: the shell passes in the composer's view.
package render

import (
	"fmt"
	"image/color"
	"math"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/charmbracelet/x/ansi"
)

// Theme is every color the TUI draws with. Text is left to the terminal,
// and so is the background: only your messages, the composer, and code
// blocks sit on Band, so the terminal's own background shows everywhere else.
type Theme struct {
	Accent, Dim, Bad, Warn color.Color
	// Good marks what is ready or passed, such as an MCP server.
	Good color.Color
	// Band is the background of your messages, the composer, and code.
	Band color.Color
	// Breath runs from dim to bright: the working λ's frames.
	Breath []color.Color
	// Code colors code blocks: keywords, names, strings, numbers, comments.
	Keyword, Name, String, Number, Comment color.Color
}

// Amber is the default theme, for dark terminals.
var Amber = Theme{
	Accent: hex("#ffc014"), Dim: hex("#8c7c58"), Bad: hex("#ff6b4a"), Warn: hex("#ffc014"), Good: hex("#b8c98a"),
	Band:    hex("#2a2a2a"),
	Breath:  []color.Color{hex("#4a3a10"), hex("#6b5214"), hex("#8c6a16"), hex("#b08618"), hex("#d6a31a"), hex("#ffc014"), hex("#ffd75a")},
	Keyword: hex("#ffc014"), Name: hex("#ecc56a"), String: hex("#b8c98a"), Number: hex("#e8a15a"), Comment: hex("#7a7466"),
}

// AmberLight is Amber for light terminals: dark amber ink.
var AmberLight = Theme{
	Accent: hex("#a35f00"), Dim: hex("#a38f6e"), Bad: hex("#c0392b"), Warn: hex("#a35f00"), Good: hex("#5f7a1f"),
	Band:    hex("#efe9dc"),
	Breath:  []color.Color{hex("#e6d3b0"), hex("#d9b986"), hex("#c99a55"), hex("#b98030"), hex("#a86b12"), hex("#a35f00"), hex("#7a4500")},
	Keyword: hex("#a35f00"), Name: hex("#8a5200"), String: hex("#5f7a1f"), Number: hex("#b4501e"), Comment: hex("#a39a88"),
}

// ThemeFor picks Amber or AmberLight for the terminal's background and
// derives the band from it, as Codex tints its message background.
func ThemeFor(bg color.Color) Theme {
	t := Amber
	if bg == nil {
		return t
	}
	r, g, b := rgb(bg)
	light := 0.299*float64(r)+0.587*float64(g)+0.114*float64(b) > 128
	if light {
		t = AmberLight
		t.Band = mix(bg, color.Black, 0.06)
	} else {
		t.Band = mix(bg, color.White, 0.07)
	}

	return t
}

// The styles items draw with, set by SetTheme.
var (
	dim, bold, accent, bad, warn, italic, header, selected lipgloss.Style
	// tool is the accent without bold; ok is Good; codeSpan is `code`.
	tool, ok, codeSpan lipgloss.Style
	quoteBar           string
	// bandOn switches the band's background on; it is re-applied after every
	// reset inside a band line.
	bandOn    string
	breath    []lipgloss.Style
	codeStyle *chroma.Style
)

func init() { SetTheme(Amber) }

// SetTheme sets the colors every later frame draws with. The shell calls it
// once it knows the terminal's background; call it from the update loop only.
func SetTheme(t Theme) {
	dim = lipgloss.NewStyle().Foreground(t.Dim)
	bold = lipgloss.NewStyle().Bold(true)
	accent = lipgloss.NewStyle().Foreground(t.Accent).Bold(true)
	bad = lipgloss.NewStyle().Foreground(t.Bad)
	warn = lipgloss.NewStyle().Foreground(t.Warn)
	italic = lipgloss.NewStyle().Foreground(t.Dim).Italic(true)
	header = lipgloss.NewStyle().Foreground(t.Band).Background(t.Accent)
	selected = header
	tool = lipgloss.NewStyle().Foreground(t.Accent)
	ok = lipgloss.NewStyle().Foreground(t.Good)
	codeSpan = lipgloss.NewStyle().Foreground(t.Name)
	quoteBar = dim.Render("│ ")
	r, g, b := rgb(t.Band)
	bandOn = fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b)
	breath = breath[:0]
	for _, c := range t.Breath {
		breath = append(breath, lipgloss.NewStyle().Foreground(c).Bold(true))
	}
	codeStyle = chroma.MustNewStyle("uah", chroma.StyleEntries{
		chroma.Keyword:             "bold " + hexOf(t.Keyword),
		chroma.NameFunction:        hexOf(t.Name),
		chroma.NameClass:           hexOf(t.Name),
		chroma.LiteralString:       hexOf(t.String),
		chroma.LiteralNumber:       hexOf(t.Number),
		chroma.Comment:             "italic " + hexOf(t.Comment),
		chroma.GenericInserted:     hexOf(t.String),
		chroma.GenericDeleted:      hexOf(t.Bad),
		chroma.NameBuiltin:         hexOf(t.Name),
		chroma.KeywordType:         hexOf(t.Name),
		chroma.OperatorWord:        "bold " + hexOf(t.Keyword),
		chroma.LiteralStringEscape: hexOf(t.Number),
	})
}

// band draws a line on the band background, padded to width w.
func band(line string, w int) string {
	line = ansi.Truncate(line, w, "")
	pad := strings.Repeat(" ", max(w-ansi.StringWidth(line), 0))

	line = strings.ReplaceAll(line, "\x1b[0m", "\x1b[m")

	return bandOn + strings.ReplaceAll(line, "\x1b[m", "\x1b[m"+bandOn) + pad + "\x1b[m"
}

// breathing is the working λ: seven shades, dim to bright and back, one
// breath every 1.6 s, eased like a slow breath.
func breathing(ms int64) string {
	const period = 1600
	t := float64(ms%period) / period
	i := int((1-cos2pi(t))/2*float64(len(breath)-1) + 0.5)

	return breath[i].Render("λ")
}

var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func hex(s string) color.Color { return lipgloss.Color(s) }

func rgb(c color.Color) (r, g, b uint8) {
	cr, cg, cb, _ := c.RGBA()

	return uint8(cr >> 8), uint8(cg >> 8), uint8(cb >> 8)
}

func hexOf(c color.Color) string {
	r, g, b := rgb(c)

	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}

// mix moves a toward b by f (0..1).
func mix(a, b color.Color, f float64) color.Color {
	ar, ag, ab := rgb(a)
	br, bg, bb := rgb(b)
	m := func(x, y uint8) uint8 { return uint8(float64(x) + (float64(y)-float64(x))*f + 0.5) }

	return color.RGBA{R: m(ar, br), G: m(ag, bg), B: m(ab, bb), A: 0xff}
}

func cos2pi(t float64) float64 { return math.Cos(2 * math.Pi * t) }

// Accent is the style of the λ prompt, for the shell's composer.
func Accent() lipgloss.Style { return accent }

// Dim is the style of the composer's placeholder.
func Dim() lipgloss.Style { return dim }
