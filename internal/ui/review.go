package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mac-cleanup/internal/protocol"
	"mac-cleanup/internal/scan"
)

// recheckMsg carries the fresh scan of one script after a re-check.
type recheckMsg scan.Result

// runEntry is one confirmed item: exactly the command the review showed.
type runEntry struct {
	Item    protocol.Item
	Action  protocol.Action
	Command string
}

// startRunMsg is sent when the owner confirms the review. Task 13 handles it.
type startRunMsg struct{ entries []runEntry }

// startReview opens the review for the selected items in list order.
func (m *Model) startReview() {
	m.revKeys, m.ract, m.notes = nil, map[string]protocol.Action{}, nil
	m.rcurID, m.rcursor, m.revScroll = "", 0, 0
	add := func(it protocol.Item) {
		if m.selected[it.Key()] {
			m.revKeys = append(m.revKeys, it.Key())
			m.ract[it.Key()] = it.Default
		}
	}
	for _, g := range m.groups() {
		for _, r := range g.rows {
			if r.view == "" {
				add(r.item)
				continue
			}
			for _, it := range m.viewItems(r.view, r.category) {
				add(it)
			}
		}
	}
	m.screen = screenReview
}

// reviewItems resolves the review keys to current items; keys that no longer
// resolve are left out.
func (m Model) reviewItems() []protocol.Item {
	byKey := map[string]protocol.Item{}
	for _, it := range m.visibleItems() {
		byKey[it.Key()] = it
	}
	var out []protocol.Item
	for _, k := range m.revKeys {
		if it, ok := byKey[k]; ok {
			out = append(out, it)
		}
	}
	return out
}

func (m Model) reviewIndex(items []protocol.Item) int {
	for i, it := range items {
		if m.rcurID != "" && it.Key() == m.rcurID {
			return i
		}
	}
	return min(m.rcursor, max(len(items)-1, 0))
}

// chosen is the action for it, falling back to its default if the stored
// choice has no command any more.
func (m Model) chosen(it protocol.Item) protocol.Action {
	if a, ok := m.ract[it.Key()]; ok {
		if _, ok := it.Commands[a]; ok {
			return a
		}
	}
	return it.Default
}

func blockedItems(items []protocol.Item) []protocol.Item {
	var out []protocol.Item
	for _, it := range items {
		if len(it.Warnings) > 0 {
			out = append(out, it)
		}
	}
	return out
}

func (m Model) updateReview(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.running {
		return m, nil
	}
	switch msg.String() {
	case "pgdown", "pgup":
		l := m.reviewLayoutFor()
		d := max(l.room, 1)
		if msg.String() == "pgup" {
			d = -d
		}
		m.revScroll = l.clamp(m.revScroll + d)
		return m, nil
	}
	nm, cmd := m.updateReviewKey(msg)
	if mm := nm.(Model); mm.screen == screenReview {
		mm.revScroll = mm.reviewLayoutFor().ensureVisible(mm.revScroll)
		return mm, cmd
	}
	return nm, cmd
}

func (m Model) updateReviewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.reviewItems()
	cur := m.reviewIndex(items)
	setCur := func(i int) {
		m.rcursor = i
		if i < len(items) {
			m.rcurID = items[i].Key()
		}
	}
	setCur(cur)
	pick := func(a protocol.Action) {
		if cur < len(items) {
			if _, ok := items[cur].Commands[a]; ok {
				m.ract[items[cur].Key()] = a
			}
		}
	}
	step := func(d int) {
		if cur >= len(items) {
			return
		}
		acts := items[cur].Actions()
		for i, a := range acts {
			if a == m.chosen(items[cur]) && i+d >= 0 && i+d < len(acts) {
				m.ract[items[cur].Key()] = acts[i+d]
				return
			}
		}
	}
	switch msg.String() {
	case "esc":
		m.screen = screenList
	case "up", "k":
		setCur(max(cur-1, 0))
	case "down", "j":
		setCur(min(cur+1, max(len(items)-1, 0)))
	case "left":
		step(-1)
	case "right":
		step(1)
	case "d":
		pick(protocol.Delete)
	case "t":
		pick(protocol.Trash)
	case "s":
		if cur < len(items) {
			m.drop(items[cur].Key())
			m.notes = nil
		}
	case "r":
		return m.recheck(blockedItems(items))
	case "enter":
		if len(items) > 0 && len(blockedItems(items)) == 0 {
			entries := make([]runEntry, len(items))
			for i, it := range items {
				a := m.chosen(it)
				entries[i] = runEntry{Item: it, Action: a, Command: it.Commands[a]}
			}
			m.running = true
			return m, func() tea.Msg { return startRunMsg{entries} }
		}
	}
	return m, nil
}

