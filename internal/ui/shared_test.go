package ui

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mac-cleanup/internal/protocol"
	"mac-cleanup/internal/scan"
)

// drain runs cmd and every command that follows from it, feeding each
// message to the model, and returns the model when nothing is left.
func drain(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for n := 0; len(queue) > 0; n++ {
		if n > 1000 {
			t.Fatal("runaway")
		}
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		msg := c()
		if b, ok := msg.(tea.BatchMsg); ok {
			queue = append(queue, b...)
			continue
		}
		var next tea.Cmd
		m, next = send(m, msg)
		queue = append(queue, next)
	}
	return m
}

type scanCall struct {
	script string
	env    []string
}

// sharedDeps counts the shared steps (each returns MC_SHARED_X=<count>) and
// records the scans; a scan started before any shared step fails the test.
func sharedDeps(t *testing.T, items func(script string) []protocol.Item) (*Deps, *int, *[]scanCall) {
	d, _ := fakeDeps()
	shared := 0
	var scans []scanCall
	d.Shared = func() []string {
		shared++
		return []string{"MC_SHARED_X=" + strconv.Itoa(shared)}
	}
	d.Scan = func(s string, env []string) scan.Result {
		if shared == 0 {
			t.Errorf("scan of %s before the shared step", s)
		}
		scans = append(scans, scanCall{s, env})
		return scan.Result{Script: s, Items: items(s)}
	}
	return d, &shared, &scans
}

func checkScans(t *testing.T, scans []scanCall, want []string, env ...string) {
	t.Helper()
	if len(scans) != len(want) {
		t.Fatalf("scans %v, want %v", scans, want)
	}
	seen := map[string]bool{}
	for _, s := range scans {
		seen[s.script] = true
		if !reflect.DeepEqual(s.env, env) {
			t.Errorf("scan of %s got env %q, want %q", s.script, s.env, env)
		}
	}
	for _, w := range want {
		if !seen[w] {
			t.Errorf("no scan of %s in %v", w, scans)
		}
	}
}

func TestScanRoundSharedOnce(t *testing.T) {
	d, shared, scans := sharedDeps(t, func(string) []protocol.Item { return nil })
	scripts := []string{"a", "b", "c"}
	m := New(scripts, *d)
	m = drain(t, m, m.Init())
	if *shared != 1 {
		t.Fatalf("shared step ran %d times, want 1", *shared)
	}
	checkScans(t, *scans, scripts, "MC_SHARED_X=1")
	if len(m.results) != 3 {
		t.Fatalf("results %d", len(m.results))
	}

	// A rescan is a new round: one more shared step, its env for every scan.
	*scans = nil
	nm, cmd := m.rescan()
	m = drain(t, nm.(Model), cmd)
	if *shared != 2 {
		t.Fatalf("shared step ran %d times after the rescan, want 2", *shared)
	}
	checkScans(t, *scans, scripts, "MC_SHARED_X=2")
}

// While the shared step runs, the round shows as scanning; a shared result of
// an earlier round starts nothing.
func TestSharedStepPending(t *testing.T) {
	d, _, scans := sharedDeps(t, func(string) []protocol.Item { return nil })
	m := New([]string{"a", "b"}, *d)
	if v := m.View(); !strings.Contains(v, "scanning 0/2") {
		t.Fatalf("no progress while the shared step runs:\n%s", v)
	}
	m.gen = 1
	m, cmd := send(m, sharedMsg{gen: 0, env: []string{"MC_SHARED_X=old"}})
	if cmd != nil || m.env != nil {
		t.Fatalf("stale shared result accepted: cmd=%v env=%q", cmd != nil, m.env)
	}
	if len(*scans) != 0 {
		t.Fatalf("scans %v", *scans)
	}
}

