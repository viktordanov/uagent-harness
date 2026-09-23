// Package render draws TUI state as screen lines with lipgloss. It knows
// nothing about Bubble Tea: the shell passes in the composer's view.
package render

import "charm.land/lipgloss/v2"

var (
	dim      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	bold     = lipgloss.NewStyle().Bold(true)
	user     = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	answer   = lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Bold(true)
	ok       = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	bad      = lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	warn     = lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	tool     = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	italic   = lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Italic(true)
	header   = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("#87afff"))
	selected = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("#87afff"))
)

var spinner = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
