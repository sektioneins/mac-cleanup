package ui

import (
	"github.com/charmbracelet/lipgloss"
	"strings"
	"testing"

	"mac-cleanup/internal/protocol"
	"mac-cleanup/internal/scan"
)

// auto builds an autostart item with the given memory and state.
func auto(id string, memory int64, state string, mods ...func(*protocol.Item)) protocol.Item {
	return item("s", id, 0, append([]func(*protocol.Item){func(i *protocol.Item) {
		i.Tab, i.Category, i.Memory, i.State = protocol.TabAutostart, "Login items", memory, state
	}}, mods...)...)
}

func mkTabs(t *testing.T, items ...protocol.Item) Model {
	t.Helper()
	d, _ := fakeDeps()
	return loaded(t, *d, scan.Result{Script: "s", Items: items})
}

func TestTabSwitch(t *testing.T) {
	m := mkTabs(t, item("s", "space-item", 5_000_000), auto("auto-item", 9_000_000, "running"))
	v := m.View()
	if !strings.Contains(v, "space-item") || strings.Contains(v, "auto-item") || !strings.Contains(v, " [Space]   Autostart") {
		t.Fatalf("space tab:\n%s", v)
	}
	m = press(m, "tab")
	v = m.View()
	if strings.Contains(v, "space-item") || !strings.Contains(v, "auto-item") || !strings.Contains(v, "[Autostart]") {
		t.Fatalf("autostart tab:\n%s", v)
	}
	if !strings.Contains(v, "running: 9 MB") || strings.Contains(v, "free:") {
		t.Errorf("autostart header:\n%s", v)
	}
	m = press(m, "1")
	if v = m.View(); !strings.Contains(v, "space-item") || strings.Contains(v, "auto-item") || !strings.Contains(v, "free:") {
		t.Fatalf("back on space:\n%s", v)
	}
	if m = press(m, "2"); m.tab != tabAutostart {
		t.Fatalf("2 did not switch")
	}
	if m = press(m, "tab"); m.tab != tabSpace {
		t.Fatalf("tab did not switch back")
	}
}

func TestTabKeepsCursor(t *testing.T) {
	m := mkTabs(t, item("s", "big", 9_000_000), item("s", "small", 5_000_000), auto("a1", 3_000_000, "running"), auto("a2", 1_000_000, "running"))
	m = press(m, "down", "tab", "down", "1")
	if m.curID != "s/small" {
		t.Fatalf("space cursor = %q", m.curID)
	}
	m = press(m, "2")
	if m.curID != "s/a2" {
		t.Fatalf("autostart cursor = %q", m.curID)
	}
	m = press(m, "1")
	if m.curID != "s/small" {
		t.Fatalf("space cursor after return = %q", m.curID)
	}
}

func TestAutostartListedBelowMinSize(t *testing.T) {
	m := mkTabs(t, auto("tiny", 0, "stopped"))
	if v := press(m, "tab").View(); !strings.Contains(v, "tiny") {
		t.Fatalf("not listed:\n%s", v)
	}
}

func TestAutostartSortByMemory(t *testing.T) {
	m := mkTabs(t,
		auto("five", 5_000_000, "running"),
		auto("stopped", 0, "stopped"),
		auto("eighty", 80_000_000, "running"),
		auto("b-tie", 5_000_000, "running"),
		auto("a-tie", 5_000_000, "running"))
	v := press(m, "tab").View()
	before(t, v, "eighty", "a-tie")
	before(t, v, "a-tie", "b-tie")
	before(t, v, "b-tie", "five")
	before(t, v, "five", "stopped")
}

func TestAutostartGroupsSortByMemory(t *testing.T) {
	cat := func(c string) func(*protocol.Item) { return func(i *protocol.Item) { i.Category = c } }
	m := mkTabs(t,
		auto("small", 1_000_000, "running", cat("Agents and daemons")),
		auto("big", 90_000_000, "running", cat("Login items")),
		auto("other", 2_000_000, "running", cat("Agents and daemons")))
	v := press(m, "tab").View()
	before(t, v, "Login items", "Agents and daemons")
	if !strings.Contains(v, "3 MB") {
		t.Errorf("group total missing:\n%s", v)
	}
}

