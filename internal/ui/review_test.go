package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"mac-cleanup/internal/protocol"
	"mac-cleanup/internal/scan"
)

func both(del, trash string, mods ...func(*protocol.Item)) protocol.Item {
	return item("s", "both", 5_000_000, append([]func(*protocol.Item){func(it *protocol.Item) {
		it.Commands = map[protocol.Action]string{protocol.Delete: del, protocol.Trash: trash}
	}}, mods...)...)
}

func warned(id string) protocol.Item {
	return item("w", id, 9_000_000, func(it *protocol.Item) { it.Warnings = []string{"Gradle is running"} })
}

// reviewing selects every listed item and opens the review.
func reviewing(t *testing.T, results ...scan.Result) Model {
	t.Helper()
	d, _ := fakeDeps()
	m := loaded(t, *d, results...)
	for _, it := range m.visibleItems() {
		m.selected[it.Key()] = true
	}
	m = press(m, "enter")
	if m.screen != screenReview {
		t.Fatal("not on review screen")
	}
	return m
}

func TestReviewDefaults(t *testing.T) {
	it := both("rm -rf d", "mv t", func(it *protocol.Item) { it.Default = protocol.Trash })
	m := reviewing(t, scan.Result{Script: "s", Items: []protocol.Item{it}})
	v := m.View()
	for _, want := range []string{"Review: 1 item, 5 MB", "( ) delete  (•) trash", "mv t"} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q in:\n%s", want, v)
		}
	}
	if strings.Contains(v, "rm -rf d") {
		t.Fatalf("unchosen command shown:\n%s", v)
	}
	if !strings.Contains(v, "[ run ]") {
		t.Fatalf("want [ run ]:\n%s", v)
	}
}

func TestCommandShownExactly(t *testing.T) {
	cmd := `rm -rf '/a b'\''c'`
	it := item("s", "q", 5_000_000, func(it *protocol.Item) {
		it.Commands = map[protocol.Action]string{protocol.Delete: cmd}
		it.Admin = true
	})
	m := reviewing(t, scan.Result{Script: "s", Items: []protocol.Item{it}})
	v := m.View()
	if !strings.Contains(v, cmd+"\n   (admin password)") {
		t.Fatalf("command not verbatim with admin note:\n%s", v)
	}
}

func TestSwitchAction(t *testing.T) {
	m := reviewing(t, scan.Result{Script: "s", Items: []protocol.Item{both("cmd_delete", "cmd_trash", func(it *protocol.Item) { it.Default = protocol.Trash })}})
	if !strings.Contains(m.View(), "cmd_trash") {
		t.Fatal("want trash first")
	}
	m = press(m, "d")
	v := m.View()
	if !strings.Contains(v, "cmd_delete") || strings.Contains(v, "cmd_trash") {
		t.Fatalf("d did not switch:\n%s", v)
	}
	m = press(m, "t")
	if !strings.Contains(m.View(), "cmd_trash") {
		t.Fatal("t did not switch")
	}
	m, _ = send(m, tea.KeyMsg{Type: tea.KeyLeft})
	if !strings.Contains(m.View(), "cmd_delete") {
		t.Fatal("left did not switch")
	}
	m, _ = send(m, tea.KeyMsg{Type: tea.KeyRight})
	if !strings.Contains(m.View(), "cmd_trash") {
		t.Fatal("right did not switch")
	}
}

func TestSingleActionNoSwitch(t *testing.T) {
	m := reviewing(t, scan.Result{Script: "s", Items: []protocol.Item{item("s", "one", 5_000_000)}})
	v := m.View()
	if strings.Contains(v, "trash") || !strings.Contains(v, "(•) delete") {
		t.Fatalf("single action row wrong:\n%s", v)
	}
	m = press(m, "t")
	m, _ = send(m, tea.KeyMsg{Type: tea.KeyRight})
	if m.View() != v {
		t.Fatal("view changed")
	}
}

func TestBlockedByWarning(t *testing.T) {
	m := reviewing(t, scan.Result{Script: "w", Items: []protocol.Item{warned("g")}})
	v := m.View()
	if !strings.Contains(v, "( run: blocked )") || !strings.Contains(v, "⚠ Gradle is running: quit it and press r to re-check, or s to skip") {
		t.Fatalf("blocked view wrong:\n%s", v)
	}
	if _, cmd := send(m, tea.KeyMsg{Type: tea.KeyEnter}); cmd != nil {
		t.Fatal("enter ran while blocked")
	}
}

