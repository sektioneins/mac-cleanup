package ui

import (
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mac-cleanup/internal/protocol"
)

func itoa(n int) string { return strconv.Itoa(n) }

// row is one navigable line of the list: an item or a separate-view summary.
type row struct {
	item     protocol.Item // zero for a view row
	view     string        // non-empty for a view row
	category string        // category of a view row
	count    int           // folders in the view
	size     int64
	last     int64 // LastUsed, 0 if none
}

type group struct {
	name  string
	total int64
	rows  []row
}

// groups builds categories from the visible items.
func (m Model) groups() []group {
	byCat := map[string]*group{}
	var order []string
	for _, it := range m.visibleItems() {
		g, ok := byCat[it.Category]
		if !ok {
			g = &group{name: it.Category}
			byCat[it.Category] = g
			order = append(order, it.Category)
		}
		g.total += it.Size
		if it.View == "" {
			g.rows = append(g.rows, row{item: it, size: it.Size, last: it.LastUsed})
			continue
		}
		i := indexOfView(g.rows, it.View)
		if i < 0 {
			g.rows = append(g.rows, row{view: it.View, category: it.Category})
			i = len(g.rows) - 1
		}
		g.rows[i].count++
		g.rows[i].size += it.Size
	}
	out := make([]group, 0, len(order))
	for _, c := range order {
		g := byCat[c]
		sort.SliceStable(g.rows, func(i, j int) bool { return rowLess(g.rows[i], g.rows[j]) })
		out = append(out, *g)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].total != out[j].total {
			return out[i].total > out[j].total
		}
		return out[i].name < out[j].name
	})
	return out
}

func indexOfView(rows []row, view string) int {
	for i, r := range rows {
		if r.view == view {
			return i
		}
	}
	return -1
}

func rowLess(a, b row) bool {
	if (a.last > 0) != (b.last > 0) {
		return a.last == 0 // rows with a last-used date go after the others
	}
	if a.last > 0 && a.last != b.last {
		return a.last < b.last
	}
	if a.size != b.size {
		return a.size > b.size
	}
	return a.item.Key()+a.view < b.item.Key()+b.view
}

// viewItems lists the visible items of a named view in a category, largest first.
func (m Model) viewItems(view, category string) []protocol.Item {
	var out []protocol.Item
	for _, it := range m.visibleItems() {
		if it.View == view && (category == "" || it.Category == category) {
			out = append(out, it)
		}
	}
	sortBySize(out)
	return out
}

// navRows flattens the groups into the rows the cursor can visit.
func (m Model) navRows() []row {
	var out []row
	for _, g := range m.groups() {
		out = append(out, g.rows...)
	}
	return out
}

func (m Model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	rows := m.navRows()
	if m.cursor >= len(rows) {
		m.cursor = max(len(rows)-1, 0)
	}
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "up", "k":
		m.cursor = max(m.cursor-1, 0)
	case "down", "j":
		m.cursor = min(m.cursor+1, max(len(rows)-1, 0))
	case " ", "space":
		if len(rows) > 0 && rows[m.cursor].view == "" {
			m.toggle(rows[m.cursor].item)
		}
	case "o":
		if len(rows) > 0 && len(rows[m.cursor].item.Paths) > 0 {
			_ = m.deps.Reveal(rows[m.cursor].item.Paths[0])
		}
	case "enter":
		if len(rows) > 0 && rows[m.cursor].view != "" {
			m.screen, m.view, m.viewCat, m.vcursor = screenView, rows[m.cursor].view, rows[m.cursor].category, 0
		} else if n, _ := m.selectedTotals(); n > 0 {
			m.screen = screenReview
		}
	}
	return m, nil
}

func (m Model) toggle(it protocol.Item) {
	if m.selected[it.Key()] {
		delete(m.selected, it.Key())
	} else {
		m.selected[it.Key()] = true
	}
}

func (m Model) updateView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.viewItems(m.view, m.viewCat)
	switch msg.String() {
	case "esc":
		m.screen = screenList
	case "up", "k":
		m.vcursor = max(m.vcursor-1, 0)
	case "down", "j":
		m.vcursor = min(m.vcursor+1, max(len(items)-1, 0))
	case " ", "space":
		if m.vcursor < len(items) {
			m.toggle(items[m.vcursor])
		}
	case "o":
		if m.vcursor < len(items) && len(items[m.vcursor].Paths) > 0 {
			_ = m.deps.Reveal(items[m.vcursor].Paths[0])
		}
	}
	return m, nil
}

var (
	bold  = lipgloss.NewStyle().Bold(true)
	faint = lipgloss.NewStyle().Faint(true)
	rev   = lipgloss.NewStyle().Reverse(true)
)

func (m Model) rule() string { return faint.Render(strings.Repeat("─", m.width)) }

func (m Model) freeText() string {
	if !m.hasFree || m.free.err != nil {
		return ""
	}
	return "free: " + HumanSize(int64(m.free.free)) + " of " + HumanSize(int64(m.free.total))
}

