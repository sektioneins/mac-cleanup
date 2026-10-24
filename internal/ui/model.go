// Package ui is the Bubble Tea interface: list, review and result screens.
package ui

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"mac-cleanup/internal/action"
	"mac-cleanup/internal/protocol"
	"mac-cleanup/internal/scan"
)

// MinSize is the smallest item size that is listed.
const MinSize = 1_000_000

// Deps are the side effects the UI uses, replaceable in tests.
type Deps struct {
	Scan func(script string) scan.Result
	// Run runs the item's command and copies its output to out as it comes.
	Run     func(ctx context.Context, it protocol.Item, a protocol.Action, out io.Writer) action.Outcome
	Reveal  func(path string) error
	Free    func() (free, total uint64, err error)
	SudoCmd func() *exec.Cmd
	// SudoRefresh renews the sudo ticket without prompting; it errors if the
	// ticket has expired.
	SudoRefresh func() error
	// OpenLog creates the log file of one run and returns it with its path.
	OpenLog func() (io.WriteCloser, string, error)
}

// DefaultDeps wires the real scanner, runner and free-space probe for freePath.
func DefaultDeps(freePath string) Deps {
	return Deps{
		Scan: func(s string) scan.Result { return scan.Run(context.Background(), s) },
		Run: func(ctx context.Context, it protocol.Item, a protocol.Action, out io.Writer) action.Outcome {
			return action.Run(ctx, it, a, out)
		},
		Reveal:      action.Reveal,
		Free:        func() (uint64, uint64, error) { return action.FreeSpace(freePath) },
		SudoCmd:     action.SudoCmd,
		SudoRefresh: action.SudoRefresh,
		OpenLog:     openLog,
	}
}

// openLog creates ~/Library/Logs/mac-cleanup/<date-time>.log.
func openLog() (io.WriteCloser, string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, "", err
	}
	dir := filepath.Join(home, "Library", "Logs", "mac-cleanup")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, "", err
	}
	path := filepath.Join(dir, time.Now().Format("2006-01-02-150405")+".log")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, "", err
	}
	return f, path, nil
}

type scanMsg scan.Result

// genScanMsg is a scan result from Init; results of an earlier scan round
// (before a rescan) are dropped.
type genScanMsg struct {
	gen int
	res scan.Result
}

type freeMsg struct {
	free, total uint64
	err         error
}

type screen int

const (
	screenList screen = iota
	screenView
	screenReview
	screenRun
	screenResult
)

// Model is the whole UI state.
type Model struct {
	deps    Deps
	scripts []string
	gen     int           // scan round, raised by each rescan
	results []scan.Result // in arrival order
	free    freeMsg
	hasFree bool

	selected map[string]bool
	screen   screen
	tab      tab       // list screen: the showing tab
	parked   tabCursor // list screen: cursor of the other tab
	cursor   int       // list screen: index into navigable rows (fallback)
	curID    string    // identity of the row under the cursor
	vcurID   string    // identity of the item under the view cursor
	view     string
	viewCat  string // category the open view was entered from
	vcursor  int    // view screen: index into the view's items

	revKeys   []string                   // review: item keys in list order
	ract      map[string]protocol.Action // review: chosen action per key
	rcurID    string                     // review: key of the item under the cursor
	rcursor   int                        // review: fallback index
	notes     []string                   // review: messages from re-checks
	revScroll int                        // review: first visible body line
	rfocus    reviewFocus                // review: group that has the keyboard
	rbutton   reviewButton               // review: focused button
	running   bool                       // review confirmed; later re-checks are ignored
	run       runState                   // running and result screens

	width, height int
}

// New creates the model; Init starts one scan per script and a free-space probe.
func New(scripts []string, d Deps) Model {
	return Model{deps: d, scripts: scripts, selected: map[string]bool{}, width: 80, height: 24}
}

func (m Model) Init() tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(m.scripts)+1)
	for _, s := range m.scripts {
		s := s
		gen := m.gen
		cmds = append(cmds, func() tea.Msg { return genScanMsg{gen, m.deps.Scan(s)} })
	}
	cmds = append(cmds, func() tea.Msg {
		f, t, err := m.deps.Free()
		return freeMsg{f, t, err}
	})
	return tea.Batch(cmds...)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case genScanMsg:
		if msg.gen != m.gen {
			return m, nil
		}
		return m.Update(scanMsg(msg.res))
	case scanMsg:
		m.pinCursors()
		m.results = append(m.results, scan.Result(msg))
	case recheckMsg:
		if !m.running && msg.gen == m.gen {
			m.applyRecheck(msg.res)
		}
	case freeMsg:
		m.free, m.hasFree = msg, true
	case startRunMsg:
		return m.startRun(msg)
	case sudoMsg:
		return m.afterSudo(msg)
	case refreshMsg:
		return m.afterRefresh(msg)
	case outcomeMsg:
		return m.afterOutcome(msg)
	case outputMsg:
		return m.afterOutput(msg)
	case tea.KeyMsg:
		if m.screen == screenRun {
			return m.updateRun(msg)
		}
		if msg.String() == "ctrl+c" {
			if m.running && m.screen == screenReview {
				return m, nil // the run is about to start and takes over stopping
			}
			return m, tea.Quit
		}
		switch m.screen {
		case screenList:
			return m.updateList(msg)
		case screenView:
			return m.updateView(msg)
		case screenReview:
			return m.updateReview(msg)
		case screenResult:
			return m.updateResult(msg)
		}
	}
	return m, nil
}

func (m Model) View() string {
	switch m.screen {
	case screenView:
		return m.viewScreen()
	case screenReview:
		return m.reviewScreen()
	case screenRun:
		return m.runScreen()
	case screenResult:
		return m.resultScreen()
	}
	return m.listScreen()
}

// visibleItems returns the listed items of the showing tab.
func (m Model) visibleItems() []protocol.Item { return m.itemsOf(m.tab) }

// selectedTotals counts the selected items of both tabs: the disk size of the
// Space ones and the memory of the Autostart ones.
func (m Model) selectedTotals() (n int, disk, memory int64) {
	for _, it := range m.allItems() {
		if !m.selected[it.Key()] {
			continue
		}
		n++
		if it.Autostart() {
			memory += it.Memory
		} else {
			disk += it.Size
		}
	}
	return
}

func (m Model) scanFailures() []scan.Result {
	var out []scan.Result
	for _, r := range m.results {
		if r.Err != nil {
			out = append(out, r)
		}
	}
	return out
}

func sortBySize(items []protocol.Item) {
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Size != items[j].Size {
			return items[i].Size > items[j].Size
		}
		return items[i].Key() < items[j].Key()
	})
}
