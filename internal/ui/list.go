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
}

type group struct {
	name  string
	total int64
	rows  []row
}

// groups builds the categories of the showing tab.
func (m Model) groups() []group { return m.groupsFor(m.tab) }

// groupsFor builds categories from the listed items of tab t.
func (m Model) groupsFor(t tab) []group {
	byCat := map[string]*group{}
	var order []string
	for _, it := range m.itemsOf(t) {
		g, ok := byCat[it.Category]
		if !ok {
			g = &group{name: it.Category}
			byCat[it.Category] = g
			order = append(order, it.Category)
		}
		g.total += amount(it)
		if it.View == "" {
			g.rows = append(g.rows, row{item: it, size: amount(it)})
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

// rowLess sorts rows by size (memory on the Autostart tab), largest first,
// then by key; Autostart rows go by name before the key.
func rowLess(a, b row) bool {
	if a.size != b.size {
		return a.size > b.size
	}
	if a.item.Autostart() && b.item.Autostart() && a.item.Name != b.item.Name {
		return a.item.Name < b.item.Name
	}
	return a.item.Key()+a.view < b.item.Key()+b.view
}

// viewItems lists the listed Space items of a named view in a category,
// largest first.
func (m Model) viewItems(view, category string) []protocol.Item {
	var out []protocol.Item
	for _, it := range m.itemsOf(tabSpace) {
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

func rowID(r row) string {
	if r.view != "" {
		return "view\x00" + r.category + "\x00" + r.view
	}
	return r.item.Key()
}

// listIndex resolves the cursor to an index in rows: the remembered row if it
// still exists, otherwise the old index clamped.
func (m Model) listIndex(rows []row) int {
	for i, r := range rows {
		if m.curID != "" && rowID(r) == m.curID {
			return i
		}
	}
	return min(m.cursor, max(len(rows)-1, 0))
}

func (m Model) viewIndex(items []protocol.Item) int {
	for i, it := range items {
		if m.vcurID != "" && it.Key() == m.vcurID {
			return i
		}
	}
	return min(m.vcursor, max(len(items)-1, 0))
}

// pinCursors fixes both cursors to the rows they are on now, so that a later
// re-sort of the lists (new scan results) does not move them.
func (m *Model) pinCursors() {
	if rows := m.navRows(); len(rows) > 0 {
		m.cursor = m.listIndex(rows)
		m.curID = rowID(rows[m.cursor])
	}
	if items := m.viewItems(m.view, m.viewCat); len(items) > 0 {
		m.vcursor = m.viewIndex(items)
		m.vcurID = items[m.vcursor].Key()
	}
}

func (m *Model) setListCursor(rows []row, i int) {
	m.cursor = i
	if i < len(rows) {
		m.curID = rowID(rows[i])
	}
}

func (m Model) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	rows := m.navRows()
	cur := m.listIndex(rows)
	m.setListCursor(rows, cur)
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "up", "k":
		m.setListCursor(rows, max(cur-1, 0))
	case "down", "j":
		m.setListCursor(rows, min(cur+1, max(len(rows)-1, 0)))
	case " ", "space":
		if len(rows) > 0 && rows[cur].view == "" {
			m.toggle(rows[cur].item)
		}
	case "tab":
		m.switchTab(1 - m.tab)
	case "1":
		m.switchTab(tabSpace)
	case "2":
		m.switchTab(tabAutostart)
	case "o":
		if len(rows) > 0 && len(rows[cur].item.Paths) > 0 {
			_ = m.deps.Reveal(rows[cur].item.Paths[0])
		}
	case "enter":
		if len(rows) > 0 && rows[cur].view != "" {
			m.screen, m.view, m.viewCat = screenView, rows[cur].view, rows[cur].category
			m.vcursor, m.vcurID = 0, ""
		} else if n, _, _ := m.selectedTotals(); n > 0 {
			m.startReview()
		}
	}
	return m, nil
}

func (m Model) toggle(it protocol.Item) {
	if len(it.Actions()) == 0 {
		return
	}
	if m.selected[it.Key()] {
		delete(m.selected, it.Key())
	} else {
		m.selected[it.Key()] = true
	}
}

func (m Model) updateView(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.viewItems(m.view, m.viewCat)
	cur := m.viewIndex(items)
	setCur := func(i int) {
		m.vcursor = i
		if i < len(items) {
			m.vcurID = items[i].Key()
		}
	}
	setCur(cur)
	switch msg.String() {
	case "esc":
		m.screen = screenList
	case "up", "k":
		setCur(max(cur-1, 0))
	case "down", "j":
		setCur(min(cur+1, max(len(items)-1, 0)))
	case " ", "space":
		if cur < len(items) {
			m.toggle(items[cur])
		}
	case "o":
		if cur < len(items) && len(items[cur].Paths) > 0 {
			_ = m.deps.Reveal(items[cur].Paths[0])
		}
	}
	return m, nil
}

var (
	bold  = lipgloss.NewStyle().Bold(true)
	faint = lipgloss.NewStyle().Faint(true)
	rev   = lipgloss.NewStyle().Reverse(true)

	// The colours are the terminal's own ANSI colours, so they follow its
	// theme; NO_COLOR turns them off. Cursor rows stay plain reverse video.
	heading = bold.Foreground(lipgloss.Color("6"))
	good    = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	caution = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
	danger  = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
)

// paint renders s in st; an empty s stays empty.
func paint(st lipgloss.Style, s string) string {
	if s == "" {
		return ""
	}
	return st.Render(s)
}

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

// box is the checkbox, green when on, plain on the cursor row.
func box(on, cur bool) string {
	if on && !cur {
		return good.Render(checkbox(on))
	}
	return checkbox(on)
}

func (m Model) listScreen() string {
	rows := m.navRows()
	cursor := m.listIndex(rows)

	var body []string
	curLine := 0
	for _, g := range m.groups() {
		body = append(body, m.twoCol(heading.Render("▾ "+Visible(g.name)), HumanSize(g.total)+"  "))
		for _, r := range g.rows {
			if len(rows) > 0 && sameRow(r, rows[cursor]) {
				curLine = len(body)
			}
			body = append(body, m.rowLine(r, len(rows) > 0 && sameRow(r, rows[cursor])))
		}
	}

	right := m.freeText()
	if m.tab == tabAutostart {
		right = m.runningText()
	}
	head := []string{
		m.twoCol(" "+heading.Render("mac-cleanup"), right+" "),
		m.tabBar(),
	}
	if done := len(m.results); done < len(m.scripts) {
		head[1] += "   scanning " + itoa(done) + "/" + itoa(len(m.scripts)) + " " + progressBar(done, len(m.scripts), 20)
	}
	head = append(head, m.rule())

	detail := make([]string, detailLines)
	if len(rows) > 0 {
		detail = m.detail(rows[cursor])
	}
	n, disk, memory := m.selectedTotals()
	sel := "  selected: " + plural(n, "item")
	if disk > 0 {
		sel += ", " + HumanSize(disk) + " disk"
	}
	if memory > 0 {
		sel += ", " + HumanSize(memory) + " memory"
	}
	var foot []string
	for _, f := range m.scanFailures() {
		foot = append(foot, danger.Render(Visible("scan failed: "+f.Script+": "+firstLine(f.Err.Error()))))
	}
	foot = append(foot, m.rule())
	foot = append(foot, detail...)
	foot = append(foot, m.rule(), sel,
		"  "+faint.Render("space select  tab switch  o finder  enter review/open  q quit"))

	return strings.Join(m.frame(head, body, foot, curLine), "\n")
}

// progressBar returns a bar of width cells, filled for done of total.
func progressBar(done, total, width int) string {
	filled := 0
	if total > 0 {
		filled = min(done*width/total, width)
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
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
		return mark + "    " + Visible(r.view) + "  (" + plural(r.count, "folder") + ")  " + HumanSize(r.size) + "  enter to open"
	}
	it := r.item
	if it.Autostart() {
		return m.autostartLine(it, cur)
	}
	grade, warn := gradeLabel(it.Grade), ""
	if len(it.Warnings) > 0 {
		warn = "  ⚠ " + Visible(it.Warnings[0])
	}
	tail := "  " + grade + warn
	nameW := max(m.width-len(mark)-4-1-9-lipgloss.Width(tail), 8)
	line := mark + box(m.selected[it.Key()], cur) + " " + fit(Visible(it.Name), nameW) + " " + padLeft(HumanSize(it.Size), 9)
	if cur {
		return rev.Render(line + tail)
	}
	return line + "  " + gradeStyle(it.Grade).Render(grade) + paint(caution, warn)
}

func padLeft(s string, w int) string {
	return strings.Repeat(" ", max(w-lipgloss.Width(s), 0)) + s
}

// detailLines is the fixed height of the detail pane.
const detailLines = 4

// detail is the pane for the row under the cursor: name and path, then the
// explanation word-wrapped over the remaining lines.
func (m Model) detail(r row) []string {
	out := make([]string, detailLines)
	if r.view != "" {
		out[0] = "  " + heading.Render(Visible(r.view))
		out[1] = "  " + plural(r.count, "folder") + ", press enter to choose among them."
		return out
	}
	it := r.item
	path := ""
	if len(it.Paths) > 0 {
		path = "   " + Visible(it.Paths[0])
	}
	out[0] = "  " + heading.Render(Visible(it.Name)) + path
	for i, l := range wrap(Visible(it.Explain), m.width-2, detailLines-1) {
		out[i+1] = "  " + l
	}
	return out
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
	cur := m.viewIndex(items)
	var body []string
	for i, it := range items {
		mark := "  "
		if i == cur {
			mark = "> "
		}
		nameW := max(m.width-len(mark)-4-1-9, 8)
		line := mark + box(m.selected[it.Key()], i == cur) + " " + fit(Visible(it.Name), nameW) + " " + padLeft(HumanSize(it.Size), 9)
		if i == cur {
			line = rev.Render(line)
		}
		body = append(body, line)
	}
	head := []string{
		m.twoCol(" "+heading.Render(Visible(m.view)), "selected: "+itoa(selN)+" of "+itoa(len(items))+", "+HumanSize(selSize)+" "),
		m.rule(),
	}
	detail := make([]string, detailLines)
	if len(items) > 0 {
		detail = m.detail(row{item: items[cur]})
	}
	foot := append([]string{m.rule()}, detail...)
	foot = append(foot, m.rule(), "  "+faint.Render("up/down move  space select  o finder  esc back to list"))
	return strings.Join(m.frame(head, body, foot, cur), "\n")
}
