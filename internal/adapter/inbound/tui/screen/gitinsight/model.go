// Package gitinsight é a tela da tool "Radar Git do dev".
package gitinsight

import (
	"context"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	core "github.com/mateuslh/lealing/internal/core/gitinsight"
)

// ScreenID identifica esta tela.
const ScreenID tui.ScreenID = "tool/git-dev-radar"

type filter uint8

const (
	filterAll filter = iota
	filterPush
	filterCleanup
	filterUntracked
	filterDirty
	filterCount
)

var filterLabels = [...]string{"todos", "para push", "locais publicadas", "sem upstream", "alterados"}

type mode uint8

const (
	modeBrowse mode = iota
	modePick
	modeConfirm
	modeRunning
	modeResults
)

type action uint8

const (
	actionNone action = iota
	actionPush
	actionDelete
	actionFetch
	actionUpdateAll
)

type operation struct {
	action action
	repo   core.Repository
	branch core.Branch
	total  int
}

// Model é o estado da tela.
type Model struct {
	deps    tui.Deps
	manager core.Manager

	width, height int
	report        core.Report
	loading       bool
	err           error

	filter       filter
	cursor       int
	detailOffset int

	mode         mode
	action       action
	actionCursor int
	operation    operation
	feedback     string
	feedbackErr  bool
	updateReport core.UpdateReport
	resultCursor int
}

var _ tui.Screen = (*Model)(nil)

// New monta a tela.
func New(deps tui.Deps, manager core.Manager) *Model {
	return &Model{deps: deps, manager: manager, loading: true}
}

// ID implementa tui.Screen.
func (*Model) ID() tui.ScreenID { return ScreenID }

// Title implementa tui.Screen.
func (*Model) Title() string { return "radar git do dev" }

// Init implementa tui.Screen.
func (m *Model) Init() tea.Cmd { return m.load() }

type reportMsg struct {
	report core.Report
	err    error
}

type actionMsg struct {
	operation    operation
	updateReport core.UpdateReport
	err          error
}

const scanTimeout = 45 * time.Second
const actionTimeout = 2 * time.Minute
const updateAllTimeout = 10 * time.Minute

func (m *Model) load() tea.Cmd {
	manager := m.manager
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), scanTimeout)
		defer cancel()
		report, err := manager.Scan(ctx)
		return reportMsg{report: report, err: err}
	}
}

func (m *Model) run(operation operation) tea.Cmd {
	manager := m.manager
	return func() tea.Msg {
		timeout := actionTimeout
		if operation.action == actionUpdateAll {
			timeout = updateAllTimeout
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		var err error
		var updateReport core.UpdateReport
		switch operation.action {
		case actionPush:
			err = manager.Push(ctx, operation.repo.Path, operation.branch)
		case actionDelete:
			err = manager.DeleteLocalBranch(ctx, operation.repo.Path, operation.branch.Name)
		case actionFetch:
			err = manager.Fetch(ctx, operation.repo.Path)
		case actionUpdateAll:
			updateReport, err = manager.UpdateAll(ctx)
		default:
			err = fmt.Errorf("ação Git desconhecida")
		}
		return actionMsg{operation: operation, updateReport: updateReport, err: err}
	}
}

func (m *Model) actionBranches() []core.Branch {
	repo, ok := m.selected()
	if !ok {
		return nil
	}
	switch m.action {
	case actionPush:
		return repo.PushBranches()
	case actionDelete:
		return repo.CleanupBranches()
	default:
		return nil
	}
}

// Capturing impede que o chrome consuma Esc enquanto há um seletor ou uma
// confirmação aberta.
func (m *Model) Capturing() bool { return m.mode != modeBrowse }

func (m *Model) repositories() []core.Repository {
	repos := make([]core.Repository, 0, len(m.report.Repositories))
	for _, repo := range m.report.Repositories {
		if m.matches(repo) {
			repos = append(repos, repo)
		}
	}
	return repos
}

func (m *Model) matches(repo core.Repository) bool {
	return matchesFilter(m.filter, repo)
}

func matchesFilter(active filter, repo core.Repository) bool {
	switch active {
	case filterPush:
		return len(repo.PushBranches()) > 0
	case filterCleanup:
		return len(repo.CleanupBranches()) > 0
	case filterUntracked:
		return len(repo.UntrackedBranches()) > 0
	case filterDirty:
		return repo.DirtyFiles > 0 || repo.Err != ""
	default:
		return true
	}
}

func (m *Model) filterCount(active filter) int {
	count := 0
	for _, repo := range m.report.Repositories {
		if matchesFilter(active, repo) {
			count++
		}
	}
	return count
}

func (m *Model) selected() (core.Repository, bool) {
	repos := m.repositories()
	if m.cursor < 0 || m.cursor >= len(repos) {
		return core.Repository{}, false
	}
	return repos[m.cursor], true
}

func (m *Model) clampCursor() {
	m.cursor = min(m.cursor, max(len(m.repositories())-1, 0))
}
