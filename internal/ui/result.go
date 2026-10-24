package ui

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"mac-cleanup/internal/action"

	"mac-cleanup/internal/protocol"
)

// sudoMsg reports the end of the admin password prompt.
type sudoMsg struct{ err error }

// restoreError marks a failure to hand the terminal back, as opposed to sudo
// itself exiting non-zero.
type restoreError struct{ error }

// sudoDone turns the result of the sudo prompt into a sudoMsg.
func sudoDone(err error) tea.Msg {
	var ee *exec.ExitError
	if err != nil && !errors.As(err, &ee) {
		err = restoreError{err}
	}
	return sudoMsg{err}
}

// refreshMsg reports the non-interactive sudo ticket check before an admin item.
type refreshMsg struct{ err error }

// outcomeMsg carries the outcome of one finished run.
type outcomeMsg action.Outcome

// outputMsg carries a piece of a running command's output. ch is the stream
// it came from; the next piece is read from it.
type outputMsg struct {
	ch   chan tea.Msg
	text string
}

// chanWriter sends each write as an outputMsg. It blocks until the UI reads,
// so the outcome sent after the command ends always comes last.
type chanWriter chan tea.Msg

func (w chanWriter) Write(p []byte) (int, error) {
	w <- outputMsg{chan tea.Msg(w), string(p)}
	return len(p), nil
}

// listen waits for the next message of a stream.
func listen(ch chan tea.Msg) tea.Cmd { return func() tea.Msg { return <-ch } }

type segKind int

const (
	segItem segKind = iota // item name and action
	segCmd                 // the command as run
	segOut                 // command output, unsanitised
	segErr                 // the error of a failed command
)

// seg is one piece of the run transcript.
type seg struct {
	kind segKind
	text string
}

var errNoAdmin = errors.New("admin password not given")

// runState is the progress of a confirmed run. entries is the snapshot taken
// by the review; commands always come from it, never from live state.
type runState struct {
	entries []runEntry
	outs    []*action.Outcome // parallel to entries; nil until done
	next    int               // index of the entry running or to run next

	sudo       bool   // sudo prompt is up
	refreshing bool   // ticket check is running
	fresh      bool   // the ticket was just primed: no check before the next item
	inflight   bool   // a Deps.Run is in progress
	stopping   bool   // stop after the in-flight item; start nothing new
	refused    string // non-empty: the password was refused, waiting for the choice
	reason     string // why sudo failed, when it was not simply refused
	cancel     context.CancelFunc

	freeBefore, freeAfter uint64
	beforeOK, afterOK     bool

	segs    []seg        // transcript of the commands and their output
	stream  chan tea.Msg // output and outcome of the in-flight item
	started time.Time    // start of the in-flight item

	log     io.WriteCloser // nil when it could not be opened
	logPath string
	logErr  error // first failure to open or write the log

	scroll int
}

// logf writes to the run log; the first failure stops further writes.
func (r *runState) logf(format string, a ...any) {
	if r.log == nil || r.logErr != nil {
		return
	}
	if _, err := fmt.Fprintf(r.log, format, a...); err != nil {
		r.logErr = err
	}
}

// startRun measures free space, asks for admin rights if needed and starts.
func (m Model) startRun(msg startRunMsg) (tea.Model, tea.Cmd) {
	m.running, m.screen = true, screenRun
	m.run = runState{entries: msg.entries, outs: make([]*action.Outcome, len(msg.entries))}
	if f, _, err := m.deps.Free(); err == nil {
		m.run.freeBefore, m.run.beforeOK = f, true
	}
	m.run.log, m.run.logPath, m.run.logErr = m.deps.OpenLog()
	if m.run.logErr != nil {
		m.run.log = nil
	}
	m.run.logf("mac-cleanup run %s\n", time.Now().Format(time.RFC3339))
	for _, e := range msg.entries {
		if e.Item.Admin {
			return m.prompt()
		}
	}
	return m.advance()
}

// prompt suspends the interface for the sudo password prompt.
func (m Model) prompt() (Model, tea.Cmd) {
	m.run.sudo = true
	return m, tea.ExecProcess(m.deps.SudoCmd(), sudoDone)
}

