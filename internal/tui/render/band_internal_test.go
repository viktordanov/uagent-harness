package render

import (
	"image/color"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/assert"
)

func TestBand_FillsTheWidthThroughTabsAndResets(t *testing.T) {
	for _, line := range []string{
		"\tfunc main() {}",
		"plain",
		"a \x1b[1;38;2;1;2;3mbold\x1b[0m b \x1b[49mno bg\x1b[m c",
		"wide 漢字 text",
	} {
		out := band(line, 30)
		assert.Equal(t, 30, ansi.StringWidth(out), line)
		assert.NotContains(t, out, "\t")
		assert.Equal(t, 0, countUnbanded(out), "every background reset is followed by the band: %q", line)
	}
}

// countUnbanded counts resets not followed by the band.
func countUnbanded(s string) int {
	n := 0
	for _, loc := range sgr.FindAllStringIndex(s, -1) {
		seq := s[loc[0]:loc[1]]
		if (seq == "\x1b[m" || seq == "\x1b[0m" || seq == "\x1b[49m") && loc[1] < len(s) && !hasPrefixAt(s, loc[1], bandOn) {
			n++
		}
	}

	return n
}

func hasPrefixAt(s string, i int, p string) bool { return len(s)-i >= len(p) && s[i:i+len(p)] == p }

func TestThemeFor(t *testing.T) {
	assert.Equal(t, Amber.Accent, ThemeFor(nil).Accent, "no answer: the dark theme")
	dark := ThemeFor(color.RGBA{R: 0x1c, G: 0x1c, B: 0x1c, A: 0xff})
	assert.Equal(t, Amber.Accent, dark.Accent)
	r, _, _ := rgb(dark.Band)
	assert.Greater(t, r, uint8(0x1c), "the band is a little lighter than a dark background")
	light := ThemeFor(color.RGBA{R: 0xfa, G: 0xfa, B: 0xf7, A: 0xff})
	assert.Equal(t, AmberLight.Accent, light.Accent)
	r, _, _ = rgb(light.Band)
	assert.Less(t, r, uint8(0xfa), "and a little darker than a light one")
}
