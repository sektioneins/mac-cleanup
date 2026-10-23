package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mac-cleanup/internal/protocol"
	"mac-cleanup/internal/scan"
)

// recheckMsg carries the fresh scan of one script after a re-check.
type recheckMsg scan.Result

// startRunMsg is sent when the owner confirms the review. Task 13 handles it.
type startRunMsg struct{}

// startReview opens the review for the selected items in list order.
func (m *Model) startReview() {
	m.rev, m.ract, m.notes = nil, map[string]protocol.Action{}, nil
	m.rcurID, m.rcursor = "", 0
	add := func(it protocol.Item) {
		if m.selected[it.Key()] {
			m.rev = append(m.rev, it.Key())
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
	for _, k := range m.rev {
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
			return m, func() tea.Msg { return startRunMsg{} }
		}
	}
	return m, nil
}

// drop removes a key from the review and deselects it in the list.
func (m *Model) drop(key string) {
	kept := make([]string, 0, len(m.rev))
	for _, k := range m.rev {
		if k != key {
			kept = append(kept, k)
		}
	}
	m.rev = kept
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
		script, scan := it.Script, m.deps.Scan
		cmds = append(cmds, func() tea.Msg { return recheckMsg(scan(script)) })
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
	for _, k := range append([]string{}, m.rev...) {
		if it, was := old[k]; was && it.Script == r.Script && !byKey[k] {
			m.drop(k)
			m.notes = append(m.notes, it.Name+" no longer found")
		}
	}
}

func (m Model) reviewScreen() string {
	items := m.reviewItems()
	cur := m.reviewIndex(items)
	var size int64
	for _, it := range items {
		size += it.Size
	}
	lines := []string{
		m.twoCol(" "+bold.Render("Review: "+plural(len(items), "item")+", "+HumanSize(size)), "←/→ or d/t to switch "),
	}
	nameW := 8
	for _, it := range items {
		nameW = max(nameW, len([]rune(it.Name)))
	}
	nameW = min(nameW, max(m.width-40, 8))
	var cmds, warns []string
	for i, it := range items {
		mark := "  "
		if i == cur {
			mark = "> "
		}
		line := mark + fit(it.Name, nameW) + " " + padLeft(HumanSize(it.Size), 9) + "   " + m.choices(it)
		if i == cur {
			line = rev.Render(line)
		}
		lines = append(lines, line)
		c := "   " + it.Commands[m.chosen(it)]
		if it.Admin {
			c += "     (admin password)"
		}
		cmds = append(cmds, c)
		for _, w := range it.Warnings {
			warns = append(warns, " ⚠ "+w+": quit it and press r to re-check, or s to skip")
		}
	}
	if len(items) == 0 {
		lines = append(lines, "  nothing left to review")
	}
	lines = append(lines, "")
	lines = append(lines, cmds...)
	lines = append(lines, warns...)
	for _, n := range m.notes {
		lines = append(lines, " "+n)
	}
	run := "[ run ]"
	if len(items) == 0 || len(warns) > 0 {
		run = "( run: blocked )"
	}
	lines = append(lines, m.twoCol("", run+"   [ back ]  "),
		"  "+faint.Render("up/down move  s skip  r re-check  enter run  esc back to list"))
	return strings.Join(lines, "\n")
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
