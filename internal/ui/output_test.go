package ui

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mac-cleanup/internal/action"
	"mac-cleanup/internal/protocol"
	"mac-cleanup/internal/scan"
)

// memLog is an in-memory run log.
type memLog struct {
	mu     sync.Mutex
	b      strings.Builder
	closed bool
}

func (l *memLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *memLog) Close() error { l.closed = true; return nil }

func (l *memLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func withLog(d *Deps) *memLog {
	l := &memLog{}
	d.OpenLog = func() (io.WriteCloser, string, error) { return l, "/logs/run.log", nil }
	return l
}

func TestRunShowsCommandAndLiveOutput(t *testing.T) {
	d, _ := fakeDeps()
	release := make(chan struct{})
	d.Run = func(_ context.Context, it protocol.Item, a protocol.Action, out io.Writer) action.Outcome {
		io.WriteString(out, "hello from the command\n")
		<-release
		return action.Outcome{Key: it.Key(), Action: a, Command: it.Commands[a]}
	}
	m, cmd := confirm(t, *d, scan.Result{Script: "s", Items: []protocol.Item{item("s", "big", 9_000_000)}})
	m, cmd = send(m, cmd())
	msg := cmd()
	if _, ok := msg.(outputMsg); !ok {
		t.Fatalf("first message %T, want output", msg)
	}
	m, cmd = send(m, msg)
	v := m.View()
	for _, want := range []string{"big", "$ rm -rf x", "hello from the command", "running 1/1: big"} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q while running:\n%s", want, v)
		}
	}
	close(release)
	m, _ = pump(t, m, cmd)
	if m.screen != screenResult || !strings.Contains(m.View(), "hello from the command") {
		t.Fatalf("output gone from the result:\n%s", m.View())
	}
}

func TestRunLogWritten(t *testing.T) {
	d, _ := fakeDeps()
	l := withLog(d)
	d.Run = func(_ context.Context, it protocol.Item, a protocol.Action, out io.Writer) action.Outcome {
		if it.ID == "big" {
			io.WriteString(out, "rm: permission denied\n")
			return action.Outcome{Key: it.Key(), Action: a, Command: it.Commands[a], Err: errors.New("exit status 1")}
		}
		return action.Outcome{Key: it.Key(), Action: a, Command: it.Commands[a]}
	}
	m := finish(t, *d, twoItems())
	log := l.String()
	for _, want := range []string{"mac-cleanup run", "== big (delete)", "$ rm -rf x", "rm: permission denied",
		"-- failed: exit status 1", "== small (delete)", "-- ok", "Free before:"} {
		if !strings.Contains(log, want) {
			t.Fatalf("log misses %q:\n%s", want, log)
		}
	}
	if !l.closed {
		t.Fatal("log not closed")
	}
	if !strings.Contains(m.View(), "Log: /logs/run.log") {
		t.Fatalf("log path not shown:\n%s", m.View())
	}
}

func TestRunWithoutLogStillRuns(t *testing.T) {
	d, _ := fakeDeps()
	d.OpenLog = func() (io.WriteCloser, string, error) { return nil, "", errors.New("disk full") }
	ran := counting(d)
	m := finish(t, *d, twoItems())
	if len(*ran) != 2 || !strings.Contains(m.View(), "log could not be written: disk full") {
		t.Fatalf("ran=%v view:\n%s", *ran, m.View())
	}
}

func TestResultRescan(t *testing.T) {
	d, _ := fakeDeps()
	counting(d)
	m := finish(t, *d, twoItems())
	m, cmd := send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd == nil || m.screen != screenList || len(m.results) != 0 || len(m.selected) != 0 || m.running {
		t.Fatalf("not rescanning: screen=%v results=%d selected=%d", m.screen, len(m.results), len(m.selected))
	}
	if !strings.Contains(m.View(), "scanning 0/1") {
		t.Fatalf("no scan progress:\n%s", m.View())
	}
	m, _ = send(m, genScanMsg{gen: m.gen - 1, res: twoItems()})
	if len(m.results) != 0 {
		t.Fatal("result of the earlier scan round accepted")
	}
	m, _ = send(m, genScanMsg{gen: m.gen, res: twoItems()})
	if len(m.results) != 1 || len(m.selected) != 0 {
		t.Fatalf("results=%d selected=%d", len(m.results), len(m.selected))
	}
}

