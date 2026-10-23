package ui

import (
	"context"
	"errors"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mac-cleanup/internal/action"
	"mac-cleanup/internal/protocol"
	"mac-cleanup/internal/scan"
)

var ctrlC = tea.KeyMsg{Type: tea.KeyCtrlC}

func counting(d *Deps) *[]string {
	var ran []string
	d.Run = func(_ context.Context, it protocol.Item, a protocol.Action) action.Outcome {
		ran = append(ran, it.Key())
		return action.Outcome{Key: it.Key(), Action: a, Command: it.Commands[a]}
	}
	return &ran
}

func TestCtrlCStopsAfterCurrentItem(t *testing.T) {
	d, _ := fakeDeps()
	ran := counting(d)
	m, cmd := confirm(t, *d, twoItems())
	m, cmd = send(m, cmd()) // startRunMsg: first Run is in flight
	m, qcmd := send(m, ctrlC)
	if qcmd != nil {
		if _, quit := qcmd().(tea.QuitMsg); quit {
			t.Fatal("must not quit while a command is in flight")
		}
	}
	if !strings.Contains(m.View(), "stopping after big") {
		t.Fatalf("view:\n%s", m.View())
	}
	m, cmd = send(m, cmd()) // the in-flight outcome arrives
	if cmd != nil {
		t.Fatalf("nothing may start after stop, got a cmd")
	}
	if len(*ran) != 1 || m.screen != screenResult {
		t.Fatalf("ran=%v screen=%v", *ran, m.screen)
	}
	v := m.View()
	if !strings.Contains(v, "Not run:") || !strings.Contains(v, "small") {
		t.Fatalf("view:\n%s", v)
	}
	_, qcmd = send(m, ctrlC)
	if qcmd == nil {
		t.Fatal("ctrl+c on the result screen must quit")
	}
	if _, ok := qcmd().(tea.QuitMsg); !ok {
		t.Fatal("ctrl+c on the result screen must quit")
	}
}

func TestQAndEscAlsoStopAfterCurrent(t *testing.T) {
	for _, k := range []string{"q", "esc"} {
		d, _ := fakeDeps()
		ran := counting(d)
		m, cmd := confirm(t, *d, twoItems())
		m, cmd = send(m, cmd())
		m = press(m, k)
		m, cmd = send(m, cmd())
		if cmd != nil || len(*ran) != 1 || m.screen != screenResult {
			t.Fatalf("%s: ran=%v screen=%v", k, *ran, m.screen)
		}
	}
}

func TestSecondCtrlCInterruptsCurrentItem(t *testing.T) {
	d, _ := fakeDeps()
	d.Run = func(ctx context.Context, it protocol.Item, a protocol.Action) action.Outcome {
		<-ctx.Done()
		return action.Outcome{Key: it.Key(), Action: a, Command: it.Commands[a], Err: errors.New("interrupted")}
	}
	m, cmd := confirm(t, *d, twoItems())
	m, cmd = send(m, cmd())
	got := make(chan tea.Msg, 1)
	go func() { got <- cmd() }()
	m = press(m, "x")
	m, _ = send(m, ctrlC)
	select {
	case <-got:
		t.Fatal("first ctrl+c must not cancel the item")
	case <-time.After(100 * time.Millisecond):
	}
	m, _ = send(m, ctrlC)
	var msg tea.Msg
	select {
	case msg = <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("second ctrl+c did not cancel the context")
	}
	m, cmd = send(m, msg)
	if cmd != nil || m.screen != screenResult {
		t.Fatalf("screen=%v", m.screen)
	}
	v := m.View()
	for _, want := range []string{"1 item failed", "big", "interrupted", "Not run:", "small"} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q in:\n%s", want, v)
		}
	}
}

func TestStaleMessagesOnResultStartNothing(t *testing.T) {
	d, _ := fakeDeps()
	ran := counting(d)
	m := finish(t, *d, twoItems())
	n := len(*ran)
	for _, msg := range []tea.Msg{outcomeMsg{Key: "s/big"}, sudoMsg{}, refreshMsg{}} {
		var cmd tea.Cmd
		m, cmd = send(m, msg)
		if cmd != nil || len(*ran) != n || m.screen != screenResult {
			t.Fatalf("%T started something", msg)
		}
	}
}

func mixed() scan.Result {
	return scan.Result{Script: "s", Items: []protocol.Item{adminItem("adm"), item("s", "plain", 3_000_000)}}
}

