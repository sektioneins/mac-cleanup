package ui

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"mac-cleanup/internal/action"
	"mac-cleanup/internal/protocol"
	"mac-cleanup/internal/scan"
)

// colourScreens returns every screen with colour in use: grades, warnings,
// a selected item, a scan failure, both actions and a failed run.
func colourScreens(t *testing.T) map[string]Model {
	t.Helper()
	d, _ := fakeDeps()
	items := []protocol.Item{
		both("rm -rf /a", "trash /a"),
		item("s", "redownload", 4_000_000, func(it *protocol.Item) { it.Grade = protocol.Redownload }),
		item("s", "mayneed", 3_000_000, func(it *protocol.Item) {
			it.Grade, it.Admin = protocol.MayNeed, true
			it.Warnings = []string{"App is running"}
		}),
		item("s", "cache", 2_000_000, func(it *protocol.Item) { it.View = "Caches" }),
	}
	m := loaded(t, *d, scan.Result{Script: "s", Items: items}, scan.Result{Script: "bad", Err: errors.New("boom")})
	for _, it := range m.visibleItems() {
		m.selected[it.Key()] = true
	}
	out := map[string]Model{"list": m}
	v := m
	v.screen, v.view, v.viewCat = screenView, "Caches", "Developer"
	out["view"] = v
	r := press(m, "enter")
	out["review"] = r
	r = press(r, "enter")
	out["review buttons"] = r
	res := m
	e := runEntry{Item: items[0], Action: protocol.Delete, Command: "rm -rf /a"}
	res.screen = screenResult
	res.run = runState{entries: []runEntry{e, e}, outs: []*action.Outcome{{Err: errors.New("exit 1"), Output: "denied"}, nil},
		freeBefore: 1_000_000_000, freeAfter: 2_000_000_000, beforeOK: true, afterOK: true}
	out["result"] = res
	return out
}

func TestColourKeepsLayout(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI)
	defer lipgloss.SetColorProfile(prev)
	sgr := regexp.MustCompile(`^\x1b\[[0-9;]*m`)
	cursor := regexp.MustCompile(`^\x1b\[7m[^\x1b]*\x1b\[0m$`)
	colour := regexp.MustCompile(`\x1b\[(?:[0-9]+;)*3[1-7]m`)
	for name, m := range colourScreens(t) {
		for _, w := range []int{40, 60, 80, 120} {
			m.width, m.height = w, 30
			out := m.View()
			if !colour.MatchString(out) {
				t.Fatalf("%s: no colour:\n%s", name, out)
			}
			for _, line := range strings.Split(out, "\n") {
				for i := 0; i < len(line); i++ {
					if line[i] == 0x1b && !sgr.MatchString(line[i:]) {
						t.Fatalf("%s width %d: partial escape in %q", name, w, line)
					}
				}
				// Below 80 columns some rows overflowed before colour, and
				// the renderer cuts them.
				if w >= 80 && lipgloss.Width(line) > w {
					t.Fatalf("%s width %d: line too wide: %q", name, w, line)
				}
				if strings.HasPrefix(line, "\x1b[7m> ") && !cursor.MatchString(line) {
					t.Fatalf("%s width %d: cursor row is not one plain reverse span: %q", name, w, line)
				}
			}
		}
	}
}

func TestNoColourWithoutProfile(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.Ascii)
	defer lipgloss.SetColorProfile(prev)
	for name, m := range colourScreens(t) {
		if out := m.View(); strings.Contains(out, "\x1b") {
			t.Fatalf("%s: escape without a colour profile:\n%q", name, out)
		}
	}
}