func TestSkipUnblocks(t *testing.T) {
	ok := item("s", "ok", 5_000_000)
	m := reviewing(t, scan.Result{Script: "s", Items: []protocol.Item{ok}}, scan.Result{Script: "w", Items: []protocol.Item{warned("g")}})
	// warned item is larger, so it is first in list order
	m = press(m, "s")
	if m.selected[warned("g").Key()] {
		t.Fatal("skipped item still selected")
	}
	if !strings.Contains(m.View(), "Review: 1 item") {
		t.Fatalf("title:\n%s", m.View())
	}
	_, cmd := send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter did nothing when unblocked")
	}
	if _, ok := cmd().(startRunMsg); !ok {
		t.Fatalf("enter emitted %T", cmd())
	}
}

func TestRecheckClears(t *testing.T) {
	m := reviewing(t, scan.Result{Script: "w", Items: []protocol.Item{warned("g")}})
	d, _ := fakeDeps()
	m.deps = *d
	m.deps.Scan = func(s string) scan.Result {
		return scan.Result{Script: s, Items: []protocol.Item{item("w", "g", 9_000_000)}}
	}
	m, cmd := send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd == nil {
		t.Fatal("r returned no cmd")
	}
	msg := cmd()
	rm, ok := msg.(recheckMsg)
	if !ok {
		t.Fatalf("got %T", msg)
	}
	m, _ = send(m, rm)
	if v := m.View(); !strings.Contains(v, "[ run ]") || strings.Contains(v, "⚠") {
		t.Fatalf("still blocked:\n%s", v)
	}
	if len(m.visibleItems()) != 1 || len(m.visibleItems()[0].Warnings) != 0 {
		t.Fatal("list data not refreshed")
	}
}

func TestRecheckItemGone(t *testing.T) {
	m := reviewing(t, scan.Result{Script: "w", Items: []protocol.Item{warned("g")}}, scan.Result{Script: "s", Items: []protocol.Item{item("s", "ok", 5_000_000)}})
	m, _ = send(m, recheckMsg(scan.Result{Script: "w"}))
	v := m.View()
	if !strings.Contains(v, "g no longer found") || !strings.Contains(v, "Review: 1 item") || !strings.Contains(v, "[ run ]") {
		t.Fatalf("view:\n%s", v)
	}
	if m.selected[warned("g").Key()] {
		t.Fatal("gone item still selected")
	}
}

func TestRecheckFailureKeepsBlock(t *testing.T) {
	m := reviewing(t, scan.Result{Script: "w", Items: []protocol.Item{warned("g")}})
	m, _ = send(m, recheckMsg(scan.Result{Script: "w", Err: errors.New("boom")}))
	v := m.View()
	if !strings.Contains(v, "( run: blocked )") || !strings.Contains(v, "boom") {
		t.Fatalf("view:\n%s", v)
	}
}

func TestEscKeepsSelection(t *testing.T) {
	m := reviewing(t, scan.Result{Script: "s", Items: []protocol.Item{item("s", "a", 5_000_000), item("s", "b", 6_000_000)}})
	m = press(m, "s", "esc")
	if m.screen != screenList {
		t.Fatal("not on list")
	}
	if n, _ := m.selectedTotals(); n != 1 {
		t.Fatalf("selected %d, want 1", n)
	}
}

func TestReviewCursorSurvivesSkip(t *testing.T) {
	m := reviewing(t, scan.Result{Script: "s", Items: []protocol.Item{
		item("s", "a", 9_000_000), item("s", "b", 8_000_000), item("s", "c", 7_000_000)}})
	m = press(m, "down", "down", "s")
	if m.reviewIndex(m.reviewItems()) != 1 {
		t.Fatalf("cursor at %d", m.reviewIndex(m.reviewItems()))
	}
}

