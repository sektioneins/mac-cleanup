package ui

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"mac-cleanup/internal/protocol"
	"mac-cleanup/internal/scan"
)

func TestHumanSize(t *testing.T) {
	for in, want := range map[int64]string{
		0: "0 B", 12_100_000_000: "12.1 GB", 640_000_000: "640 MB", 12_000: "12 KB",
	} {
		if got := HumanSize(in); got != want {
			t.Errorf("HumanSize(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestNothingPreselected(t *testing.T) {
	d, _ := fakeDeps()
	m := loaded(t, *d,
		scan.Result{Script: "a", Items: []protocol.Item{item("a", "x", 5_000_000)}},
		scan.Result{Script: "b", Items: []protocol.Item{item("b", "y", 6_000_000)}})
	v := m.View()
	if !strings.Contains(v, "selected: 0 items") {
		t.Fatalf("missing footer:\n%s", v)
	}
	for _, s := range []string{"mac-cleanup", "free: 41.2 GB of 994 GB", "[Space]"} {
		if !strings.Contains(v, s) {
			t.Errorf("missing %q", s)
		}
	}
}

func TestGroupingAndOrder(t *testing.T) {
	d, _ := fakeDeps()
	cat := func(c string) func(*protocol.Item) { return func(i *protocol.Item) { i.Category = c } }
	m := loaded(t, *d, scan.Result{Script: "a", Items: []protocol.Item{
		item("a", "small-dev", 2_000_000, cat("Developer")),
		item("a", "big-dev", 3_000_000_000, cat("Developer")),
		item("a", "browser", 4_000_000_000, cat("Browsers")),
	}})
	v := m.View()
	before(t, v, "Browsers", "Developer")
	before(t, v, "big-dev", "small-dev")
	before(t, v, "Developer", "big-dev")
	for _, s := range []string{"4 GB", "3 GB", "regenerates"} {
		if !strings.Contains(v, s) {
			t.Errorf("missing %q in:\n%s", s, v)
		}
	}
}

func TestUnusedAppsSortedBySize(t *testing.T) {
	d, _ := fakeDeps()
	used := func(n int64) func(*protocol.Item) {
		return func(i *protocol.Item) { i.LastUsed = n; i.Category = "Unused apps" }
	}
	m := loaded(t, *d, scan.Result{Script: "a", Items: []protocol.Item{
		item("a", "older-small", 2_000_000, used(100)),
		item("a", "newer-big", 9_000_000_000, used(200)),
		item("a", "unknown-mid", 5_000_000, used(0)),
	}})
	v := m.View()
	before(t, v, "newer-big", "unknown-mid")
	before(t, v, "unknown-mid", "older-small")
}

func TestSmallItemsHidden(t *testing.T) {
	d, _ := fakeDeps()
	m := loaded(t, *d, scan.Result{Script: "a", Items: []protocol.Item{
		item("a", "tiny", MinSize-1), item("a", "fine", MinSize)}})
	v := m.View()
	if strings.Contains(v, "tiny") || !strings.Contains(v, "fine") {
		t.Fatalf("bad filtering:\n%s", v)
	}
}

func TestToggleSelection(t *testing.T) {
	d, _ := fakeDeps()
	m := loaded(t, *d, scan.Result{Script: "a", Items: []protocol.Item{
		item("a", "big", 3_000_000_000), item("a", "mid", 2_000_000_000)}})
	m = press(m, "space")
	v := m.View()
	if !strings.Contains(v, "selected: 1 item, 3 GB") || !strings.Contains(v, "[x] big") {
		t.Fatalf("not selected:\n%s", v)
	}
	m = press(m, "space")
	if !strings.Contains(m.View(), "selected: 0 items") {
		t.Fatal("not deselected")
	}
	m = press(m, "j", "space", "k", "j")
	if !strings.Contains(m.View(), "[x] mid") {
		t.Fatal("j/k navigation broken")
	}
}

func TestRevealUsesFirstPath(t *testing.T) {
	d, c := fakeDeps()
	m := loaded(t, *d, scan.Result{Script: "a", Items: []protocol.Item{
		item("a", "big", 3_000_000_000, func(i *protocol.Item) { i.Paths = []string{"/p/one", "/p/two"} }),
		item("a", "nopath", 2_000_000_000)}})
	m = press(m, "o")
	if len(c.reveals) != 1 || c.reveals[0] != "/p/one" {
		t.Fatalf("reveals = %v", c.reveals)
	}
	press(m, "down", "o")
	if len(c.reveals) != 1 {
		t.Fatalf("pathless item revealed: %v", c.reveals)
	}
}

func TestEnterNeedsSelection(t *testing.T) {
	d, _ := fakeDeps()
	m := loaded(t, *d, scan.Result{Script: "a", Items: []protocol.Item{item("a", "big", 3_000_000_000)}})
	m = press(m, "enter")
	if m.screen != screenList {
		t.Fatal("opened review without selection")
	}
	m = press(m, "space", "enter")
	if m.screen != screenReview {
		t.Fatal("review not opened")
	}
}

func TestQuit(t *testing.T) {
	d, _ := fakeDeps()
	m := loaded(t, *d)
	for _, k := range []string{"q", "ctrl+c"} {
		_, cmd := send(m, keyFor(k))
		if cmd == nil {
			t.Fatalf("%s gave no command", k)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("%s did not quit", k)
		}
	}
}

func keyFor(k string) tea.KeyMsg {
	if k == "ctrl+c" {
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
}

func TestScanFailureShown(t *testing.T) {
	d, _ := fakeDeps()
	m := loaded(t, *d,
		scan.Result{Script: "bad.sh", Err: errors.New("boom happened\nsecond line")},
		scan.Result{Script: "ok.sh", Items: []protocol.Item{item("ok.sh", "survivor", 3_000_000_000)}})
	v := m.View()
	if !strings.Contains(v, "scan failed: bad.sh: boom happened") || strings.Contains(v, "second line") {
		t.Fatalf("failure line wrong:\n%s", v)
	}
	if !strings.Contains(v, "survivor") {
		t.Fatal("other items missing")
	}
}

func TestScanProgress(t *testing.T) {
	d, _ := fakeDeps()
	m := New([]string{"a", "b"}, *d)
	if !strings.Contains(m.View(), "scanning 0/2") {
		t.Fatalf("no initial progress:\n%s", m.View())
	}
	m, _ = send(m, scanMsg(scan.Result{Script: "a"}))
	if !strings.Contains(m.View(), "scanning 1/2") {
		t.Fatal("no progress after one result")
	}
	if !strings.Contains(m.View(), "scanning 1/2 "+strings.Repeat("█", 10)+strings.Repeat("░", 10)) {
		t.Fatalf("no progress bar:\n%s", m.View())
	}
	m, _ = send(m, scanMsg(scan.Result{Script: "b"}))
	if strings.Contains(m.View(), "scanning") || strings.Contains(m.View(), "░") {
		t.Fatal("still scanning")
	}
}

// Next to the progress bar the header names what the round waits for, as far
// as the width allows.
func TestScanProgressNames(t *testing.T) {
	d, _ := fakeDeps()
	m := New([]string{"/s/targets/brew.sh", "/s/targets/cargo.sh", "/s/targets/autostart-kext.sh"}, *d)
	line := func(m Model) string { return strings.Split(m.View(), "\n")[1] }
	if l := line(m); !strings.HasSuffix(l, "░  shared sources") {
		t.Fatalf("shared step: %q", l)
	}
	m, _ = send(m, sharedMsg{tab: tabSpace})
	if l := line(m); !strings.HasSuffix(l, "░  brew, cargo") {
		t.Fatalf("scans: %q", l)
	}
	m, _ = send(m, scanMsg(scan.Result{Script: "brew.sh"}))
	if l := line(m); !strings.HasSuffix(l, "░  cargo") {
		t.Fatalf("after one result: %q", l)
	}
	m.results = nil
	for w, want := range map[int]string{
		70: "░  brew, cargo", // 56 cells before the names, one kept free
		69: "░  brew +1",
		66: "░  brew +1",
		65: "░",
	} {
		m.width = w
		if l := line(m); !strings.HasSuffix(l, want) {
			t.Errorf("width %d: %q, want suffix %q", w, l, want)
		}
	}
}

func TestProgressBar(t *testing.T) {
	for _, c := range []struct {
		done, total int
		want        string
	}{
		{0, 3, strings.Repeat("░", 20)},
		{1, 3, strings.Repeat("█", 6) + strings.Repeat("░", 14)},
		{3, 3, strings.Repeat("█", 20)},
		{0, 0, strings.Repeat("░", 20)},
	} {
		if got := progressBar(c.done, c.total, 20); got != c.want {
			t.Errorf("progressBar(%d, %d) = %q, want %q", c.done, c.total, got, c.want)
		}
	}
}

func TestInitStartsScans(t *testing.T) {
	d, _ := fakeDeps()
	m := New([]string{"a", "b"}, *d)
	if m.Init() == nil {
		t.Fatal("Init returned no command")
	}
}

func cacheItems() []protocol.Item {
	v := func(i *protocol.Item) { i.View = "~/Library/Caches"; i.Category = "Library" }
	return []protocol.Item{
		item("a", "com.spotify", 2_400_000_000, v),
		item("a", "Google", 1_900_000_000, v),
		item("a", "com.apple.Safari", 700_000_000, v),
		item("a", "Logs", 1_200_000_000, func(i *protocol.Item) { i.Category = "Library" }),
	}
}

func TestViewRowSummarises(t *testing.T) {
	d, _ := fakeDeps()
	m := loaded(t, *d, scan.Result{Script: "a", Items: cacheItems()})
	v := m.View()
	if !strings.Contains(v, "~/Library/Caches  (3 folders)  5 GB  enter to open") {
		t.Fatalf("no summary row:\n%s", v)
	}
	for _, n := range []string{"com.spotify", "Google", "com.apple.Safari"} {
		if strings.Contains(v, n) {
			t.Errorf("%s listed in main list", n)
		}
	}
	before(t, v, "~/Library/Caches", "Logs")
}

func TestViewRowNotSelectable(t *testing.T) {
	d, _ := fakeDeps()
	m := loaded(t, *d, scan.Result{Script: "a", Items: cacheItems()})
	m = press(m, "space")
	if !strings.Contains(m.View(), "selected: 0 items") {
		t.Fatal("view row got selected")
	}
}

func TestViewOpenSelectBack(t *testing.T) {
	d, _ := fakeDeps()
	m := loaded(t, *d, scan.Result{Script: "a", Items: cacheItems()})
	m = press(m, "enter")
	v := m.View()
	if m.screen != screenView || !strings.Contains(v, "selected: 0 of 3, 0 B") {
		t.Fatalf("view not open:\n%s", v)
	}
	before(t, v, "com.spotify", "Google")
	before(t, v, "Google", "com.apple.Safari")
	m = press(m, "space")
	if !strings.Contains(m.View(), "selected: 1 of 3, 2.4 GB") {
		t.Fatalf("not selected in view:\n%s", m.View())
	}
	m = press(m, "esc")
	v = m.View()
	if m.screen != screenList || !strings.Contains(v, "selected: 1 item, 2.4 GB") {
		t.Fatalf("back on list wrong:\n%s", v)
	}
	// The cursor is still on the view row, so enter reopens the view.
	m = press(m, "enter")
	if m.screen != screenView {
		t.Fatal("enter on view row should open the view")
	}
}

func TestCursorFollowsRowOnRescan(t *testing.T) {
	d, _ := fakeDeps()
	m := New([]string{"a", "b"}, *d)
	m, _ = send(m, scanMsg(scan.Result{Script: "a", Items: []protocol.Item{
		item("a", "top", 5_000_000_000), item("a", "x", 3_000_000_000)}}))
	m = press(m, "down")
	m, _ = send(m, scanMsg(scan.Result{Script: "b", Items: []protocol.Item{item("b", "new", 9_000_000_000)}}))
	m = press(m, "space")
	v := m.View()
	if !strings.Contains(v, "[x] x") || strings.Contains(v, "[x] new") || !strings.Contains(v, "selected: 1 item,") {
		t.Fatalf("space hit the wrong row:\n%s", v)
	}
}

func TestViewCursorFollowsRowOnRescan(t *testing.T) {
	d, _ := fakeDeps()
	v := func(i *protocol.Item) { i.View = "V" }
	m := New([]string{"a", "b"}, *d)
	m, _ = send(m, scanMsg(scan.Result{Script: "a", Items: []protocol.Item{
		item("a", "first", 5_000_000_000, v), item("a", "second", 3_000_000_000, v)}}))
	m = press(m, "enter", "down")
	m, _ = send(m, scanMsg(scan.Result{Script: "b", Items: []protocol.Item{item("b", "new", 9_000_000_000, v)}}))
	m = press(m, "space")
	out := m.View()
	if !strings.Contains(out, "[x] second") || strings.Contains(out, "[x] new") {
		t.Fatalf("wrong row selected:\n%s", out)
	}
}

func TestLongExplanationWrapped(t *testing.T) {
	d, _ := fakeDeps()
	long := strings.Repeat("Downloaded build dependencies are fetched again. ", 3) + "Stop gradle daemons first."
	m := loaded(t, *d, scan.Result{Script: "a", Items: []protocol.Item{
		item("a", "g", 3_000_000_000, func(i *protocol.Item) { i.Explain = long })}})
	if v := m.View(); !strings.Contains(v, "gradle daemons first.") {
		t.Fatalf("end of explanation lost:\n%s", v)
	}
}

func TestPlurals(t *testing.T) {
	d, _ := fakeDeps()
	m := loaded(t, *d, scan.Result{Script: "a", Items: []protocol.Item{
		item("a", "one", 3_000_000_000, func(i *protocol.Item) { i.View = "V" })}})
	if !strings.Contains(m.View(), "(1 folder)") {
		t.Fatalf("folder plural:\n%s", m.View())
	}
	m = loaded(t, *d, scan.Result{Script: "a", Items: []protocol.Item{
		item("a", "x", 3_000_000_000), item("a", "y", 2_000_000_000)}})
	m = press(m, "space", "down", "space")
	if !strings.Contains(m.View(), "selected: 2 items,") {
		t.Fatal("2 items")
	}
}

func TestScanFailureStaysVisibleWhenScrolled(t *testing.T) {
	d, _ := fakeDeps()
	var items []protocol.Item
	for i := 0; i < 40; i++ {
		items = append(items, item("a", "item"+itoa(i), int64(40-i)*100_000_000))
	}
	m := New([]string{"a", "bad"}, *d)
	m, _ = send(m, tea.WindowSizeMsg{Width: 80, Height: 20})
	m, _ = send(m, scanMsg(scan.Result{Script: "a", Items: items}))
	m, _ = send(m, scanMsg(scan.Result{Script: "bad", Err: errors.New("nope")}))
	if v := m.View(); !strings.Contains(v, "scan failed: bad: nope") {
		t.Fatalf("failure hidden:\n%s", v)
	}
}

// Moving up from the bottom of the window moves the cursor first; the list
// scrolls only once the cursor is on the top line, and the first row of a
// group brings its heading along.
func TestListScrollsOnlyAtTheEdge(t *testing.T) {
	d, _ := fakeDeps()
	var items []protocol.Item
	for i := 0; i < 40; i++ {
		items = append(items, item("a", "item"+itoa(i), int64(40-i)*100_000_000))
	}
	m := New([]string{"a"}, *d)
	m, _ = send(m, tea.WindowSizeMsg{Width: 80, Height: 20})
	m, _ = send(m, scanMsg(scan.Result{Script: "a", Items: items}))
	// The body is the 9 lines after the 3 header lines.
	window := func() (first, cur string) {
		body := strings.Split(m.View(), "\n")[3:12]
		re := regexp.MustCompile(`item\d+|▾ Developer`)
		for _, l := range body {
			if strings.HasPrefix(l, "> ") || strings.Contains(l, "> [") {
				cur = re.FindString(l)
			}
		}
		return re.FindString(body[0]), cur
	}
	check := func(wantFirst, wantCur string) {
		t.Helper()
		if first, cur := window(); first != wantFirst || cur != wantCur {
			t.Fatalf("window starts at %q with the cursor on %q, want %q and %q:\n%s", first, cur, wantFirst, wantCur, m.View())
		}
	}
	for i := 0; i < 20; i++ {
		m = press(m, "down")
	}
	check("item12", "item20")
	m = press(m, "up")
	check("item12", "item19")
	for i := 0; i < 7; i++ {
		m = press(m, "up")
	}
	check("item12", "item12")
	m = press(m, "up")
	check("item11", "item11")
	m = press(m, "down", "down")
	check("item11", "item13")
	for i := 0; i < 13; i++ {
		m = press(m, "up")
	}
	check("▾ Developer", "item0")

	// The window survives a trip to the other tab.
	m = press(m, "down", "down", "down", "down", "down", "down", "down", "down", "down", "down")
	check("item2", "item10")
	m = press(m, "2", "1")
	check("item2", "item10")
}

func TestViewScrollsOnlyAtTheEdge(t *testing.T) {
	d, _ := fakeDeps()
	var items []protocol.Item
	for i := 0; i < 30; i++ {
		items = append(items, item("a", "dir"+itoa(i), int64(30-i)*100_000_000, func(i *protocol.Item) {
			i.View, i.Category = "~/Library/Caches", "Library"
		}))
	}
	m := New([]string{"a"}, *d)
	m, _ = send(m, tea.WindowSizeMsg{Width: 80, Height: 20})
	m, _ = send(m, scanMsg(scan.Result{Script: "a", Items: items}))
	m = press(m, "enter")
	// The body is the 11 lines after the 2 header lines.
	check := func(wantFirst, wantCur string) {
		t.Helper()
		body := strings.Split(m.View(), "\n")[2:13]
		re := regexp.MustCompile(`dir\d+`)
		cur := ""
		for _, l := range body {
			if strings.Contains(l, "> [") {
				cur = re.FindString(l)
			}
		}
		if first := re.FindString(body[0]); first != wantFirst || cur != wantCur {
			t.Fatalf("window starts at %q with the cursor on %q, want %q and %q:\n%s", first, cur, wantFirst, wantCur, m.View())
		}
	}
	for i := 0; i < 15; i++ {
		m = press(m, "down")
	}
	check("dir5", "dir15")
	m = press(m, "up")
	check("dir5", "dir14")
	for i := 0; i < 9; i++ {
		m = press(m, "up")
	}
	check("dir5", "dir5")
	m = press(m, "up")
	check("dir4", "dir4")
	// Reopening the view starts at the top.
	m = press(m, "esc", "enter")
	check("dir0", "dir0")
}