// drop removes a key from the review and deselects it in the list.
func (m *Model) drop(key string) {
	kept := make([]string, 0, len(m.revKeys))
	for _, k := range m.revKeys {
		if k != key {
			kept = append(kept, k)
		}
	}
	m.revKeys = kept
	delete(m.selected, key)
	delete(m.ract, key)
}

// recheck re-runs the scripts of the warned items, one scan per script.
func (m Model) recheck(warned []protocol.Item) (tea.Model, tea.Cmd) {
	seen := map[string]bool{}
	var cmds []tea.Cmd
	for _, it := range warned {
		if seen[it.Script] {
			continue
		}
		seen[it.Script] = true
		script, scanFn := it.Script, m.deps.Scan
		cmds = append(cmds, func() tea.Msg { return recheckMsg(scanFn(script)) })
	}
	if len(cmds) == 0 {
		return m, nil
	}
	m.notes = nil
	return m, tea.Batch(cmds...)
}

// applyRecheck replaces the script's items by the fresh ones. Review items the
// script no longer reports are removed with a note. A failed scan changes
// nothing, so a blocked item stays blocked.
func (m *Model) applyRecheck(r scan.Result) {
	if r.Err != nil {
		m.notes = append(m.notes, "re-check of "+r.Script+" failed: "+firstLine(r.Err.Error()))
		return
	}
	old := map[string]protocol.Item{}
	for _, it := range m.visibleItems() {
		old[it.Key()] = it
	}
	replaced := false
	results := append([]scan.Result{}, m.results...)
	for i := range results {
		if results[i].Script == r.Script {
			results[i], replaced = r, true
		}
	}
	if !replaced {
		results = append(results, r)
	}
	m.pinCursors()
	m.results = results

	byKey := map[string]bool{}
	for _, it := range m.visibleItems() {
		byKey[it.Key()] = true
	}
	inScan := map[string]bool{}
	for _, it := range r.Items {
		inScan[it.Key()] = true
	}
	for _, k := range append([]string{}, m.revKeys...) {
		if it, was := old[k]; was && it.Script == r.Script && !byKey[k] {
			m.drop(k)
			if inScan[k] {
				m.notes = append(m.notes, it.Name+" is now below "+HumanSize(MinSize)+", removed")
			} else {
				m.notes = append(m.notes, it.Name+" no longer found")
			}
		}
	}
}

// hardWrap splits s at newlines and then into pieces of at most w display
// cells, keeping every character (spaces included). The first piece of a
// line gets first as prefix, later pieces get cont. w <= 0 disables wrapping.
func hardWrap(s string, w int, first, cont string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if w <= 0 {
			out = append(out, first+line)
			first = cont
			continue
		}
		pw := max(w-lipgloss.Width(first), 4)
		cw := max(w-lipgloss.Width(cont), 4)
		cur, curW, limit := "", 0, pw
		flush := func() {
			out = append(out, first+cur)
			first, cur, curW, limit = cont, "", 0, cw
		}
		for _, r := range line {
			rw := cellW(r)
			if curW+rw > limit && cur != "" {
				flush()
			}
			cur += string(r)
			curW += rw
		}
		flush()
	}
	return out
}

// cellW is the display width of r; a tab counts as 8 cells (worst case).
func cellW(r rune) int {
	if r == '\t' {
		return 8
	}
	return lipgloss.Width(string(r))
}

// clip cuts s to at most w display cells (w <= 0: unchanged).
func clip(s string, w int) string {
	if w <= 0 || lipgloss.Width(s) <= w {
		return s
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		if used+cellW(r) > w {
			break
		}
		b.WriteRune(r)
		used += cellW(r)
	}
	return b.String()
}

const (
	cmdFirst = "   "
	cmdCont  = "     "
)

// reviewLayout is the review screen split into a pinned title, a scrollable
// body, and a pinned footer. cs and ce are the body lines of the cursor
// item's row and the end of its block; room is how many body lines are
// shown at once (len(body) when everything fits or height is unlimited).
type reviewLayout struct {
	title, foot []string
	body        []string
	cs, ce      int
	room        int
}

