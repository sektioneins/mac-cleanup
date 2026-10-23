package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"mac-cleanup/internal/protocol"
)

// HumanSize formats bytes in decimal units like Finder: 12.1 GB, 640 MB, 12 KB.
func HumanSize(b int64) string {
	switch {
	case b >= 1_000_000_000_000:
		return fmt.Sprintf("%.1f TB", float64(b)/1e12)
	case b >= 1_000_000_000:
		return trimZero(fmt.Sprintf("%.1f", float64(b)/1e9)) + " GB"
	case b >= 1_000_000:
		return fmt.Sprintf("%.0f MB", float64(b)/1e6)
	case b >= 1_000:
		return fmt.Sprintf("%.0f KB", float64(b)/1e3)
	}
	return fmt.Sprintf("%d B", b)
}

// trimZero turns "5.0" into "5".
func trimZero(s string) string { return strings.TrimSuffix(s, ".0") }

func gradeLabel(g protocol.Grade) string {
	switch g {
	case protocol.Regenerates:
		return "● regenerates"
	case protocol.Redownload:
		return "◐ re-download"
	case protocol.MayNeed:
		return "○ may need"
	}
	return string(g)
}

// fit pads or truncates s to exactly w display cells.
func fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	for lipgloss.Width(string(r)) > w {
		r = r[:len(r)-1]
	}
	return string(r) + strings.Repeat(" ", w-lipgloss.Width(string(r)))
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return itoa(n) + " " + noun + "s"
}

// wrap word-wraps s to width w and returns at most maxLines lines; if the
// text is longer, the last line ends in an ellipsis.
func wrap(s string, w, maxLines int) []string {
	var lines []string
	cur := ""
	for _, word := range strings.Fields(s) {
		switch {
		case cur == "":
			cur = word
		case lipgloss.Width(cur)+1+lipgloss.Width(word) <= w:
			cur += " " + word
		default:
			lines = append(lines, cur)
			cur = word
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines[maxLines-1] = strings.TrimRight(fit(lines[maxLines-1], w-1), " ") + "…"
	}
	for i, l := range lines {
		lines[i] = fit(l, w)
	}
	return lines
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}
