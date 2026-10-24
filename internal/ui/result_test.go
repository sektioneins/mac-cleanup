package ui

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mac-cleanup/internal/action"
	"mac-cleanup/internal/protocol"
	"mac-cleanup/internal/scan"
)

// pump runs cmd; outcomeMsg, outputMsg and startRunMsg results are fed back in until no
// command is left. Other messages (sudo) are returned unhandled.
func pump(t *testing.T, m Model, cmd tea.Cmd) (Model, tea.Msg) {
	t.Helper()
	for i := 0; cmd != nil; i++ {
		if i > 100 {
			t.Fatal("runaway")
		}
		msg := cmd()
		switch msg.(type) {
		case startRunMsg, outcomeMsg, outputMsg, refreshMsg:
			m, cmd = send(m, msg)
		default:
			return m, msg
		}
	}
	return m, nil
}

// confirm selects all items of the results, opens the review and presses
// enter twice: once to reach the run button, once to press it.
func confirm(t *testing.T, d Deps, results ...scan.Result) (Model, tea.Cmd) {
	t.Helper()
	m := loaded(t, d, results...)
	for _, it := range m.visibleItems() {
		m.selected[it.Key()] = true
	}
	m = press(m, "enter", "enter")
	return send(m, tea.KeyMsg{Type: tea.KeyEnter})
}

func twoItems() scan.Result {
	return scan.Result{Script: "s", Items: []protocol.Item{
		item("s", "big", 9_000_000), item("s", "small", 2_000_000),
	}}
}

func TestRunsInOrder(t *testing.T) {
	d, _ := fakeDeps()
	var keys []string
	d.Run = func(_ context.Context, it protocol.Item, a protocol.Action, _ io.Writer) action.Outcome {
		keys = append(keys, it.Key())
		return action.Outcome{Key: it.Key(), Action: a, Command: it.Commands[a]}
	}
	m, cmd := confirm(t, *d, twoItems())
	m, _ = pump(t, m, cmd)
	if strings.Join(keys, ",") != "s/big,s/small" {
		t.Fatalf("order: %v", keys)
	}
	if m.screen != screenResult {
		t.Fatalf("screen %v", m.screen)
	}
}

func TestRunOneAtATimeAndProgress(t *testing.T) {
	d, _ := fakeDeps()
	n := 0
	d.Run = func(_ context.Context, it protocol.Item, a protocol.Action, _ io.Writer) action.Outcome {
		n++
		return action.Outcome{Key: it.Key()}
	}
	m, cmd := confirm(t, *d, twoItems())
	m, cmd = send(m, cmd()) // startRunMsg
	if n != 0 || !strings.Contains(m.View(), "running 1/2: big") {
		t.Fatalf("n=%d view:\n%s", n, m.View())
	}
	m, cmd = send(m, cmd())
	if n != 1 || !strings.Contains(m.View(), "running 2/2: small") {
		t.Fatalf("n=%d view:\n%s", n, m.View())
	}
	m = press(m, "enter", "x")
	if m.screen != screenRun {
		t.Fatal("keys must be ignored while running")
	}
}

func TestRunUsesChosenAction(t *testing.T) {
	d, _ := fakeDeps()
	var got []protocol.Action
	d.Run = func(_ context.Context, it protocol.Item, a protocol.Action, _ io.Writer) action.Outcome {
		got = append(got, a)
		return action.Outcome{Key: it.Key(), Action: a}
	}
	it := both("rm -rf d", "mv t")
	m := loaded(t, *d, scan.Result{Script: "s", Items: []protocol.Item{it}})
	m.selected[it.Key()] = true
	m = press(m, "enter", "t", "enter")
	m, cmd := send(m, tea.KeyMsg{Type: tea.KeyEnter})
	pump(t, m, cmd)
	if len(got) != 1 || got[0] != protocol.Trash {
		t.Fatalf("got %v", got)
	}
}

