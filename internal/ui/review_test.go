package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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
	if !strings.Contains(v, cmd+"     (admin password)") {
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
	if _, cmd := send(m, tea.KeyMsg{Type: tea.KeyEnter}); cmd == nil {
		t.Fatal("enter did nothing when unblocked")
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
