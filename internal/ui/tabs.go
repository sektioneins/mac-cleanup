package ui

import (
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"mac-cleanup/internal/protocol"
	"mac-cleanup/internal/scan"
)

// tab is one of the main screen's lists.
type tab int

const (
	tabSpace tab = iota
	tabAutostart
)

// tabOf is the tab whose part of the scan round runs script.
func tabOf(script string) tab {
	if scan.Autostart(script) {
		return tabAutostart
	}
	return tabSpace
}

// tabCursor is the list cursor of the tab that is not showing.
type tabCursor struct {
	cursor int
	curID  string
	top    int
}

// listed reports whether an item shows up in its tab: autostart items always,
// the others from MinSize on.
func listed(it protocol.Item) bool {
	return it.Autostart() || it.Size >= MinSize
}

// amount is what an item is sorted and summed by: memory for autostart items,
// size for the others.
func amount(it protocol.Item) int64 {
	if it.Autostart() {
		return it.Memory
	}
	return it.Size
}

// totalsText is "3 items" followed by the disk and memory sums that are not 0.
func totalsText(n int, disk, memory int64) string {
	s := plural(n, "item")
	if disk > 0 {
		s += ", " + HumanSize(disk) + " disk"
	}
	if memory > 0 {
		s += ", " + HumanSize(memory) + " memory"
	}
	return s
}

// allItems returns the listed items of both tabs.
func (m Model) allItems() []protocol.Item {
	var out []protocol.Item
	for _, r := range m.results {
		for _, it := range r.Items {
			if listed(it) {
				out = append(out, it)
			}
		}
	}
	return out
}

// itemsOf returns the listed items of one tab.
func (m Model) itemsOf(t tab) []protocol.Item {
	var out []protocol.Item
	for _, it := range m.allItems() {
		if it.Autostart() == (t == tabAutostart) {
			out = append(out, it)
		}
	}
	return out
}

// switchTab shows t, parking the cursor of the tab that was showing. The first
// time t shows in a scan round, it returns the start of t's part of the round.
func (m *Model) switchTab(t tab) tea.Cmd {
	if t == m.tab {
		return nil
	}
	cur := tabCursor{m.cursor, m.curID, m.top}
	m.cursor, m.curID, m.top = m.parked.cursor, m.parked.curID, m.parked.top
	m.parked = cur
	m.tab = t
	if m.started[t] {
		return nil
	}
	m.started[t] = true
	return m.sharedStep(t)
}

// progress counts the scan results of tab t's scripts and its scripts.
func (m Model) progress(t tab) (done, total int) {
	for _, r := range m.results {
		if tabOf(r.Script) == t {
			done++
		}
	}
	return done, len(m.tabScripts(t))
}

// scanningNames returns what tab t's part of the round is waiting for: its
// shared sources, or the names (without .sh) of its scripts with no result
// yet, which all run at the same time.
func (m Model) scanningNames(t tab) []string {
	if !m.sharedOK[t] {
		return []string{"shared sources"}
	}
	got := map[string]bool{}
	for _, r := range m.results {
		got[r.Script] = true
	}
	var out []string
	for _, s := range m.tabScripts(t) {
		if b := filepath.Base(s); !got[b] {
			out = append(out, Visible(strings.TrimSuffix(b, ".sh")))
		}
	}
	return out
}

// tabColour is each tab's own colour, one of the terminal's ANSI colours.
var tabColour = [2]lipgloss.Color{tabSpace: "4", tabAutostart: "5"}

// tabBar is the second header line: each tab in its colour, the current one
// bold in reverse video, or in brackets when there is no colour.
func (m Model) tabBar() string {
	out := " "
	for t, name := range [2]string{tabSpace: "Space", tabAutostart: "Autostart"} {
		st, label := lipgloss.NewStyle().Foreground(tabColour[t]), " "+name+" "
		if tab(t) == m.tab {
			st = st.Bold(true).Reverse(true)
			if lipgloss.ColorProfile() == termenv.Ascii {
				label = "[" + name + "]"
			}
		}
		if t > 0 {
			out += "  "
		}
		out += st.Render(label)
	}
	return strings.TrimRight(out, " ")
}

// runningText is the header figure of the Autostart tab: the memory of the
// entries that are running or loaded.
func (m Model) runningText() string {
	var sum int64
	for _, it := range m.itemsOf(tabAutostart) {
		if it.State == "running" || it.State == "loaded" {
			sum += it.Memory
		}
	}
	return "running: " + HumanSize(sum)
}

// autostartLine renders an Autostart row: checkbox, name, memory, state,
// enabled, then the orphan reason or the owner.
func (m Model) autostartLine(it protocol.Item, cur bool) string {
	mark := "  "
	if cur {
		mark = "> "
	}
	boxText := "   "
	if len(it.Actions()) > 0 {
		boxText = box(m.selected[it.Key()], cur)
	}
	mem := "–"
	if it.State != "stopped" {
		mem = HumanSize(it.Memory)
	}
	state := fit(stateLabel(it.State), 9)
	enabled := fit(it.Enabled, 3)
	fixed := padLeft(mem, 9) + "  " + state + "  " + enabled + "  "
	avail := m.width - len(mark) - 4 - 1 - lipgloss.Width(fixed)

	// The owner or orphan reason gives way when the row is too wide, but the
	// name keeps up to 20 cells.
	name := Visible(it.Name)
	text, style := "owner unknown", faint
	switch {
	case it.Orphan != "":
		text, style = "✗ orphan: "+Visible(it.Orphan), danger
	case it.Owner != "":
		text = Visible(it.Owner)
	}
	extra := ""
	if len(it.Actions()) == 0 {
		extra = "  no action"
	}
	text = strings.TrimRight(fit(text, min(lipgloss.Width(text), max(avail-min(lipgloss.Width(name), 20)-len(extra), 8))), " ")
	tail := text + extra
	nameW := max(avail-lipgloss.Width(tail), 8)
	line := mark + boxText + " " + fit(name, nameW) + " " + fixed
	if cur {
		return rev.Render(line + tail)
	}
	return line + style.Render(text) + faint.Render(extra)
}