func TestRunIgnoresLateRecheck(t *testing.T) {
	d, _ := fakeDeps()
	var cmds []string
	d.Run = func(_ context.Context, it protocol.Item, a protocol.Action, _ io.Writer) action.Outcome {
		cmds = append(cmds, it.Commands[a])
		return action.Outcome{Key: it.Key(), Action: a}
	}
	m, cmd := confirm(t, *d, twoItems())
	m, cmd = send(m, cmd())
	changed := item("s", "big", 9_000_000, func(it *protocol.Item) {
		it.Commands = map[protocol.Action]string{protocol.Delete: "rm -rf evil"}
	})
	m, _ = send(m, recheckMsg{res: scan.Result{Script: "s", Items: []protocol.Item{changed}}})
	pump(t, m, cmd)
	if len(cmds) != 2 || cmds[0] != "rm -rf x" || cmds[1] != "rm -rf x" {
		t.Fatalf("ran %v", cmds)
	}
}

func adminItem(id string) protocol.Item {
	return item("s", id, 5_000_000, func(it *protocol.Item) { it.Admin = true })
}

func TestSudoRequestedForAdmin(t *testing.T) {
	d, _ := fakeDeps()
	ran := 0
	d.Run = func(_ context.Context, it protocol.Item, a protocol.Action, _ io.Writer) action.Outcome {
		ran++
		return action.Outcome{Key: it.Key()}
	}
	m, cmd := confirm(t, *d, scan.Result{Script: "s", Items: []protocol.Item{adminItem("a")}})
	m, cmd = send(m, cmd())
	if cmd == nil || ran != 0 {
		t.Fatalf("want a sudo cmd and nothing run yet (ran=%d)", ran)
	}
	if !strings.Contains(m.View(), "admin password") {
		t.Fatalf("view:\n%s", m.View())
	}
	m, cmd = send(m, sudoMsg{})
	pump(t, m, cmd)
	if ran != 1 {
		t.Fatalf("ran=%d", ran)
	}

	// Non-admin review needs no sudo.
	sudo := 0
	d.SudoCmd = func() *exec.Cmd { sudo++; return exec.Command("true") }
	m, cmd = confirm(t, *d, twoItems())
	m, cmd = send(m, cmd())
	if sudo != 0 || cmd == nil {
		t.Fatalf("sudo=%d", sudo)
	}
	if msg := cmd(); func() bool { _, ok := msg.(outcomeMsg); return !ok }() {
		t.Fatalf("want first run, got %T", msg)
	}
}

func finish(t *testing.T, d Deps, res scan.Result) Model {
	t.Helper()
	m, cmd := confirm(t, d, res)
	m, _ = pump(t, m, cmd)
	if m.screen != screenResult {
		t.Fatal("no result screen")
	}
	return m
}

func TestResultFreeSpace(t *testing.T) {
	d, _ := fakeDeps()
	calls := 0
	d.Free = func() (uint64, uint64, error) {
		calls++
		if calls == 1 {
			return 40_000_000_000, 994_000_000_000, nil
		}
		return 52_500_000_000, 994_000_000_000, nil
	}
	m := finish(t, *d, twoItems())
	if !strings.Contains(m.View(), "Free before: 40 GB   after: 52.5 GB   gained: 12.5 GB") {
		t.Fatalf("view:\n%s", m.View())
	}

	// Less free space afterwards: gained is 0 B.
	calls = 0
	d.Free = func() (uint64, uint64, error) {
		calls++
		return uint64(50_000_000_000 - calls*1_000_000_000), 1, nil
	}
	m = finish(t, *d, twoItems())
	if !strings.Contains(m.View(), "gained: 0 B") {
		t.Fatalf("view:\n%s", m.View())
	}

	// Free fails: unknown, no gained.
	d.Free = func() (uint64, uint64, error) { return 0, 0, errors.New("x") }
	m = finish(t, *d, twoItems())
	v := m.View()
	if !strings.Contains(v, "Free before: unknown   after: unknown") || strings.Contains(v, "gained") {
		t.Fatalf("view:\n%s", v)
	}
}

