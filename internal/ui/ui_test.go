package ui

import (
	"os/exec"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mac-cleanup/internal/action"
	"mac-cleanup/internal/protocol"
	"mac-cleanup/internal/scan"
)

type calls struct {
	reveals []string
}

func fakeDeps() (*Deps, *calls) {
	c := &calls{}
	d := &Deps{
		Scan: func(script string) scan.Result { return scan.Result{Script: script} },
		Run: func(it protocol.Item, a protocol.Action) action.Outcome {
			return action.Outcome{Key: it.Key(), Action: a}
		},
		Reveal:  func(p string) error { c.reveals = append(c.reveals, p); return nil },
		Free:    func() (uint64, uint64, error) { return 41_200_000_000, 994_000_000_000, nil },
		SudoCmd: func() *exec.Cmd { return exec.Command("true") },
	}
	return d, c
}

func send(m Model, msg tea.Msg) (Model, tea.Cmd) {
	nm, cmd := m.Update(msg)
	return nm.(Model), cmd
}

func press(m Model, keys ...string) Model {
	for _, k := range keys {
		var msg tea.KeyMsg
		switch k {
		case "up":
			msg = tea.KeyMsg{Type: tea.KeyUp}
		case "down":
			msg = tea.KeyMsg{Type: tea.KeyDown}
		case "enter":
			msg = tea.KeyMsg{Type: tea.KeyEnter}
		case "esc":
			msg = tea.KeyMsg{Type: tea.KeyEsc}
		case "ctrl+c":
			msg = tea.KeyMsg{Type: tea.KeyCtrlC}
		case "space":
			msg = tea.KeyMsg{Type: tea.KeySpace}
		default:
			msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
		}
		m, _ = send(m, msg)
	}
	return m
}

func item(script, id string, size int64, mods ...func(*protocol.Item)) protocol.Item {
	it := protocol.Item{
		Script: script, ID: id, Name: id, Category: "Developer", Size: size,
		Grade: protocol.Regenerates, Default: protocol.Delete,
		Commands: map[protocol.Action]string{protocol.Delete: "rm -rf x"},
	}
	for _, f := range mods {
		f(&it)
	}
	return it
}

// loaded builds a model and feeds it one scan result per given item set.
func loaded(t *testing.T, d Deps, results ...scan.Result) Model {
	t.Helper()
	scripts := make([]string, len(results))
	for i, r := range results {
		scripts[i] = r.Script
	}
	m := New(scripts, d)
	for _, r := range results {
		m, _ = send(m, scanMsg(r))
	}
	m, _ = send(m, freeMsg{free: 41_200_000_000, total: 994_000_000_000})
	return m
}

func before(t *testing.T, s, a, b string) {
	t.Helper()
	ia, ib := strings.Index(s, a), strings.Index(s, b)
	if ia < 0 || ib < 0 || ia >= ib {
		t.Fatalf("want %q before %q in:\n%s", a, b, s)
	}
}