func TestSudoRefusedAsksBeforeRunningAnything(t *testing.T) {
	d, _ := fakeDeps()
	ran := counting(d)
	m, cmd := confirm(t, *d, mixed())
	m, _ = send(m, cmd())
	m, cmd = send(m, sudoMsg{err: errors.New("exit 1")})
	if cmd != nil || len(*ran) != 0 {
		t.Fatal("a refused password must not start anything")
	}
	v := m.View()
	for _, want := range []string{"Admin password not given: 1 admin item will be skipped.", "enter runs the other 1, q stops"} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q in:\n%s", want, v)
		}
	}
	m = press(m, "x")
	if len(*ran) != 0 {
		t.Fatal("other keys do nothing")
	}
	m, cmd = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = pump(t, m, cmd)
	if strings.Join(*ran, ",") != "s/plain" {
		t.Fatalf("ran %v", *ran)
	}
	v = m.View()
	for _, want := range []string{"1 item failed", "adm", "rm -rf x", "admin password not given"} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q in:\n%s", want, v)
		}
	}
}

func TestSudoRefusedThenStop(t *testing.T) {
	for _, k := range []string{"q", "esc", "ctrl+c"} {
		d, _ := fakeDeps()
		ran := counting(d)
		m, cmd := confirm(t, *d, mixed())
		m, _ = send(m, cmd())
		m, _ = send(m, sudoMsg{err: errors.New("exit 1")})
		var qcmd tea.Cmd
		m, qcmd = send(m, map[string]tea.Msg{"q": tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")}, "esc": tea.KeyMsg{Type: tea.KeyEsc}, "ctrl+c": ctrlC}[k])
		if qcmd != nil {
			if _, quit := qcmd().(tea.QuitMsg); quit {
				t.Fatalf("%s must show the result, not quit", k)
			}
		}
		if len(*ran) != 0 || m.screen != screenResult {
			t.Fatalf("%s: ran=%v screen=%v", k, *ran, m.screen)
		}
		v := m.View()
		if !strings.Contains(v, "Not run:") || !strings.Contains(v, "adm") || !strings.Contains(v, "plain") {
			t.Fatalf("%s: view:\n%s", k, v)
		}
	}
}

func TestSudoRefusedWithNothingElseGoesToResult(t *testing.T) {
	d, _ := fakeDeps()
	ran := counting(d)
	m, cmd := confirm(t, *d, scan.Result{Script: "s", Items: []protocol.Item{adminItem("adm")}})
	m, _ = send(m, cmd())
	m, _ = send(m, sudoMsg{err: errors.New("exit 1")})
	if len(*ran) != 0 || m.screen != screenResult || !strings.Contains(m.View(), "admin password not given") {
		t.Fatalf("view:\n%s", m.View())
	}
}

func TestSudoAcceptedRunsMixInReviewOrder(t *testing.T) {
	d, _ := fakeDeps()
	ran := counting(d)
	res := scan.Result{Script: "s", Items: []protocol.Item{adminItem("a"), item("s", "b", 4_000_000), adminItem("c")}}
	// sizes: a 5 MB, c 5 MB, b 4 MB -> review order a, c, b (size ties by key)
	m, cmd := confirm(t, *d, res)
	m, _ = send(m, cmd())
	m, cmd = send(m, sudoMsg{})
	m, _ = pump(t, m, cmd)
	if strings.Join(*ran, ",") != "s/a,s/c,s/b" || m.screen != screenResult {
		t.Fatalf("ran %v", *ran)
	}
}

func TestSudoRefreshedBeforeEachAdminItem(t *testing.T) {
	d, _ := fakeDeps()
	ran := counting(d)
	refreshes, expired := 0, false
	d.SudoRefresh = func() error {
		refreshes++
		if expired {
			return errors.New("sudo: a password is required")
		}
		return nil
	}
	res := scan.Result{Script: "s", Items: []protocol.Item{
		adminItem("a"), item("s", "b", 4_900_000), adminItem("c"),
	}}
	res.Items[2].Size = 4_000_000
	m, cmd := confirm(t, *d, res) // order a, b, c
	m, _ = send(m, cmd())
	m, cmd = send(m, sudoMsg{}) // fresh ticket: a runs without a refresh
	if refreshes != 0 {
		t.Fatalf("refreshes=%d before first item", refreshes)
	}
	m, cmd = send(m, cmd()) // a done -> b runs (not admin)
	m, cmd = send(m, cmd()) // b done -> refresh before c
	if len(*ran) != 2 || cmd == nil {
		t.Fatalf("ran=%v", *ran)
	}
	expired = true
	m, cmd = send(m, cmd()) // refreshMsg with error -> prompt again
	if refreshes != 1 || len(*ran) != 2 || !strings.Contains(m.View(), "admin password") {
		t.Fatalf("refreshes=%d ran=%v view:\n%s", refreshes, *ran, m.View())
	}
	if cmd == nil {
		t.Fatal("want the sudo prompt cmd")
	}
	m, cmd = send(m, sudoMsg{}) // accepted: c runs without another refresh
	m, _ = pump(t, m, cmd)
	if refreshes != 1 || strings.Join(*ran, ",") != "s/a,s/b,s/c" {
		t.Fatalf("refreshes=%d ran=%v", refreshes, *ran)
	}
}

func TestMidRunRefusalCountsRemaining(t *testing.T) {
	d, _ := fakeDeps()
	ran := counting(d)
	d.SudoRefresh = func() error { return errors.New("expired") }
	res := scan.Result{Script: "s", Items: []protocol.Item{adminItem("a"), item("s", "b", 4_900_000), adminItem("c"), item("s", "d", 3_000_000)}}
	res.Items[2].Size = 4_000_000
	m, cmd := confirm(t, *d, res) // a b c d
	m, _ = send(m, cmd())
	m, cmd = send(m, sudoMsg{})
	m, cmd = send(m, cmd()) // a done, b runs
	m, cmd = send(m, cmd()) // b done, refresh before c
	m, _ = send(m, cmd())   // refresh fails -> prompt
	m, _ = send(m, sudoMsg{err: errors.New("exit 1")})
	if !strings.Contains(m.View(), "Admin password not given: 1 admin item will be skipped.") ||
		!strings.Contains(m.View(), "enter runs the other 1, q stops") {
		t.Fatalf("view:\n%s", m.View())
	}
	m, cmd = send(m, tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = pump(t, m, cmd)
	if strings.Join(*ran, ",") != "s/a,s/b,s/d" {
		t.Fatalf("ran %v", *ran)
	}
	if !strings.Contains(m.View(), "admin password not given") {
		t.Fatalf("view:\n%s", m.View())
	}
}

func TestSudoDoneDistinguishesRestoreErrors(t *testing.T) {
	d, _ := fakeDeps()
	m, cmd := confirm(t, *d, mixed())
	m, _ = send(m, cmd())
	m, _ = send(m, sudoDone(errors.New("tty gone")))
	if !strings.Contains(m.View(), "sudo could not be started or the terminal could not be restored: tty gone") ||
		!strings.Contains(m.View(), "enter runs the other 1, q stops") {
		t.Fatalf("view:\n%s", m.View())
	}

	m, cmd = confirm(t, *d, mixed())
	m, _ = send(m, cmd())
	m, _ = send(m, sudoDone(&exec.ExitError{}))
	if strings.Contains(m.View(), "could not restore") || !strings.Contains(m.View(), "Admin password not given") {
		t.Fatalf("view:\n%s", m.View())
	}
	if msg := sudoDone(nil); msg.(sudoMsg).err != nil {
		t.Fatal("nil error must stay nil")
	}
}

func TestOutputSanitised(t *testing.T) {
	d, _ := fakeDeps()
	d.Run = func(_ context.Context, it protocol.Item, a protocol.Action) action.Outcome {
		return action.Outcome{Key: it.Key(), Action: a, Command: it.Commands[a],
			Output: "\x1b[31mred\x1b[0m\rprogress 50%\r\nline2\x07\x00\ttab\n", Err: errors.New("exit status 1")}
	}
	m := finish(t, *d, scan.Result{Script: "s", Items: []protocol.Item{item("s", "big", 9_000_000)}})
	for _, bad := range []string{"\x1b[31m", "\r", "\x07", "\x00"} {
		if strings.Contains(m.resultText(), bad) {
			t.Fatalf("contains %q", bad)
		}
	}
	for _, want := range []string{"red", "progress 50%", "line2", "\ttab"} {
		if !strings.Contains(m.resultText(), want) {
			t.Fatalf("missing %q in %q", want, m.resultText())
		}
	}
}

func TestTrashSumUsesSnapshotAction(t *testing.T) {
	d, _ := fakeDeps()
	d.Run = func(_ context.Context, it protocol.Item, a protocol.Action) action.Outcome {
		return action.Outcome{Key: it.Key(), Action: protocol.Delete} // misreports its action
	}
	it := both("rm -rf d", "mv t", func(it *protocol.Item) { it.Default = protocol.Trash })
	m := finish(t, *d, scan.Result{Script: "s", Items: []protocol.Item{it}})
	if !strings.Contains(m.View(), "Moved to Trash: 5 MB") {
		t.Fatalf("view:\n%s", m.View())
	}
}

func TestAdminInFlightCannotBeInterrupted(t *testing.T) {
	d, _ := fakeDeps()
	ctxCh := make(chan context.Context, 1)
	release := make(chan struct{})
	d.Run = func(ctx context.Context, it protocol.Item, a protocol.Action) action.Outcome {
		ctxCh <- ctx
		<-release
		return action.Outcome{Key: it.Key(), Action: a, Command: it.Commands[a]}
	}
	m, cmd := confirm(t, *d, scan.Result{Script: "s", Items: []protocol.Item{adminItem("adm"), item("s", "plain", 3_000_000)}})
	m, _ = send(m, cmd())
	m, cmd = send(m, sudoMsg{})
	got := make(chan tea.Msg, 1)
	go func() { got <- cmd() }()
	runCtx := <-ctxCh
	m, _ = send(m, ctrlC)
	m, _ = send(m, ctrlC)
	if runCtx.Err() != nil {
		t.Fatal("an admin command must not be cancelled")
	}
	m.width = 200
	v := m.View()
	if !strings.Contains(v, "stopping after adm… (admin command, cannot be interrupted, waiting for it to finish)") {
		t.Fatalf("view:\n%s", v)
	}
	close(release)
	m, cmd = send(m, <-got)
	if cmd != nil || m.screen != screenResult || !strings.Contains(m.View(), "Not run:") {
		t.Fatalf("screen=%v", m.screen)
	}
}

func TestRefusalReasonVisibleWhenNothingElseRuns(t *testing.T) {
	d, _ := fakeDeps()
	m, cmd := confirm(t, *d, scan.Result{Script: "s", Items: []protocol.Item{adminItem("adm")}})
	m, _ = send(m, cmd())
	m.width = 200
	m, _ = send(m, sudoDone(errors.New("tty gone")))
	want := "admin password not given (sudo could not be started or the terminal could not be restored: tty gone)"
	if m.screen != screenResult || !strings.Contains(m.resultText(), want) {
		t.Fatalf("view:\n%s", m.View())
	}
}

func TestCtrlCOnReviewAfterEnterDoesNotQuit(t *testing.T) {
	d, _ := fakeDeps()
	ran := counting(d)
	m := loaded(t, *d, twoItems())
	for _, it := range m.visibleItems() {
		m.selected[it.Key()] = true
	}
	m = press(m, "enter", "enter")
	m, start := send(m, tea.KeyMsg{Type: tea.KeyEnter})
	m, cmd := send(m, ctrlC)
	if cmd != nil {
		t.Fatal("ctrl+c between enter and the run start must be dropped")
	}
	m, cmd = send(m, start())
	m, _ = pump(t, m, cmd)
	if len(*ran) != 2 || m.screen != screenResult {
		t.Fatalf("ran=%v screen=%v", *ran, m.screen)
	}
}

func TestRunScreenWrapKeepsEscapesWhole(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI)
	defer lipgloss.SetColorProfile(prev)
	d, _ := fakeDeps()
	counting(d)
	m, cmd := confirm(t, *d, twoItems())
	m, _ = send(m, cmd())
	stopped, _ := send(m, ctrlC)
	sgr := regexp.MustCompile(`^\x1b\[[0-9;]*m`)
	for _, v := range []Model{m, stopped} {
		for w := 28; w <= 35; w++ {
			v.width = w
			out := v.View()
			if !strings.Contains(out, "\x1b[") {
				t.Fatal("test needs styled output")
			}
			for _, line := range strings.Split(out, "\n") {
				for i := 0; i < len(line); i++ {
					if line[i] == 0x1b && !sgr.MatchString(line[i:]) {
						t.Fatalf("width %d: partial escape in %q", w, line)
					}
				}
				if lipgloss.Width(line) > w {
					t.Fatalf("width %d: line too wide: %q", w, line)
				}
			}
		}
	}
}
