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
	m.rcurID, m.rcursor = "", 0
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
			rw := lipgloss.Width(string(r))
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

// clip cuts s to at most w display cells (w <= 0: unchanged).
func clip(s string, w int) string {
	if w <= 0 || lipgloss.Width(s) <= w {
		return s
	}
	r := []rune(s)
	for lipgloss.Width(string(r)) > w {
		r = r[:len(r)-1]
	}
	return string(r)
}

const (
	cmdFirst = "   "
	cmdCont  = "     "
)

func (m Model) reviewScreen() string {
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

	var body, foot []string
	cs, ce := 0, 0
	for i, it := range items {
		mark := "  "
		if i == cur {
			mark = "> "
			cs = len(body)
		}
		ch := m.choices(it)
		nameW := 0
		for _, o := range items {
			nameW = max(nameW, lipgloss.Width(o.Name))
		}
		if m.width > 0 {
			nameW = min(nameW, max(m.width-15-lipgloss.Width(ch), 4))
		}
		line := mark + fit(it.Name, max(nameW, 4)) + " " + padLeft(HumanSize(it.Size), 9) + "   " + ch
		if i == cur {
			line = rev.Render(clip(line, m.width))
		}
		body = append(body, line)
		body = append(body, hardWrap(it.Commands[m.chosen(it)], m.width, cmdFirst, cmdCont)...)
		if it.Admin {
			body = append(body, cmdFirst+"(admin password)")
		}
		if i == cur {
			ce = len(body) - 1
		}
		for _, w := range it.Warnings {
			foot = append(foot, hardWrap("⚠ "+w+": quit it and press r to re-check, or s to skip", m.width, " ", "   ")...)
		}
	}
	if len(items) == 0 {
		body = append(body, "  nothing left to review")
	}
	blocked := len(items) == 0 || len(foot) > 0
	for _, n := range m.notes {
		foot = append(foot, hardWrap(n, m.width, " ", "   ")...)
	}
	run := "[ run ]"
	if blocked {
		run = "( run: blocked )"
	}
	foot = append(foot, clip(m.twoCol("", run+"   [ back ]  "), m.width),
		clip("  "+faint.Render("up/down move  s skip  r re-check  enter run  esc back to list"), m.width))

	out := []string{title}
	if m.height > 0 {
		body = window(body, cs, ce, max(m.height-1-len(foot), 3))
	}
	out = append(out, body...)
	return strings.Join(append(out, foot...), "\n")
}

// window shows avail lines of body that include lines cs..ce (the cursor's
// block, as far as it fits) and marks hidden content with arrows.
func window(body []string, cs, ce, avail int) []string {
	if len(body) <= avail {
		return body
	}
	start, end := 0, 0
	room := avail
	for range 3 {
		start = max(0, min(cs, ce-room+1))
		end = min(start+room, len(body))
		room = avail
		if start > 0 {
			room--
		}
		if end < len(body) {
			room--
		}
	}
	start = max(0, min(cs, ce-room+1))
	end = min(start+room, len(body))
	var out []string
	if start > 0 {
		out = append(out, faint.Render("  ↑ more"))
	}
	out = append(out, body[start:end]...)
	if end < len(body) {
		out = append(out, faint.Render("  ↓ more"))
	}
	return out
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