func (m Model) afterSudo(msg sudoMsg) (tea.Model, tea.Cmd) {
	r := &m.run
	if m.screen != screenRun || !r.sudo {
		return m, nil
	}
	r.sudo = false
	if msg.err == nil || r.stopping {
		r.fresh = msg.err == nil
		return m.advance()
	}
	r.reason = ""
	var re restoreError
	if errors.As(msg.err, &re) {
		r.reason = "sudo could not be started or the terminal could not be restored: " + re.Error()
	}
	adm, other := m.remaining()
	if other == 0 {
		return m.skipAdmin()
	}
	r.refused = "Admin password not given: " + plural(adm, "admin item") + " will be skipped.\n" +
		"enter runs the other " + itoa(other) + ", q stops"
	if r.reason != "" {
		r.refused = r.reason + "\n" + r.refused
	}
	return m, nil
}

// remaining counts the entries not yet run, split into admin and other.
func (m Model) remaining() (adm, other int) {
	for i, e := range m.run.entries {
		switch {
		case m.run.outs[i] != nil:
		case e.Item.Admin:
			adm++
		default:
			other++
		}
	}
	return
}

// skipAdmin gives every admin entry not yet run the refused-password outcome
// and carries on with the others.
func (m Model) skipAdmin() (tea.Model, tea.Cmd) {
	r := &m.run
	r.refused = ""
	err := errNoAdmin
	if r.reason != "" {
		err = fmt.Errorf("%w (%s)", errNoAdmin, r.reason)
	}
	for i, e := range r.entries {
		if e.Item.Admin && r.outs[i] == nil {
			r.outs[i] = &action.Outcome{Key: e.Item.Key(), Action: e.Action, Command: e.Command, Err: err}
			r.logf("\n== %s (%s)\n$ %s\n-- skipped: %v\n", e.Item.Name, e.Action, e.Command, err)
		}
	}
	return m.advance()
}

func (m Model) afterRefresh(msg refreshMsg) (tea.Model, tea.Cmd) {
	r := &m.run
	if m.screen != screenRun || !r.refreshing {
		return m, nil
	}
	r.refreshing = false
	if msg.err == nil || r.stopping {
		r.fresh = msg.err == nil
		return m.advance()
	}
	return m.prompt()
}

func (m Model) afterOutcome(msg outcomeMsg) (tea.Model, tea.Cmd) {
	r := &m.run
	if m.screen != screenRun || !r.inflight || r.next >= len(r.entries) {
		return m, nil
	}
	o := action.Outcome(msg)
	r.outs[r.next] = &o
	took := time.Since(r.started).Round(100 * time.Millisecond)
	if o.Err != nil {
		r.segs = append(r.segs, seg{segErr, o.Err.Error()})
		r.logf("\n-- failed: %v (%s)\n", o.Err, took)
	} else {
		r.logf("\n-- ok (%s)\n", took)
	}
	r.next++
	r.inflight, r.stream = false, nil
	if r.cancel != nil {
		r.cancel()
		r.cancel = nil
	}
	return m.advance()
}

// advance starts the next entry without an outcome, or finishes the run.
func (m Model) advance() (Model, tea.Cmd) {
	r := &m.run
	for r.next < len(r.entries) && r.outs[r.next] != nil {
		r.next++
	}
	if r.stopping || r.next >= len(r.entries) {
		if r.inflight {
			return m, nil
		}
		return m.finish()
	}
	e := r.entries[r.next]
	if e.Item.Admin && !r.fresh {
		r.refreshing = true
		refresh := m.deps.SudoRefresh
		return m, func() tea.Msg { return refreshMsg{refresh()} }
	}
	r.fresh, r.inflight = false, true
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	r.segs = append(r.segs, seg{segItem, e.Item.Name + " (" + string(e.Action) + ")"}, seg{segCmd, e.Command})
	r.logf("\n== %s (%s)\n$ %s\n", e.Item.Name, e.Action, e.Command)
	r.started = time.Now()
	ch := make(chan tea.Msg)
	r.stream = ch
	run := m.deps.Run
	return m, func() tea.Msg {
		go func() { ch <- outcomeMsg(run(ctx, e.Item, e.Action, chanWriter(ch))) }()
		return <-ch
	}
}