// twoCol puts right at the right edge of a line of the screen width.
func (m Model) twoCol(left, right string) string {
	gap := m.width - lipgloss.Width(left) - lipgloss.Width(right)
	return left + strings.Repeat(" ", max(gap, 1)) + right
}

func checkbox(on bool) string {
	if on {
		return "[x]"
	}
	return "[ ]"
}

func (m Model) listScreen() string {
	rows := m.navRows()
	cursor := min(m.cursor, max(len(rows)-1, 0))

	var body []string
	curLine := 0
	for _, g := range m.groups() {
		body = append(body, m.twoCol(bold.Render("▾ "+g.name), HumanSize(g.total)+"  "))
		for _, r := range g.rows {
			if len(rows) > 0 && sameRow(r, rows[cursor]) {
				curLine = len(body)
			}
			body = append(body, m.rowLine(r, len(rows) > 0 && sameRow(r, rows[cursor])))
		}
	}
	for _, f := range m.scanFailures() {
		body = append(body, "scan failed: "+f.Script+": "+firstLine(f.Err.Error()))
	}

	head := []string{
		m.twoCol(" "+bold.Render("mac-cleanup"), m.freeText()+" "),
		" [Space]",
	}
	if done := len(m.results); done < len(m.scripts) {
		head[1] += "   scanning " + itoa(done) + "/" + itoa(len(m.scripts))
	}
	head = append(head, m.rule())

	detail := []string{"", "", ""}
	if len(rows) > 0 {
		detail = m.detail(rows[cursor])
	}
	n, size := m.selectedTotals()
	foot := []string{m.rule()}
	foot = append(foot, detail...)
	foot = append(foot, m.rule(),
		"  selected: "+itoa(n)+" items, "+HumanSize(size)+"   "+faint.Render("space select  o finder  enter review/open  q quit"))

	return strings.Join(m.frame(head, body, foot, curLine), "\n")
}

func sameRow(a, b row) bool { return a.view == b.view && a.item.Key() == b.item.Key() }

// frame returns head, a window of body that keeps curLine visible, and foot.
func (m Model) frame(head, body, foot []string, curLine int) []string {
	avail := max(m.height-len(head)-len(foot), 3)
	start := 0
	if curLine >= avail {
		start = curLine - avail + 1
	}
	end := min(start+avail, len(body))
	out := append([]string{}, head...)
	out = append(out, body[start:end]...)
	return append(out, foot...)
}

func (m Model) rowLine(r row, cur bool) string {
	mark := "  "
	if cur {
		mark = "> "
	}
	if r.view != "" {
		return mark + "    " + r.view + "  (" + itoa(r.count) + " folders)  " + HumanSize(r.size) + "  enter to open"
	}
	it := r.item
	tail := "  " + gradeLabel(it.Grade)
	if len(it.Warnings) > 0 {
		tail += "  ⚠ " + it.Warnings[0]
	}
	size := HumanSize(it.Size)
	nameW := max(m.width-len(mark)-4-1-9-lipgloss.Width(tail), 8)
	line := mark + checkbox(m.selected[it.Key()]) + " " + fit(it.Name, nameW) + " " + padLeft(size, 9) + tail
	if cur {
		return rev.Render(line)
	}
	return line
}

func padLeft(s string, w int) string {
	return strings.Repeat(" ", max(w-lipgloss.Width(s), 0)) + s
}

// detail is the three-line pane for the row under the cursor.
func (m Model) detail(r row) []string {
	if r.view != "" {
		return []string{"  " + bold.Render(r.view), "  " + itoa(r.count) + " folders, press enter to choose among them.", ""}
	}
	it := r.item
	path := ""
	if len(it.Paths) > 0 {
		path = "   " + it.Paths[0]
	}
	return []string{
		"  " + bold.Render(it.Name) + path,
		"  " + fit(firstLine(it.Explain), m.width-2),
		"",
	}
}

func (m Model) viewScreen() string {
	items := m.viewItems(m.view, m.viewCat)
	var selN int
	var selSize int64
	for _, it := range items {
		if m.selected[it.Key()] {
			selN++
			selSize += it.Size
		}
	}
	cur := min(m.vcursor, max(len(items)-1, 0))
	var body []string
	for i, it := range items {
		mark := "  "
		if i == cur {
			mark = "> "
		}
		nameW := max(m.width-len(mark)-4-1-9, 8)
		line := mark + checkbox(m.selected[it.Key()]) + " " + fit(it.Name, nameW) + " " + padLeft(HumanSize(it.Size), 9)
		if i == cur {
			line = rev.Render(line)
		}
		body = append(body, line)
	}
	head := []string{
		m.twoCol(" "+bold.Render(m.view), "selected: "+itoa(selN)+" of "+itoa(len(items))+", "+HumanSize(selSize)+" "),
		m.rule(),
	}
	detail := []string{"", "", ""}
	if len(items) > 0 {
		detail = m.detail(row{item: items[cur]})
	}
	foot := append([]string{m.rule()}, detail...)
	foot = append(foot, m.rule(), "  "+faint.Render("up/down move  space select  o finder  esc back to list"))
	return strings.Join(m.frame(head, body, foot, cur), "\n")
}
