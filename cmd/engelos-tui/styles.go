package main

import "github.com/charmbracelet/lipgloss"

// Palette mirrors the Svelte dashboard so the two UIs feel like one app.
// Values are exported so other files in the binary (and future tests) can
// reference them directly without re-defining colours.
var (
	ColorPrimary    = lipgloss.Color("#8b5cf6")
	ColorBackground = lipgloss.Color("#0a0a0a")
	ColorSurface    = lipgloss.Color("#1a1a1a")
	ColorText       = lipgloss.Color("#e5e7eb")
	ColorMuted      = lipgloss.Color("#9ca3af")
	ColorSuccess    = lipgloss.Color("#10b981")
	ColorError      = lipgloss.Color("#ef4444")
)

// TitleStyle is used for view headers and the top bar.
var TitleStyle = lipgloss.NewStyle().
	Foreground(ColorPrimary).
	Bold(true).
	Padding(0, 1)

// StatStyle frames each headline counter in the dashboard.
var StatStyle = lipgloss.NewStyle().
	Foreground(ColorText).
	Background(ColorSurface).
	Padding(0, 2).
	Margin(0, 1).
	Border(lipgloss.RoundedBorder()).
	BorderForeground(ColorMuted)

// ErrorStyle highlights login failures and transient API errors.
var ErrorStyle = lipgloss.NewStyle().
	Foreground(ColorError).
	Bold(true)

// MutedStyle is the default style for secondary text (footers, hints).
var MutedStyle = lipgloss.NewStyle().
	Foreground(ColorMuted)

// SuccessStyle marks healthy connection indicators.
var SuccessStyle = lipgloss.NewStyle().
	Foreground(ColorSuccess).
	Bold(true)

// BorderStyle is the inert border used around inactive panels.
var BorderStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(ColorMuted).
	Padding(0, 1)

// ActiveBorderStyle is BorderStyle with the primary accent.
var ActiveBorderStyle = lipgloss.NewStyle().
	Border(lipgloss.RoundedBorder()).
	BorderForeground(ColorPrimary).
	Padding(0, 1)

// FooterStyle paints the keybind hint bar pinned to the bottom of every view.
var FooterStyle = lipgloss.NewStyle().
	Foreground(ColorMuted).
	Padding(0, 1)

// HelpStyle is the overlay shown when the user presses '?'. It sits on top
// of the underlying view with a primary border so it visually pops.
var HelpStyle = lipgloss.NewStyle().
	Border(lipgloss.DoubleBorder()).
	BorderForeground(ColorPrimary).
	Padding(1, 2).
	Background(ColorBackground).
	Foreground(ColorText)
