package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mac-cleanup/internal/action"
	"mac-cleanup/internal/protocol"
	"mac-cleanup/internal/scan"
)

func TestVisible(t *testing.T) {
	for in, want := range map[string]string{
		"plain":        "plain",
		"a\tb\nc":      "a\tb\nc",
		"é 漢 ⚠":        "é 漢 ⚠",
		"a\rb":         `a\rb`,
		"\x1b[K":       `\x1b[K`,
		"\x00\x07\x1f": `\x00\x07\x1f`,
		"\x7f":         `\x7f`,
		"a\u0085b":     `a\u0085b`,
		"\u009b2J":     `\u009b2J`,
		"bad\xffbyte":  `bad\xffbyte`,
	} {
		if got := Visible(in); got != want {
			t.Errorf("Visible(%q) = %q, want %q", in, got, want)
		}
	}
	if hasControl("a\tb\nc é") || !hasControl("a\rb") || !hasControl("\u0085") || !hasControl("\x7f") {
		t.Error("hasControl wrong")
	}
}

// noRaw fails if v contains a raw ESC or CR (tests render without styles).
func noRaw(t *testing.T, v string) {
	t.Helper()
	if strings.ContainsAny(v, "\x1b\r") {
		t.Fatalf("raw control character in view:\n%q", v)
	}
}

const ctrlCmd = "rm -rf -- '/x/Icon\r' && printf '\x1b[K'"

func ctrlItem(mods ...func(*protocol.Item)) protocol.Item {
	return item("s", "c", 5_000_000, append([]func(*protocol.Item){func(it *protocol.Item) {
		it.Name = "Ico\x1bn"
		it.Paths = []string{"/x/Icon\r"}
		it.Commands = map[protocol.Action]string{protocol.Delete: ctrlCmd}
	}}, mods...)...)
}

func TestReviewShowsControlCharactersEscaped(t *testing.T) {
	m := reviewing(t, scan.Result{Script: "s", Items: []protocol.Item{ctrlItem(), item("s", "plain", 5_000_000)}})
	v := m.View()
	noRaw(t, v)
	for _, want := range []string{`rm -rf -- '/x/Icon\r' && printf '\x1b[K'`, `Ico\x1bn`, "contains control characters, shown escaped", "[ run ]"} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q in:\n%s", want, v)
		}
	}
	if n := strings.Count(v, "contains control characters"); n != 1 {
		t.Fatalf("want the note once (only on the item with control characters), got %d:\n%s", n, v)
	}
	m, cmd := send(m, tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter did not start the run")
	}
	run, ok := cmd().(startRunMsg)
	if !ok {
		t.Fatal("want startRunMsg")
	}
	if run.entries[0].Command != ctrlCmd || run.entries[0].Item.Commands[protocol.Delete] != ctrlCmd {
		t.Fatalf("command changed: %q", run.entries[0].Command)
	}
	_ = m
}

func TestReviewControlCharacterInPathOnly(t *testing.T) {
	it := item("s", "p", 5_000_000, func(it *protocol.Item) { it.Paths = []string{"/a\x7f"} })
	v := reviewing(t, scan.Result{Script: "s", Items: []protocol.Item{it}}).View()
	if !strings.Contains(v, "contains control characters, shown escaped") {
		t.Fatalf("missing note:\n%s", v)
	}
}

func TestListShowsControlCharactersEscaped(t *testing.T) {
	d, _ := fakeDeps()
	warn := func(it *protocol.Item) { it.Warnings = []string{"/x/Icon\r is not writable by you"} }
	m := loaded(t, *d, scan.Result{Script: "s", Items: []protocol.Item{ctrlItem(warn)}})
	m.width, m.height = 120, 30
	v := m.View()
	noRaw(t, v)
	for _, want := range []string{`Ico\x1bn`, `/x/Icon\r`} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q in:\n%s", want, v)
		}
	}
}

func TestViewShowsControlCharactersEscaped(t *testing.T) {
	d, _ := fakeDeps()
	inView := func(it *protocol.Item) { it.View = "Caches" }
	m := loaded(t, *d, scan.Result{Script: "s", Items: []protocol.Item{ctrlItem(inView)}})
	m.width, m.height = 120, 30
	m = press(m, "enter")
	if m.screen != screenView {
		t.Fatal("not in view")
	}
	v := m.View()
	noRaw(t, v)
	if !strings.Contains(v, `Ico\x1bn`) || !strings.Contains(v, `/x/Icon\r`) {
		t.Fatalf("name or path not escaped:\n%s", v)
	}
}

func TestRunAndResultShowControlCharactersEscaped(t *testing.T) {
	d, _ := fakeDeps()
	var ran []string
	d.Run = func(_ context.Context, it protocol.Item, a protocol.Action) action.Outcome {
		ran = append(ran, it.Commands[a])
		return action.Outcome{Key: it.Key(), Action: a, Command: it.Commands[a], Err: errors.New("exit status 1")}
	}
	m, cmd := confirm(t, *d, scan.Result{Script: "s", Items: []protocol.Item{ctrlItem()}})
	m, cmd = send(m, cmd())
	if m.screen != screenRun {
		t.Fatal("not running")
	}
	v := m.View()
	noRaw(t, v)
	if !strings.Contains(v, `Ico\x1bn`) {
		t.Fatalf("run screen name not escaped:\n%s", v)
	}
	m, _ = pump(t, m, cmd)
	if m.screen != screenResult {
		t.Fatal("not on result screen")
	}
	if len(ran) != 1 || ran[0] != ctrlCmd {
		t.Fatalf("ran %q, want %q", ran, ctrlCmd)
	}
	v = m.View()
	noRaw(t, v)
	for _, want := range []string{`Ico\x1bn`, `$ rm -rf -- '/x/Icon\r' && printf '\x1b[K'`} {
		if !strings.Contains(v, want) {
			t.Fatalf("missing %q in:\n%s", want, v)
		}
	}
}