func TestResultHint(t *testing.T) {
	d, _ := fakeDeps()
	counting(d)
	m := finish(t, *d, twoItems())
	if !strings.Contains(m.View(), "r rescan  q quit") {
		t.Fatalf("view:\n%s", m.View())
	}
}

// A screen that falls behind must not turn a finished command into a
// failure: action.Run gives up on output still being copied a second after
// the command ends.
func TestRunSlowScreenKeepsSuccess(t *testing.T) {
	d, _ := fakeDeps()
	d.Run = action.Run
	it := item("s", "big", 9_000_000)
	it.Commands = map[protocol.Action]string{protocol.Delete: "echo first; sleep 0.2; echo second"}
	m, cmd := confirm(t, *d, scan.Result{Script: "s", Items: []protocol.Item{it}})
	m, cmd = send(m, cmd())
	msg := cmd()
	if _, ok := msg.(outputMsg); !ok {
		t.Fatalf("first message %T, want output", msg)
	}
	time.Sleep(1500 * time.Millisecond)
	m, _ = pump(t, m, func() tea.Msg { return msg })
	if m.screen != screenResult {
		t.Fatal("no result screen")
	}
	if o := m.run.outs[0]; o == nil || o.Err != nil {
		t.Fatalf("outcome %+v, want success", o)
	}
	if v := m.View(); !strings.Contains(v, "first") || !strings.Contains(v, "second") {
		t.Fatalf("output lost:\n%s", v)
	}
}

// Control characters from names, commands and output reach the log escaped,
// so viewing it in a terminal neither runs escape sequences nor shows a
// forged status line.
func TestRunLogEscapesControls(t *testing.T) {
	d, _ := fakeDeps()
	l := withLog(d)
	d.Run = func(_ context.Context, it protocol.Item, a protocol.Action, out io.Writer) action.Outcome {
		io.WriteString(out, "rm: x\x1b[2Jy: denied\n")
		return action.Outcome{Key: it.Key(), Action: a, Command: it.Commands[a], Err: errors.New("exit status 1")}
	}
	it := item("s", "big", 9_000_000)
	it.Name = "big\n-- ok (0s)\x1b]0;t\x07"
	it.Commands = map[protocol.Action]string{protocol.Delete: "rm -rf 'x\x1b[2Jy'"}
	finish(t, *d, scan.Result{Script: "s", Items: []protocol.Item{it}})
	log := l.String()
	if strings.ContainsAny(log, "\x1b\x07") || strings.Contains(log, "\n-- ok") {
		t.Fatalf("raw controls in the log:\n%q", log)
	}
	for _, want := range []string{`big\n-- ok (0s)\x1b]0;t\x07 (delete)`, `$ rm -rf 'x\x1b[2Jy'`, `rm: x\x1b[2Jy: denied`} {
		if !strings.Contains(log, want) {
			t.Fatalf("log misses %q:\n%q", want, log)
		}
	}
}

// A re-check started before the run that ends after a rescan belongs to the
// earlier scan round and is dropped.
func TestRescanDropsEarlierRecheck(t *testing.T) {
	d, _ := fakeDeps()
	counting(d)
	m := finish(t, *d, twoItems())
	stale := recheckMsg{gen: m.gen, res: twoItems()}
	m, _ = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m, _ = send(m, stale)
	m, _ = send(m, genScanMsg{gen: m.gen, res: twoItems()})
	if len(m.results) != 1 {
		t.Fatalf("got %d results, want 1", len(m.results))
	}
}

// Every output line in the log starts with "| ", even when a line arrives in
// pieces, so output cannot pass for the log's own lines.
func TestRunLogMarksOutputLines(t *testing.T) {
	d, _ := fakeDeps()
	l := withLog(d)
	d.Run = func(_ context.Context, it protocol.Item, a protocol.Action, out io.Writer) action.Outcome {
		io.WriteString(out, "rm: f\n-- ok (0.1s)\n\n== other (trash)\n$ trash x")
		io.WriteString(out, " still the same line\nlast")
		return action.Outcome{Key: it.Key(), Action: a, Command: it.Commands[a], Err: errors.New("exit status 1")}
	}
	finish(t, *d, scan.Result{Script: "s", Items: []protocol.Item{item("s", "big", 9_000_000)}})
	log := l.String()
	want := "$ rm -rf x\n| rm: f\n| -- ok (0.1s)\n| \n| == other (trash)\n| $ trash x still the same line\n| last\n-- failed: exit status 1"
	if !strings.Contains(log, want) {
		t.Fatalf("log misses %q:\n%q", want, log)
	}
}