// afterOutput adds a piece of output to the transcript and the log. The
// stream is always read on, so a command never blocks on its output.
func (m Model) afterOutput(msg outputMsg) (tea.Model, tea.Cmd) {
	r := &m.run
	if m.screen == screenRun && r.inflight && msg.ch == r.stream {
		if n := len(r.segs); n > 0 && r.segs[n-1].kind == segOut {
			r.segs[n-1].text += msg.text
		} else {
			r.segs = append(r.segs, seg{segOut, msg.text})
		}
		r.logf("%s", msg.text)
	}
	return m, listen(msg.ch)
}

func (m Model) finish() (Model, tea.Cmd) {
	if f, _, err := m.deps.Free(); err == nil {
		m.run.freeAfter, m.run.afterOK = f, true
	}
	m.screen = screenResult
	m.run.scroll = 1 << 30 // start at the end
	r := &m.run
	if r.log != nil {
		r.logf("\n%s\n", ansi.Strip(m.resultText()))
		if err := r.log.Close(); err != nil && r.logErr == nil {
			r.logErr = err
		}
	}
	return m, nil
}

// updateRun handles keys while running: ctrl+c, q and esc stop after the
// current item, a second ctrl+c interrupts it. The program never quits here.
func (m Model) updateRun(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	r := &m.run
	key := msg.String()
	stop := key == "ctrl+c" || key == "q" || key == "esc"
	switch {
	case r.refused != "":
		switch {
		case key == "enter":
			return m.skipAdmin()
		case stop:
			r.refused, r.stopping = "", true
			return m.advance()
		}
	case stop && !r.stopping:
		r.stopping = true
	case key == "ctrl+c" && r.cancel != nil && !r.entries[r.next].Item.Admin:
		r.cancel()
	}
	return m, nil
}

func (m Model) runScreen() string {
	r := m.run
	// Lines are wrapped as plain text and styled afterwards, so a wrap can
	// never split an escape sequence.
	type line struct {
		text  string
		style lipgloss.Style
	}
	plain := lipgloss.NewStyle()
	var lines []line
	add := func(st lipgloss.Style, texts ...string) {
		for _, t := range texts {
			lines = append(lines, line{t, st})
		}
	}
	name := ""
	if r.next < len(r.entries) {
		name = Visible(r.entries[r.next].Item.Name)
	}
	switch {
	case r.refused != "":
		for _, l := range strings.Split(r.refused, "\n") {
			add(caution, "  "+l)
		}
	case r.stopping && r.inflight && r.entries[r.next].Item.Admin:
		add(plain, "  stopping after "+name+"… (admin command, cannot be interrupted, waiting for it to finish)")
	case r.stopping && r.inflight:
		add(plain, "  stopping after "+name+"…")
		add(faint, "  ctrl+c again interrupts it")
	case r.stopping:
		add(plain, "  stopping…")
	case r.sudo:
		add(plain, "  waiting for the admin password...")
	case r.next < len(r.entries):
		add(plain, "  running "+itoa(r.next+1)+"/"+itoa(len(r.entries))+": "+name)
		add(faint, "  ctrl+c stops after this item")
	}
	var foot []string
	for _, l := range lines {
		for _, w := range hardWrap(l.text, m.width, "", "    ") {
			if w != "" {
				w = l.style.Render(w)
			}
			foot = append(foot, w)
		}
	}
	done := 0
	for _, o := range r.outs {
		if o != nil {
			done++
		}
	}
	head := paintAll(heading, hardWrap(" Running "+progressBar(done, len(r.entries), 20), m.width, "", " "))
	body := m.transcriptLines()
	room := max(m.height-len(head)-len(foot)-1, 1)
	if len(body) > room {
		body = body[len(body)-room:] // follow the newest output
	}
	out := append(head, body...)
	out = append(out, "")
	return strings.Join(append(out, foot...), "\n")
}

// transcriptLines renders the commands run so far and their output. Lines are
// wrapped as plain text and styled afterwards.
func (m Model) transcriptLines() []string {
	var out []string
	for _, sg := range m.run.segs {
		switch sg.kind {
		case segItem:
			out = append(out, paintAll(heading, hardWrap("── "+Visible(sg.text), m.width, " ", "    "))...)
		case segCmd:
			out = append(out, hardWrap(Visible(sg.text), m.width, " $ ", "   ")...)
		case segOut:
			if t := strings.TrimRight(sanitize(sg.text), "\n"); t != "" {
				out = append(out, paintAll(faint, hardWrap(t, m.width, " ", " "))...)
			}
		case segErr:
			out = append(out, paintAll(danger, hardWrap("error: "+sanitize(sg.text), m.width, " ", "   "))...)
		}
	}
	return out
}