func (m Model) reviewLayoutFor() reviewLayout {
	items := m.reviewItems()
	cur := m.reviewIndex(items)
	var size int64
	for _, it := range items {
		size += it.Size
	}
	title := " " + bold.Render("Review: "+plural(len(items), "item")+", "+HumanSize(size))
	if hint := "←/→ or d/t to switch "; lipgloss.Width(title)+lipgloss.Width(hint)+1 <= m.width {
		title = m.twoCol(title, hint)
	}

	var l reviewLayout
	l.title = []string{title}
	nameW := 0
	for _, o := range items {
		nameW = max(nameW, lipgloss.Width(o.Name))
	}
	var warns []string
	for i, it := range items {
		mark := "  "
		if i == cur {
			mark = "> "
			l.cs = len(l.body)
		}
		ch := m.choices(it)
		w := nameW
		if m.width > 0 {
			w = min(w, max(m.width-15-lipgloss.Width(ch), 4))
		}
		line := mark + fit(it.Name, max(w, 4)) + " " + padLeft(HumanSize(it.Size), 9) + "   " + ch
		if i == cur {
			line = rev.Render(clip(line, m.width))
		}
		l.body = append(l.body, line)
		l.body = append(l.body, hardWrap(it.Commands[m.chosen(it)], m.width, cmdFirst, cmdCont)...)
		if it.Admin {
			l.body = append(l.body, cmdFirst+"(admin password)")
		}
		if i == cur {
			l.ce = len(l.body) - 1
		}
		for _, w := range it.Warnings {
			warns = append(warns, hardWrap("⚠ "+w+": quit it and press r to re-check, or s to skip", m.width, " ", "   ")...)
		}
	}
	if len(items) == 0 {
		l.body = append(l.body, "  nothing left to review")
	}
	blocked := len(items) == 0 || len(warns) > 0
	extra := warns
	for _, n := range m.notes {
		extra = append(extra, hardWrap(n, m.width, " ", "   ")...)
	}
	run := "[ run ]"
	if blocked {
		run = "( run: blocked )"
	}
	pinned := []string{clip(m.twoCol("", run+"   [ back ]  "), m.width),
		clip("  "+faint.Render("up/down move  s skip  r re-check  enter run  esc back to list"), m.width)}

	// Warnings and notes stay pinned if at least 3 body lines fit beside them;
	// otherwise they scroll with the body so the view never exceeds the height.
	if m.height > 0 && m.height-len(l.title)-len(extra)-len(pinned) < 3 {
		if len(extra) > 0 {
			l.body = append(l.body, extra...)
		}
		extra = nil
	}
	l.foot = append(extra, pinned...)
	l.room = len(l.body)
	if m.height > 0 {
		avail := max(m.height-len(l.title)-len(l.foot), 1)
		if len(l.body) > avail {
			l.room = max(avail-2, 1) // two lines reserved for the more markers
		}
	}
	return l
}

func (l reviewLayout) clamp(scroll int) int {
	return max(min(scroll, len(l.body)-l.room), 0)
}

// ensureVisible moves scroll just enough to show the cursor item's block, or
// at least its row if the block is taller than the room.
func (l reviewLayout) ensureVisible(scroll int) int {
	scroll = l.clamp(scroll)
	if l.ce-l.cs+1 <= l.room {
		scroll = min(max(scroll, l.ce-l.room+1), l.cs)
	} else if scroll > l.cs || scroll+l.room <= l.cs {
		scroll = l.cs
	}
	return l.clamp(scroll)
}

func (m Model) reviewScreen() string {
	l := m.reviewLayoutFor()
	start := l.clamp(m.revScroll)
	end := min(start+l.room, len(l.body))
	out := append([]string{}, l.title...)
	if start > 0 {
		out = append(out, faint.Render("  ↑ more (pgup)"))
	}
	out = append(out, l.body[start:end]...)
	if end < len(l.body) {
		out = append(out, faint.Render("  ↓ more (pgdn)"))
	}
	return strings.Join(append(out, l.foot...), "\n")
}

// choices renders the action radio for one item; a single action has no choice.
func (m Model) choices(it protocol.Item) string {
	acts := it.Actions()
	if len(acts) == 1 {
		return "(•) " + string(acts[0])
	}
	var parts []string
	for _, a := range acts {
		dot := "( )"
		if a == m.chosen(it) {
			dot = "(•)"
		}
		parts = append(parts, dot+" "+string(a))
	}
	return strings.Join(parts, "  ")
}
