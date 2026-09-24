// Package render draws TUI state as screen lines with lipgloss. It knows
// nothing about Bubble Tea: the shell passes in the composer's view.
package render

import (
	"fmt"
	"image/color"
	"math"
	"regexp"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/alecthomas/chroma/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/viktordanov/uagent-harness/internal/contextusage"
)

// Theme is every color the TUI draws with. Text is left to the terminal,
// and so is the background: only your messages, the composer, and code
// blocks sit on Band, so the terminal's own background shows everywhere else.
type Theme struct {
	Accent, Dim, Bad, Warn color.Color
	// Good marks what is ready or passed, such as an MCP server.
	Good color.Color
	// Info and Extra tell /context's categories apart.
	Info, Extra color.Color
	// Band is the background of your messages, the composer, and code.
	Band color.Color
	// Breath runs from dim to bright: the working λ's frames.
	Breath []color.Color
	// Code colors code blocks: keywords, names, strings, numbers, comments.
	Keyword, Name, String, Number, Comment color.Color
	// DiffAdd and DiffDel tint added and removed diff lines across the
	// whole line; DiffAddWord and DiffDelWord mark the changed words in them.
	DiffAdd, DiffDel, DiffAddWord, DiffDelWord color.Color
}

// Amber is the default theme, for dark terminals: a saturated amber for
// the λ, live commands, and marks, on dim text that stays warm.
var Amber = Theme{
	Accent: hex("#ffc400"), Dim: hex("#a08c64"), Bad: hex("#ff5a3c"), Warn: hex("#ffc400"), Good: hex("#9be564"),
	Info: hex("#5cc8ff"), Extra: hex("#d49bff"),
	Band:    hex("#2a2a2a"),
	Breath:  []color.Color{hex("#5a4200"), hex("#806000"), hex("#a67c00"), hex("#cc9900"), hex("#e6b000"), hex("#ffc400"), hex("#ffe066")},
	Keyword: hex("#ffc400"), Name: hex("#ffd75e"), String: hex("#9be564"), Number: hex("#ff9f43"), Comment: hex("#8a8272"),
	// Codex's dark diff tints, and stronger ones for the changed words.
	DiffAdd: hex("#212922"), DiffDel: hex("#3c170f"), DiffAddWord: hex("#2f5a32"), DiffDelWord: hex("#6e2a18"),
}

// AmberLight is Amber for light terminals: deep amber ink.
var AmberLight = Theme{
	Accent: hex("#b86e00"), Dim: hex("#8f7b58"), Bad: hex("#d0301c"), Warn: hex("#b86e00"), Good: hex("#4f8a10"),
	Info: hex("#0a7bc2"), Extra: hex("#8a4fd6"),
	Band:    hex("#efe9dc"),
	Breath:  []color.Color{hex("#ecd9b0"), hex("#e0bf80"), hex("#d4a24c"), hex("#c88a22"), hex("#bd7a08"), hex("#b86e00"), hex("#8a4f00")},
	Keyword: hex("#b86e00"), Name: hex("#9a5c00"), String: hex("#4f8a10"), Number: hex("#c4501a"), Comment: hex("#9a917f"),
	// GitHub's light diff colors, as Codex uses on light terminals.
	DiffAdd: hex("#e6ffec"), DiffDel: hex("#ffebe9"), DiffAddWord: hex("#abf2bc"), DiffDelWord: hex("#ffc1c0"),
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
	// categoryColors color /context's categories.
	categoryColors map[string]lipgloss.Style
	// diffStyles draw added and removed diff lines.
	diffStyles map[string]diffStyle
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
	categoryColors = map[string]lipgloss.Style{
		contextusage.SystemPrompt: dim,
		contextusage.Instructions: lipgloss.NewStyle().Foreground(t.Name),
		contextusage.Skills:       lipgloss.NewStyle().Foreground(t.Good),
		contextusage.Tools:        lipgloss.NewStyle().Foreground(t.Accent),
		contextusage.MCPTools:     lipgloss.NewStyle().Foreground(t.Number),
		contextusage.UserMessages: lipgloss.NewStyle().Foreground(t.Info),
		contextusage.Assistant:    lipgloss.NewStyle().Foreground(t.Extra),
		contextusage.ToolResults:  lipgloss.NewStyle().Foreground(t.Bad),
	}
	diffStyles = newDiffStyles(t)
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
	line = ansi.Truncate(untab(line), w, "")
	pad := strings.Repeat(" ", max(w-ansi.StringWidth(line), 0))
	// Any SGR that resets the background (0, 49, or no parameters) would
	// end the band early, so the band follows each one.
	line = sgr.ReplaceAllStringFunc(line, func(seq string) string {
		params := strings.Split(seq[2:len(seq)-1], ";")
		if slices.ContainsFunc(params, func(p string) bool { return p == "" || p == "0" || p == "49" }) {
			return seq + bandOn
		}

		return seq
	})

	return bandOn + line + pad + "\x1b[m"
}

// sgr matches one SGR escape sequence.
var sgr = regexp.MustCompile("\x1b\\[[0-9;]*m")

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
