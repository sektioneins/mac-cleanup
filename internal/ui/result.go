package ui

import (
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"mac-cleanup/internal/action"
	"mac-cleanup/internal/protocol"
)

// sudoMsg reports the end of the admin password prompt.
type sudoMsg struct{ err error }

// outcomeMsg carries the outcome of one finished run.
type outcomeMsg action.Outcome

var errNoAdmin = errors.New("admin password not given")

// runState is the progress of a confirmed run. entries is the snapshot taken
// by the review; commands always come from it, never from live state.
type runState struct {
	entries []runEntry
	outs    []*action.Outcome // parallel to entries; nil until done
	next    int               // index of the entry running or to run next
	sudo    bool              // waiting for the admin password

	freeBefore, freeAfter uint64
	beforeOK, afterOK     bool

	scroll int
}

// startRun measures free space, asks for admin rights if needed and starts.
func (m Model) startRun(msg startRunMsg) (tea.Model, tea.Cmd) {
	m.running, m.screen = true, screenRun
	m.run = runState{entries: msg.entries, outs: make([]*action.Outcome, len(msg.entries))}
	if f, _, err := m.deps.Free(); err == nil {
		m.run.freeBefore, m.run.beforeOK = f, true
	}
	for _, e := range msg.entries {
		if e.Item.Admin {
			m.run.sudo = true
			return m, tea.ExecProcess(m.deps.SudoCmd(), func(err error) tea.Msg { return sudoMsg{err} })
		}
	}
	return m.runNext()
}

func (m Model) afterSudo(msg sudoMsg) (tea.Model, tea.Cmd) {
	if m.screen != screenRun || !m.run.sudo {
		return m, nil
	}
	m.run.sudo = false
	if msg.err != nil {
		for i, e := range m.run.entries {
			if e.Item.Admin {
				m.run.outs[i] = &action.Outcome{Key: e.Item.Key(), Action: e.Action, Command: e.Command, Err: errNoAdmin}
			}
		}
	}
	return m.runNext()
}

func (m Model) afterOutcome(msg outcomeMsg) (tea.Model, tea.Cmd) {
	if m.screen != screenRun || m.run.sudo || m.run.next >= len(m.run.entries) {
		return m, nil
	}
	o := action.Outcome(msg)
	m.run.outs[m.run.next] = &o
	m.run.next++
	return m.runNext()
}

// runNext starts the next entry without an outcome, or finishes the run.
func (m Model) runNext() (tea.Model, tea.Cmd) {
	r := &m.run
	for r.next < len(r.entries) && r.outs[r.next] != nil {
		r.next++
	}
	if r.next >= len(r.entries) {
		if f, _, err := m.deps.Free(); err == nil {
			r.freeAfter, r.afterOK = f, true
		}
		m.screen = screenResult
		return m, nil
	}
	e, run := r.entries[r.next], m.deps.Run
	return m, func() tea.Msg { return outcomeMsg(run(e.Item, e.Action)) }
}

func (m Model) runScreen() string {
	r := m.run
	line := "  starting..."
	switch {
	case r.sudo:
		line = "  waiting for the admin password..."
	case r.next < len(r.entries):
		line = "  running " + itoa(r.next+1) + "/" + itoa(len(r.entries)) + ": " + r.entries[r.next].Item.Name
	}
	return " " + bold.Render("Running") + "\n\n" + clip(line, m.width)
}

func (m Model) updateResult(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	lines := m.resultLines()
	room := max(m.height-2, 1)
	maxScroll := max(len(lines)-room, 0)
	d := 0
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "up", "k":
		d = -1
	case "down", "j":
		d = 1
	case "pgup":
		d = -room
	case "pgdown":
		d = room
	}
	m.run.scroll = max(min(m.run.scroll+d, maxScroll), 0)
	return m, nil
}

func (m Model) resultLines() []string {
	r := m.run
	free := func(ok bool, v uint64) string {
		if !ok {
			return "unknown"
		}
		return HumanSize(int64(v))
	}
	l := "Free before: " + free(r.beforeOK, r.freeBefore) + "   after: " + free(r.afterOK, r.freeAfter)
	if r.beforeOK && r.afterOK {
		gained := int64(0)
		if r.freeAfter > r.freeBefore {
			gained = int64(r.freeAfter - r.freeBefore)
		}
		l += "   gained: " + HumanSize(gained)
	}
	out := []string{" " + l}

	var trashed int64
	var failed []int
	for i, o := range r.outs {
		switch {
		case o == nil:
		case o.Err != nil:
			failed = append(failed, i)
		case o.Action == protocol.Trash:
			trashed += r.entries[i].Item.Size
		}
	}
	if trashed > 0 {
		out = append(out, " Moved to Trash: "+HumanSize(trashed)+", freed once you empty the Trash")
	}
	if len(failed) > 0 {
		out = append(out, "", " "+bold.Render(plural(len(failed), "item")+" failed"))
	}
	for _, i := range failed {
		e, o := r.entries[i], r.outs[i]
		out = append(out, "", "  "+clip(e.Item.Name, m.width-2))
		out = append(out, hardWrap(e.Command, m.width, "   $ ", "     ")...)
		out = append(out, hardWrap("error: "+o.Err.Error(), m.width, "   ", "     ")...)
		if s := strings.TrimRight(o.Output, "\n"); s != "" {
			out = append(out, hardWrap(s, m.width, "   ", "   ")...)
		}
	}
	return out
}

func (m Model) resultScreen() string {
	lines := m.resultLines()
	room := max(m.height-2, 1)
	start := max(min(m.run.scroll, len(lines)-room), 0)
	end := min(start+room, len(lines))
	hint := "q quit"
	if len(lines) > room {
		hint = "up/down pgup/pgdn scroll  q quit"
	}
	out := append([]string{" " + bold.Render("Done")}, lines[start:end]...)
	return strings.Join(append(out, "  "+faint.Render(hint)), "\n")
}