func TestLongCommandWrapsLosslessly(t *testing.T) {
	cmd := "[ ! -e '/x/a  b   c' ] &&  sudo mv -- '/Users/me/a  b/it'\\''s/very/long/path/name' '/Users/me/.Trash/it'\\''s'\nsecond   line"
	it := item("s", "long", 5_000_000, func(it *protocol.Item) {
		it.Commands = map[protocol.Action]string{protocol.Delete: cmd}
	})
	m := reviewing(t, scan.Result{Script: "s", Items: []protocol.Item{it}})
	m, _ = send(m, tea.WindowSizeMsg{Width: 40, Height: 30})
	lines := strings.Split(m.View(), "\n")
	for _, l := range lines {
		if w := lipgloss.Width(l); w > 40 {
			t.Fatalf("line of %d cells: %q", w, l)
		}
	}
	var segs []string
	in := false
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "> "):
			in = true
		case strings.Contains(l, "[ run ]"):
			in = false
		case in && strings.HasPrefix(l, cmdCont):
			segs = append(segs, strings.TrimPrefix(l, cmdCont))
		case in && strings.HasPrefix(l, cmdFirst):
			segs = append(segs, strings.TrimPrefix(l, cmdFirst))
		}
	}
	got := strings.Join(segs, "")
	if want := strings.ReplaceAll(cmd, "\n", ""); got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestReviewScrolls(t *testing.T) {
	var items []protocol.Item
	for i := 0; i < 12; i++ {
		id := string(rune('a' + i))
		items = append(items, item("s", id, int64(20_000_000-i*1_000_000), func(it *protocol.Item) {
			it.Commands = map[protocol.Action]string{protocol.Delete: "rm -rf /cmd-" + id}
		}))
	}
	m := reviewing(t, scan.Result{Script: "s", Items: items})
	m, _ = send(m, tea.WindowSizeMsg{Width: 80, Height: 10})
	v := m.View()
	if n := len(strings.Split(v, "\n")); n > 10 {
		t.Fatalf("%d lines:\n%s", n, v)
	}
	if !strings.Contains(v, "↓ more") || strings.Contains(v, "↑ more") {
		t.Fatalf("markers:\n%s", v)
	}
	for i := 0; i < 11; i++ {
		m = press(m, "down")
	}
	v = m.View()
	if n := len(strings.Split(v, "\n")); n > 10 {
		t.Fatalf("%d lines:\n%s", n, v)
	}
	if !strings.Contains(v, "> l ") || !strings.Contains(v, "rm -rf /cmd-l") || !strings.Contains(v, "↑ more") || strings.Contains(v, "↓ more") {
		t.Fatalf("last item not fully visible:\n%s", v)
	}
}

func TestEnterSnapshotAndIgnoresLateRecheck(t *testing.T) {
	a := both("del-a", "trash-a", func(it *protocol.Item) { it.Default = protocol.Trash })
	b := item("s", "z", 4_000_000)
	m := reviewing(t, scan.Result{Script: "s", Items: []protocol.Item{a, b}})
	m, cmd := send(m, tea.KeyMsg{Type: tea.KeyEnter})
	msg, ok := cmd().(startRunMsg)
	if !ok || len(msg.entries) != 2 {
		t.Fatalf("got %#v", msg)
	}
	e := msg.entries[0]
	if e.Item.ID != "both" || e.Action != protocol.Trash || e.Command != "trash-a" || msg.entries[1].Command != "rm -rf x" {
		t.Fatalf("entries %#v", msg.entries)
	}
	changed := both("del-a", "CHANGED")
	m, _ = send(m, recheckMsg(scan.Result{Script: "s", Items: []protocol.Item{changed}}))
	if got := m.reviewItems(); len(got) != 2 || got[0].Commands[protocol.Trash] != "trash-a" {
		t.Fatal("late recheck applied after enter")
	}
}

func TestRecheckBelowMinSizeNote(t *testing.T) {
	m := reviewing(t, scan.Result{Script: "w", Items: []protocol.Item{warned("g")}})
	small := warned("g")
	small.Size = 10
	m, _ = send(m, recheckMsg(scan.Result{Script: "w", Items: []protocol.Item{small}}))
	v := m.View()
	if strings.Contains(v, "no longer found") || !strings.Contains(v, "g is now below 1 MB, removed") {
		t.Fatalf("view:\n%s", v)
	}
}

func recheckCmds(t *testing.T, m Model, scans *[]string, fresh map[string][]protocol.Item) Model {
	t.Helper()
	m.deps.Scan = func(s string) scan.Result {
		*scans = append(*scans, s)
		return scan.Result{Script: s, Items: fresh[s]}
	}
	m, cmd := send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd == nil {
		t.Fatal("no cmd")
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range batch {
			m, _ = send(m, c())
		}
		return m
	}
	t.Fatal("expected a batch")
	return m
}

func TestRecheckOnlyWarnedScripts(t *testing.T) {
	m := reviewing(t,
		scan.Result{Script: "s", Items: []protocol.Item{item("s", "ok", 5_000_000)}},
		scan.Result{Script: "w1", Items: []protocol.Item{warned1("w1", "a")}},
		scan.Result{Script: "w2", Items: []protocol.Item{warned1("w2", "b")}})
	var scans []string
	m = recheckCmds(t, m, &scans, map[string][]protocol.Item{
		"w1": {item("w1", "a", 9_000_000)},
		"w2": {warned1("w2", "b")},
	})
	if len(scans) != 2 || scans[0] == "s" || scans[1] == "s" {
		t.Fatalf("scanned %v", scans)
	}
	v := m.View()
	if !strings.Contains(v, "( run: blocked )") || strings.Count(v, "⚠") != 1 {
		t.Fatalf("want exactly w2 still blocking:\n%s", v)
	}
}

func warned1(script, id string) protocol.Item {
	return item(script, id, 9_000_000, func(it *protocol.Item) { it.Warnings = []string{"busy"} })
}