func TestRecheckReusesRoundEnv(t *testing.T) {
	warn := true
	d, shared, scans := sharedDeps(t, func(s string) []protocol.Item {
		it := item(s, "g", 9_000_000)
		if warn {
			it.Warnings = []string{"busy"}
		}
		return []protocol.Item{it}
	})
	m := New([]string{"w"}, *d)
	m = drain(t, m, m.Init())
	for _, it := range m.visibleItems() {
		m.selected[it.Key()] = true
	}
	m = press(m, "enter")
	if m.screen != screenReview {
		t.Fatal("not on review screen")
	}
	*scans = nil
	warn = false
	m, cmd := send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if cmd == nil {
		t.Fatal("r returned no cmd")
	}
	m = drain(t, m, cmd)
	if *shared != 1 {
		t.Fatalf("re-check ran the shared step (%d runs)", *shared)
	}
	checkScans(t, *scans, []string{"w"}, "MC_SHARED_X=1")
	if !strings.Contains(m.View(), "[ run ]") {
		t.Fatalf("re-check not applied:\n%s", m.View())
	}
}

// A re-check runs the script by the path the round discovered, not by the
// base name the scan stores in the items.
func TestRecheckUsesScriptPath(t *testing.T) {
	warn := true
	d, _, scans := sharedDeps(t, func(s string) []protocol.Item {
		it := item(filepath.Base(s), "g", 9_000_000)
		if warn {
			it.Warnings = []string{"busy"}
		}
		return []protocol.Item{it}
	})
	m := New([]string{"/x/targets/a.sh", "/x/targets/b.sh"}, *d)
	m = drain(t, m, m.Init())
	m.selected["a.sh/g"] = true
	m = press(m, "enter")
	if m.screen != screenReview {
		t.Fatal("not on review screen")
	}
	*scans = nil
	warn = false
	m, cmd := send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = drain(t, m, cmd)
	if len(*scans) != 1 || (*scans)[0].script != "/x/targets/a.sh" {
		t.Fatalf("re-check scanned %v, want /x/targets/a.sh", *scans)
	}
	if !strings.Contains(m.View(), "[ run ]") {
		t.Fatalf("re-check not applied:\n%s", m.View())
	}
}

// A warned item whose script is not among the round's paths gets a failed
// re-check (the block stays), never a run of the bare name.
func TestRecheckUnknownScript(t *testing.T) {
	d, _ := fakeDeps()
	var scans []string
	d.Scan = func(s string, _ []string) scan.Result {
		scans = append(scans, s)
		return scan.Result{Script: s}
	}
	m := New([]string{"/x/targets/a.sh"}, *d)
	m, _ = send(m, scanMsg(scan.Result{Script: "other.sh", Items: []protocol.Item{
		item("other.sh", "g", 9_000_000, func(it *protocol.Item) { it.Warnings = []string{"busy"} }),
	}}))
	m.selected["other.sh/g"] = true
	m = press(m, "enter")
	m, cmd := send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = drain(t, m, cmd)
	if len(scans) != 0 {
		t.Fatalf("scanned %v", scans)
	}
	if v := m.View(); !strings.Contains(v, "re-check of other.sh failed") || !strings.Contains(v, "( run: blocked )") {
		t.Fatalf("want a failed re-check that keeps the block:\n%s", v)
	}
}

// With the real scanner, a re-check started from another working directory
// finds the script.
func TestRecheckRealScanOtherDir(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "targets", "w.sh")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "printf 'item\\nid\\tg\\nname\\tG\\ncategory\\tDeveloper\\nsize\\t9000000\\ngrade\\tregenerates\\ndefault\\tdelete\\nexplain\\te\\ncmd_delete\\ttrue\\nend\\n'\n"
	if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	d, _ := fakeDeps()
	d.Scan = func(s string, env []string) scan.Result { return scan.Run(context.Background(), s, env) }
	m := New([]string{script}, *d)
	m, _ = send(m, scanMsg(scan.Result{Script: "w.sh", Items: []protocol.Item{
		item("w.sh", "g", 9_000_000, func(it *protocol.Item) { it.Warnings = []string{"busy"} }),
	}}))
	m.selected["w.sh/g"] = true
	m = press(m, "enter")
	m, cmd := send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	m = drain(t, m, cmd)
	if v := m.View(); !strings.Contains(v, "[ run ]") || strings.Contains(v, "failed") {
		t.Fatalf("re-check did not find the script:\n%s", v)
	}
}
