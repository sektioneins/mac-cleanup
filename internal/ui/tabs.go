package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"mac-cleanup/internal/protocol"
)

// tab is one of the main screen's lists.
type tab int

const (
	tabSpace tab = iota
	tabAutostart
)

// tabCursor is the list cursor of the tab that is not showing.
type tabCursor struct {
	cursor int
	curID  string
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

// switchTab shows t, parking the cursor of the tab that was showing.
func (m *Model) switchTab(t tab) {
	if t == m.tab {
		return
	}
	cur := tabCursor{m.cursor, m.curID}
	m.cursor, m.curID = m.parked.cursor, m.parked.curID
	m.parked = cur
	m.tab = t
}

// tabBar is the second header line, with the current tab in brackets.
func (m Model) tabBar() string {
	if m.tab == tabAutostart {
		return "  Space   [Autostart]"
	}
	return " [Space]   Autostart"
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