func TestFreeBeforeMeasuredAtStart(t *testing.T) {
	d, _ := fakeDeps()
	cur := uint64(10_000_000_000)
	d.Free = func() (uint64, uint64, error) { return cur, 1, nil }
	d.Run = func(_ context.Context, it protocol.Item, a protocol.Action, _ io.Writer) action.Outcome {
		cur = 30_000_000_000
		return action.Outcome{Key: it.Key()}
	}
	m := finish(t, *d, twoItems())
	if !strings.Contains(m.View(), "Free before: 10 GB   after: 30 GB   gained: 20 GB") {
		t.Fatalf("view:\n%s", m.View())
	}
}

func TestResultTrashNote(t *testing.T) {
	d, _ := fakeDeps()
	trashOnly := func(id string, size int64) protocol.Item {
		return item("s", id, size, func(it *protocol.Item) {
			it.Default = protocol.Trash
			it.Commands = map[protocol.Action]string{protocol.Trash: "mv x"}
		})
	}
	d.Run = func(_ context.Context, it protocol.Item, a protocol.Action, _ io.Writer) action.Outcome {
		o := action.Outcome{Key: it.Key(), Action: a, Command: it.Commands[a]}
		if it.ID == "bad" {
			o.Err = errors.New("exit status 1")
		}
		return o
	}
	res := scan.Result{Script: "s", Items: []protocol.Item{trashOnly("a", 2_000_000_000), trashOnly("bad", 5_000_000_000), item("s", "del", 7_000_000_000)}}
	v := finish(t, *d, res).View()
	if !strings.Contains(v, "Moved to Trash: 2 GB, freed once you empty the Trash") {
		t.Fatalf("view:\n%s", v)
	}

	v = finish(t, *d, twoItems()).View()
	if strings.Contains(v, "Moved to Trash") {
		t.Fatalf("unexpected note:\n%s", v)
	}
}

func TestResultFailureListed(t *testing.T) {
	d, _ := fakeDeps()
	d.Run = func(_ context.Context, it protocol.Item, a protocol.Action, _ io.Writer) action.Outcome {
		return action.Outcome{Key: it.Key(), Action: a, Command: it.Commands[a], Output: "rm: permission denied\n", Err: errors.New("exit status 1")}
	}
	m := finish(t, *d, scan.Result{Script: "s", Items: []protocol.Item{item("s", "big", 9_000_000)}})
	v := m.View()
	for _, want := range []string{"1 item failed", "big", "$ rm -rf x", "exit status 1", "rm: permission denied"} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q in:\n%s", want, v)
		}
	}
	_, cmd := send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("q must quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("q must quit")
	}
}

func TestResultNoFreeLineForAutostartOnly(t *testing.T) {
	d, _ := fakeDeps()
	d.Free = func() (uint64, uint64, error) { return 40_000_000_000, 994_000_000_000, nil }
	au := auto("au", 7_000_000, "running", autoActions)
	finishAll := func(items ...protocol.Item) Model {
		t.Helper()
		m := loaded(t, *d, scan.Result{Script: "s", Items: items})
		for _, it := range m.allItems() {
			m.selected[it.Key()] = true
		}
		m = press(m, "enter", "enter")
		m, cmd := send(m, tea.KeyMsg{Type: tea.KeyEnter})
		m, _ = pump(t, m, cmd)
		if m.screen != screenResult {
			t.Fatal("no result screen")
		}
		return m
	}

	v := finishAll(au).View()
	if strings.Contains(v, "Free before") || !strings.Contains(v, "au (disable)") {
		t.Fatalf("autostart-only result:\n%s", v)
	}
	if v = finishAll(au, item("s", "sp", 5_000_000)).View(); !strings.Contains(v, "Free before: 40 GB") {
		t.Fatalf("mixed result:\n%s", v)
	}
}