func TestAutostartRow(t *testing.T) {
	m := mkTabs(t,
		auto("com.example.run", 80_000_000, "running", func(i *protocol.Item) { i.Enabled, i.Owner = "on", "Example App" }),
		auto("com.example.old", 0, "stopped", func(i *protocol.Item) { i.Orphan = "program missing" }),
		auto("com.example.idle", 0, "stopped", func(i *protocol.Item) { i.Enabled = "off" }),
		auto("com.example.driver", 24_000, "loaded"))
	m = press(m, "tab")
	line := func(name string) string {
		for _, l := range strings.Split(m.View(), "\n") {
			if strings.Contains(l, name) && strings.Contains(l, "[ ]") {
				return l
			}
		}
		t.Fatalf("no row for %s", name)
		return ""
	}
	for _, s := range []string{"80 MB", "● running", "on", "Example App"} {
		if l := line("com.example.run"); !strings.Contains(l, s) {
			t.Errorf("running row lacks %q: %q", s, l)
		}
	}
	if l := line("com.example.old"); !strings.Contains(l, "✗ orphan: program missing") || !strings.Contains(l, "–") || !strings.Contains(l, "○ stopped") {
		t.Errorf("orphan row: %q", l)
	}
	if l := line("com.example.idle"); !strings.Contains(l, "–") || !strings.Contains(l, "off") || !strings.Contains(l, "owner unknown") {
		t.Errorf("stopped row: %q", l)
	}
	if l := line("com.example.driver"); !strings.Contains(l, "● loaded") || !strings.Contains(l, "24 KB") {
		t.Errorf("loaded row: %q", l)
	}
}

func TestAutostartRunningSum(t *testing.T) {
	m := mkTabs(t,
		auto("a", 10_000_000, "running"),
		auto("b", 5_000_000, "loaded"),
		auto("c", 99_000_000, "stopped"))
	if v := press(m, "tab").View(); !strings.Contains(v, "running: 15 MB") {
		t.Fatalf("sum:\n%s", v)
	}
}

func TestNoActionNotSelectable(t *testing.T) {
	m := mkTabs(t, auto("sysext", 310_000_000, "running", func(i *protocol.Item) { i.Commands = nil }))
	m = press(m, "tab")
	if v := m.View(); !strings.Contains(v, "no action") || strings.Contains(v, "[ ]") {
		t.Fatalf("row:\n%s", v)
	}
	if m = press(m, "space"); len(m.selected) != 0 {
		t.Fatalf("selected %v", m.selected)
	}
}

func TestFooterDiskAndMemory(t *testing.T) {
	m := mkTabs(t, item("s", "sp", 5_000_000), auto("au", 7_000_000, "running"))
	m = press(m, "space", "tab", "space")
	v := m.View()
	for _, s := range []string{"selected: 2 items", "5 MB disk", "7 MB memory", "tab switch"} {
		if !strings.Contains(v, s) {
			t.Errorf("footer lacks %q:\n%s", s, v)
		}
	}
	m = press(m, "space")
	if v = m.View(); !strings.Contains(v, "selected: 1 item, 5 MB disk") || strings.Contains(v, "memory,") || strings.Contains(v, "MB memory") {
		t.Errorf("space-only footer:\n%s", v)
	}
}

func TestStateLabel(t *testing.T) {
	for in, want := range map[string]string{"running": "● running", "stopped": "○ stopped", "loaded": "● loaded", "x": "x"} {
		if got := stateLabel(in); got != want {
			t.Errorf("stateLabel(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAutostartRowsFitWidth(t *testing.T) {
	long := strings.Repeat("n", 120)
	m := mkTabs(t,
		auto(long, 80_000_000, "running", func(i *protocol.Item) { i.Enabled, i.Owner = "on", strings.Repeat("o", 60) }),
		auto("orphan", 0, "stopped", func(i *protocol.Item) { i.Orphan = "app deleted"; i.Commands = nil }))
	m = press(m, "tab")
	for _, l := range strings.Split(m.View(), "\n") {
		isRow := strings.Contains(l, "running") || strings.Contains(l, "stopped")
		if w := lipgloss.Width(l); isRow && w > m.width {
			t.Errorf("line is %d cells, want <= %d: %q", w, m.width, l)
		}
	}
}
