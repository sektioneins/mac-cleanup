// Package ui is the Bubble Tea interface: list, review and result screens.
package ui

import (
	"context"
	"os/exec"
	"sort"

	tea "github.com/charmbracelet/bubbletea"

	"mac-cleanup/internal/action"
	"mac-cleanup/internal/protocol"
	"mac-cleanup/internal/scan"
)

// MinSize is the smallest item size that is listed.
const MinSize = 1_000_000

// Deps are the side effects the UI uses, replaceable in tests.
type Deps struct {
	Scan    func(script string) scan.Result
	Run     func(it protocol.Item, a protocol.Action) action.Outcome
	Reveal  func(path string) error
	Free    func() (free, total uint64, err error)
	SudoCmd func() *exec.Cmd
}

// DefaultDeps wires the real scanner, runner and free-space probe for freePath.
func DefaultDeps(freePath string) Deps {
	return Deps{
		Scan: func(s string) scan.Result { return scan.Run(context.Background(), s) },
		Run: func(it protocol.Item, a protocol.Action) action.Outcome {
			return action.Run(context.Background(), it, a)
		},
		Reveal:  action.Reveal,
		Free:    func() (uint64, uint64, error) { return action.FreeSpace(freePath) },
		SudoCmd: action.SudoCmd,
	}
}

type scanMsg scan.Result

type freeMsg struct {
	free, total uint64
	err         error
}

type screen int

const (
	screenList screen = iota
	screenView
	screenReview
)

// Model is the whole UI state.
type Model struct {
	deps    Deps
	scripts []string
	results []scan.Result // in arrival order
	free    freeMsg
	hasFree bool

	selected map[string]bool
	screen   screen
	cursor   int    // list screen: index into navigable rows (fallback)
	curID    string // identity of the row under the cursor
	vcurID   string // identity of the item under the view cursor
	view     string
	viewCat  string // category the open view was entered from
	vcursor  int    // view screen: index into the view's items

	revKeys   []string                   // review: item keys in list order
	ract      map[string]protocol.Action // review: chosen action per key
	rcurID    string                     // review: key of the item under the cursor
	rcursor   int                        // review: fallback index
	notes     []string                   // review: messages from re-checks
	revScroll int                        // review: first visible body line
	running   bool                       // review confirmed; later re-checks are ignored

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
		cmds = append(cmds, func() tea.Msg { return scanMsg(m.deps.Scan(s)) })
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
	case scanMsg:
		m.pinCursors()
		m.results = append(m.results, scan.Result(msg))
	case recheckMsg:
		if !m.running {
			m.applyRecheck(scan.Result(msg))
		}
	case freeMsg:
		m.free, m.hasFree = msg, true
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return m, tea.Quit
		}
		switch m.screen {
		case screenList:
			return m.updateList(msg)
		case screenView:
			return m.updateView(msg)
		case screenReview:
			return m.updateReview(msg)
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
	}
	return m.listScreen()
}

// visibleItems returns all listed items (at least MinSize).
func (m Model) visibleItems() []protocol.Item {
	var out []protocol.Item
	for _, r := range m.results {
		for _, it := range r.Items {
			if it.Size >= MinSize {
				out = append(out, it)
			}
		}
	}
	return out
}

func (m Model) selectedTotals() (n int, size int64) {
	for _, it := range m.visibleItems() {
		if m.selected[it.Key()] {
			n++
			size += it.Size
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
