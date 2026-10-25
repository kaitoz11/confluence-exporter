package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	colAccent  = lipgloss.AdaptiveColor{Light: "#0052CC", Dark: "#4C9AFF"}
	colText    = lipgloss.AdaptiveColor{Light: "#172B4D", Dark: "#DFE1E6"}
	colMuted   = lipgloss.AdaptiveColor{Light: "#6B778C", Dark: "#8993A4"}
	colSuccess = lipgloss.AdaptiveColor{Light: "#006644", Dark: "#57D9A3"}
	colWarn    = lipgloss.AdaptiveColor{Light: "#974F0C", Dark: "#FFC400"}
	colError   = lipgloss.AdaptiveColor{Light: "#BF2600", Dark: "#FF7452"}
	colSelBg   = lipgloss.AdaptiveColor{Light: "#DEEBFF", Dark: "#253858"}

	sTitle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#0052CC")).Padding(0, 1)
	sHeading = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	sText    = lipgloss.NewStyle().Foreground(colText)
	sMuted   = lipgloss.NewStyle().Foreground(colMuted)
	sBold    = lipgloss.NewStyle().Bold(true).Foreground(colText)
	sSuccess = lipgloss.NewStyle().Foreground(colSuccess)
	sWarn    = lipgloss.NewStyle().Foreground(colWarn)
	sError   = lipgloss.NewStyle().Foreground(colError)
	sKey     = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	sCursor  = lipgloss.NewStyle().Background(colSelBg).Foreground(colText).Bold(true)
	sLabel   = lipgloss.NewStyle().Bold(true).Foreground(colText).Width(22)

	sButton       = lipgloss.NewStyle().Foreground(colText).Background(lipgloss.AdaptiveColor{Light: "#EBECF0", Dark: "#344563"}).Padding(0, 2)
	sButtonActive = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#0052CC")).Padding(0, 2)
)

// keyHelp renders "key action · key action" for the footer.
func keyHelp(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, sKey.Render(pairs[i])+" "+sMuted.Render(pairs[i+1]))
	}
	return strings.Join(parts, sMuted.Render("  ·  "))
}

func button(label string, active bool) string {
	if active {
		return sKey.Render("› ") + sButtonActive.Render(label)
	}
	return "  " + sButton.Render(label)
}

// hintIndent lines up hints under the value of an inputRow.
const hintIndent = "                          "

// inputRow renders "› Label   ▌ value" on a single line.
func inputRow(label, view string, focused bool) string {
	if focused {
		return sKey.Render("› ") + sLabel.Foreground(colAccent).Render(label) + sKey.Render("▌ ") + view
	}
	return "  " + sLabel.Render(label) + sMuted.Render("▏ ") + view
}

func checkbox(label string, checked, focused bool) string {
	box := "[ ]"
	if checked {
		box = "[" + sSuccess.Render("x") + "]"
	}
	line := box + " " + label
	if focused {
		return sKey.Render("›") + " " + sCursor.Render(line)
	}
	return "  " + line
}

// truncate shortens s to at most n display cells.
func truncate(s string, n int) string {
	if n <= 1 {
		return ""
	}
	if lipgloss.Width(s) <= n {
		return s
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > n {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}
