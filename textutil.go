package main

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/muesli/reflow/truncate"
)

const ellipsis = "…"

// truncateText cuts s (which may contain ANSI styles) to at most w terminal
// cells, ending with an ellipsis when something was cut.
func truncateText(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	return truncate.StringWithTail(s, uint(w), ellipsis)
}

// truncateLeft keeps the end of plain text s within w cells, e.g. for paths
// where the last segments matter most.
func truncateLeft(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= w {
		return s
	}
	rs := []rune(s)
	width := runewidth.StringWidth(ellipsis)
	i := len(rs)
	for i > 0 && width+runewidth.RuneWidth(rs[i-1]) <= w {
		i--
		width += runewidth.RuneWidth(rs[i])
	}
	return ellipsis + string(rs[i:])
}
