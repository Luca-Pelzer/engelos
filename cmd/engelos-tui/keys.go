package main

// Keybinds is the canonical map of single-character actions used by every
// view. Centralising them here keeps the help overlay and the per-view
// switch statements in sync.
const (
	KeyQuit        = "q"
	KeyRefresh     = "r"
	KeyLeaderboard = "l"
	KeyChat        = "c"
	KeyDashboard   = "d"
	KeyBack        = "b"
	KeyHelp        = "?"
	KeyClose       = "esc"
)

// HelpLines returns the keybind cheatsheet shown by the '?' overlay. The
// slice ordering is the display order; keep it readable, not alphabetical.
func HelpLines() []string {
	return []string{
		"engelos-tui - keyboard reference",
		"",
		"  ?            toggle this help overlay",
		"  q / ctrl+c   quit (logs out first)",
		"  r            refresh current view",
		"  d            dashboard (default view)",
		"  l            leaderboards (pity + streak)",
		"  c            chat (live WebSocket)",
		"  b / esc      go back / close overlay",
		"  tab          cycle focus (leaderboard view)",
		"  ↑/↓ pg up/dn scroll (chat view)",
	}
}