func (m Model) updateResult(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	lines := m.resultScreenLines()
	room := max(m.height-2, 1)
	maxScroll := max(len(lines)-room, 0)
	d := 0
	switch msg.String() {
	case "q":
		return m, tea.Quit
	case "r":
		return m.rescan()
	case "up", "k":
		d = -1
	case "down", "j":
		d = 1
	case "pgup":
		d = -room
	case "pgdown":
		d = room
	}
	m.run.scroll = max(min(min(m.run.scroll, maxScroll)+d, maxScroll), 0)
	return m, nil
}

// rescan goes back to an empty list and scans again. Nothing is selected.
func (m Model) rescan() (tea.Model, tea.Cmd) {
	nm := New(m.scripts, m.deps)
	nm.width, nm.height, nm.gen = m.width, m.height, m.gen+1
	return nm, nm.Init()
}

// resultText is the result body as one string.
func (m Model) resultText() string { return strings.Join(m.resultLines(), "\n") }

// sanitize makes command output safe to print: no escape sequences, CR as
// newline, no control characters except newline and tab.
func sanitize(s string) string {
	s = ansi.Strip(s)
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' || (r >= ' ' && r != 0x7f && !(r >= 0x80 && r < 0xa0)) {
			return r
		}
		return -1
	}, s)
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
		l += "   gained: " + good.Render(HumanSize(gained))
	}
	out := []string{" " + l}

	var trashed int64
	var failed []int
	for i, o := range r.outs {
		switch {
		case o == nil:
		case o.Err != nil:
			failed = append(failed, i)
		case r.entries[i].Action == protocol.Trash:
			trashed += r.entries[i].Item.Size
		}
	}
	if trashed > 0 {
		out = append(out, " Moved to Trash: "+HumanSize(trashed)+", freed once you empty the Trash")
	}
	if len(failed) > 0 {
		out = append(out, "", " "+danger.Bold(true).Render(plural(len(failed), "item")+" failed"))
	}
	for _, i := range failed {
		e, o := r.entries[i], r.outs[i]
		out = append(out, "", "  "+clip(Visible(e.Item.Name), m.width-2))
		out = append(out, hardWrap(Visible(e.Command), m.width, "   $ ", "     ")...)
		out = append(out, paintAll(danger, hardWrap("error: "+sanitize(o.Err.Error()), m.width, "   ", "     "))...)
		if s := strings.TrimRight(sanitize(o.Output), "\n"); s != "" {
			out = append(out, hardWrap(s, m.width, "   ", "   ")...)
		}
	}
	var notRun []string
	for i, o := range r.outs {
		if o == nil {
			notRun = append(notRun, "  "+clip(Visible(r.entries[i].Item.Name), m.width-2))
		}
	}
	if len(notRun) > 0 {
		out = append(out, "", " "+caution.Bold(true).Render("Not run:"))
		out = append(out, notRun...)
	}
	return out
}

// resultScreenLines is the transcript followed by the summary and the log.
func (m Model) resultScreenLines() []string {
	lines := m.transcriptLines()
	if len(lines) > 0 {
		lines = append(lines, "")
	}
	lines = append(lines, m.resultLines()...)
	r := m.run
	switch {
	case r.logErr != nil:
		lines = append(lines, "", paint(caution, " log could not be written: "+Visible(r.logErr.Error())))
	case r.logPath != "":
		lines = append(lines, "", " Log: "+Visible(r.logPath))
	}
	return lines
}

func (m Model) resultScreen() string {
	lines := m.resultScreenLines()
	room := max(m.height-2, 1)
	start := max(min(m.run.scroll, len(lines)-room), 0)
	end := min(start+room, len(lines))
	hint := "r rescan  q quit"
	if len(lines) > room {
		hint = "up/down pgup/pgdn scroll  r rescan  q quit"
	}
	out := append([]string{" " + heading.Render("Done")}, lines[start:end]...)
	return strings.Join(append(out, "  "+faint.Render(hint)), "\n")
}
