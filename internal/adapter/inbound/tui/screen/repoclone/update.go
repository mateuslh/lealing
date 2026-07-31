package repoclone

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	core "github.com/mateuslh/lealing/internal/core/repoclone"
)

// Update implementa tui.Screen.
func (m *Model) Update(msg tea.Msg) (tui.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.Width = max(msg.Width-12, 8)
		m.addInput.Width = max(msg.Width-12, 8)
		return m, nil

	case discoveredMsg:
		if msg.err != nil {
			m.phase, m.err = phaseInput, msg.err
			m.input.Focus()
			return m, nil
		}
		m.phase, m.plan, m.err = phaseConfirm, msg.plan, nil
		m.cursor = 0
		m.included = make([]bool, len(msg.plan.Repositories))
		for i := range m.included {
			m.included[i] = true
		}
		m.feedback = "família encontrada — revise a seleção antes de clonar"
		m.input.Blur()
		return m, nil

	case resolvedMsg:
		if msg.err != nil {
			m.phase, m.err = phaseAdding, msg.err
			m.addInput.Focus()
			return m, nil
		}
		for i, repo := range m.plan.Repositories {
			if strings.EqualFold(repo.Name, msg.repository.Name) {
				m.phase, m.cursor, m.included[i] = phaseConfirm, i, true
				m.feedback = "“" + repo.Name + "” já estava na lista e foi incluído"
				m.err = nil
				m.addInput.Blur()
				return m, nil
			}
		}
		m.plan.Repositories = append(m.plan.Repositories, msg.repository)
		m.included = append(m.included, true)
		m.cursor = len(m.plan.Repositories) - 1
		m.phase, m.err = phaseConfirm, nil
		m.feedback = "“" + msg.repository.Name + "” adicionado ao plano"
		m.addInput.Blur()
		return m, nil

	case clonedMsg:
		m.phase, m.result, m.err = phaseDone, msg.result, msg.err
		return m, nil

	case tea.KeyMsg:
		switch m.phase {
		case phaseInput:
			return m, m.keyInput(msg)
		case phaseConfirm:
			return m, m.keyConfirm(msg)
		case phaseAdding:
			return m, m.keyAdding(msg)
		case phaseDone:
			return m, m.keyDone(msg)
		}
	}
	return m, nil
}

func (m *Model) keyInput(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		return func() tea.Msg { return tui.Back() }
	case "enter":
		if m.input.Value() == "" {
			m.err = errEmptyInput
			return nil
		}
		m.phase, m.err = phaseDiscovering, nil
		m.input.Blur()
		return m.discover()
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.err != nil {
		m.err = nil
	}
	return cmd
}

func (m *Model) keyConfirm(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.plan.Repositories)-1 {
			m.cursor++
		}
	case " ":
		if m.cursor >= 0 && m.cursor < len(m.included) {
			m.included[m.cursor] = !m.included[m.cursor]
			state := "fora do clone"
			if m.included[m.cursor] {
				state = "incluído no clone"
			}
			m.feedback = "“" + m.plan.Repositories[m.cursor].Name + "” " + state
			m.err = nil
		}
	case "d":
		if m.cursor >= 0 && m.cursor < len(m.plan.Repositories) {
			name := m.plan.Repositories[m.cursor].Name
			m.plan.Repositories = append(m.plan.Repositories[:m.cursor], m.plan.Repositories[m.cursor+1:]...)
			m.included = append(m.included[:m.cursor], m.included[m.cursor+1:]...)
			m.cursor = min(m.cursor, max(len(m.plan.Repositories)-1, 0))
			m.feedback = "“" + name + "” removido da proposta; nada foi apagado no GitHub"
			m.err = nil
		}
	case "a":
		m.phase, m.err, m.feedback = phaseAdding, nil, ""
		m.addInput.SetValue("")
		m.addInput.Focus()
		return textinputBlink()
	case "enter", "c":
		if m.selectedCount() == 0 {
			m.err = inputError("inclua pelo menos um repositório antes de clonar")
			return nil
		}
		m.phase, m.err = phaseCloning, nil
		return m.clone()
	case "e", "esc":
		m.phase, m.err = phaseInput, nil
		m.input.Focus()
		return textinputBlink()
	}
	return nil
}

func (m *Model) keyAdding(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "esc":
		m.phase, m.err = phaseConfirm, nil
		m.addInput.Blur()
		return nil
	case "enter":
		if strings.TrimSpace(m.addInput.Value()) == "" {
			m.err = inputError("informe o nome ou a URL do repositório")
			return nil
		}
		m.phase, m.err = phaseResolving, nil
		m.addInput.Blur()
		return m.resolve()
	}

	var cmd tea.Cmd
	m.addInput, cmd = m.addInput.Update(msg)
	if m.err != nil {
		m.err = nil
	}
	return cmd
}

func (m *Model) keyDone(msg tea.KeyMsg) tea.Cmd {
	if msg.String() == "r" {
		m.phase, m.plan, m.result, m.err = phaseInput, core.Plan{}, core.Result{}, nil
		m.cursor, m.included, m.feedback = 0, nil, ""
		m.input.SetValue("")
		m.input.Focus()
		return textinputBlink()
	}
	return nil
}

var (
	errEmptyInput = inputError("informe o link do repositório")
)

type inputError string

func (e inputError) Error() string { return string(e) }

func textinputBlink() tea.Cmd { return textinput.Blink }
