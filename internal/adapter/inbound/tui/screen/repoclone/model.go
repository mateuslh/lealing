// Package repoclone é a tela da tool "Clone Repo Bradesco".
package repoclone

import (
	"context"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	core "github.com/mateuslh/lealing/internal/core/repoclone"
)

// ScreenID identifica esta tela.
const ScreenID tui.ScreenID = "tool/clone-repo-bradesco"

type phase uint8

const (
	phaseInput phase = iota
	phaseDiscovering
	phaseConfirm
	phaseAdding
	phaseResolving
	phaseCloning
	phaseDone
)

// Model é o estado da tela.
type Model struct {
	deps     tui.Deps
	manager  core.Manager
	input    textinput.Model
	addInput textinput.Model

	width, height int
	phase         phase
	plan          core.Plan
	cursor        int
	included      []bool
	result        core.Result
	err           error
	feedback      string
}

var _ tui.Screen = (*Model)(nil)

// New monta a tela.
func New(deps tui.Deps, manager core.Manager) *Model {
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = "https://github.com/organizacao/projeto"
	in.CharLimit = 512
	in.Focus()

	add := textinput.New()
	add.Prompt = ""
	add.Placeholder = "nome-do-repo ou https://github.com/owner/repo"
	add.CharLimit = 512
	return &Model{deps: deps, manager: manager, input: in, addInput: add}
}

// ID implementa tui.Screen.
func (m *Model) ID() tui.ScreenID { return ScreenID }

// Title implementa tui.Screen.
func (m *Model) Title() string { return "clone repo bradesco" }

// Init implementa tui.Screen.
func (m *Model) Init() tea.Cmd { return textinput.Blink }

// Capturing impede os atalhos globais de consumirem letras da URL e bloqueia
// a saída enquanto git ou gh ainda mantêm processos em execução.
func (m *Model) Capturing() bool { return m.phase != phaseDone }

type discoveredMsg struct {
	plan core.Plan
	err  error
}

type clonedMsg struct {
	result core.Result
	err    error
}

type resolvedMsg struct {
	repository core.Repository
	err        error
}

const (
	discoverTimeout = 30 * time.Second
	cloneTimeout    = 30 * time.Minute
)

func (m *Model) discover() tea.Cmd {
	manager, raw := m.manager, m.input.Value()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), discoverTimeout)
		defer cancel()
		plan, err := manager.Discover(ctx, raw)
		return discoveredMsg{plan: plan, err: err}
	}
}

func (m *Model) clone() tea.Cmd {
	manager, plan := m.manager, m.selectedPlan()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), cloneTimeout)
		defer cancel()
		result, err := manager.Clone(ctx, plan)
		return clonedMsg{result: result, err: err}
	}
}

func (m *Model) resolve() tea.Cmd {
	manager, source, raw := m.manager, m.plan.Source, m.addInput.Value()
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), discoverTimeout)
		defer cancel()
		repo, err := manager.Resolve(ctx, source, raw)
		return resolvedMsg{repository: repo, err: err}
	}
}

func (m *Model) selectedPlan() core.Plan {
	plan := m.plan
	plan.Repositories = make([]core.Repository, 0, m.selectedCount())
	for i, repo := range m.plan.Repositories {
		if i < len(m.included) && m.included[i] {
			plan.Repositories = append(plan.Repositories, repo)
		}
	}
	return plan
}

func (m *Model) selectedCount() int {
	count := 0
	for _, included := range m.included {
		if included {
			count++
		}
	}
	return count
}
