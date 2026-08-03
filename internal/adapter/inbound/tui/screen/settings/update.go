package settings

import (
	"errors"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	core "github.com/mateuslh/lealing/internal/core/settings"
)

var errNotConfigured = errors.New("configuração indisponível nesta sessão")

func (m *Model) Update(message tea.Msg) (tui.Screen, tea.Cmd) {
	switch message := message.(type) {
	case loadedMsg:
		m.err = message.err
		if message.err == nil {
			m.values, m.info = message.values, message.info
		}
		m.clampCursor()
		return m, nil

	case savedMsg:
		if message.err != nil {
			m.err, m.message = message.err, ""
			return m, nil
		}
		m.err, m.success, m.message = nil, true, message.message
		if message.restart {
			// Marcado por campo, e não uma vez para a tela toda: o usuário
			// precisa saber qual ajuste ainda não está valendo.
			m.restarted[message.key] = true
		}
		return m, m.reload()

	case tea.KeyMsg:
		return m.handleKey(message)
	}
	return m, nil
}

func (m *Model) handleKey(key tea.KeyMsg) (tui.Screen, tea.Cmd) {
	if m.manager == nil {
		return m, nil
	}
	if m.editing {
		return m.handleEditing(key)
	}

	switch key.String() {
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)

	case "left", "h", "shift+tab":
		m.focus = zoneSections
	case "right", "l", "tab":
		if len(m.sectionEntries()) > 0 {
			m.focus, m.field = zoneFields, 0
		}

	case "enter", " ", "space":
		return m.activate()

	case "r":
		// Reset é por campo: um "restaurar tudo" apagaria de uma vez ajustes
		// que o usuário levou meses para acertar.
		if m.focus != zoneFields {
			return m, nil
		}
		value, ok := m.currentField()
		if !ok || value.Source != core.SourceUser {
			return m, nil
		}
		m.err, m.message = nil, ""
		return m, m.reset(value.Key, value.Restart)

	case "ctrl+r":
		m.err, m.message = nil, ""
		return m, m.reload()
	}
	return m, nil
}

// activate abre a edição de um texto ou alterna um interruptor.
func (m *Model) activate() (tui.Screen, tea.Cmd) {
	if m.focus == zoneSections {
		if len(m.sectionEntries()) == 0 {
			return m, nil
		}
		m.focus, m.field = zoneFields, 0
		return m, nil
	}
	current, ok := m.currentEntry()
	if !ok {
		return m, nil
	}
	if current.isAction {
		if current.action.Screen == nil {
			return m, nil
		}
		return m, tui.Navigate(current.action.Screen())
	}
	value := current.value

	if value.Kind == core.KindToggle {
		next := "true"
		if value.Bool() {
			next = "false"
		}
		m.err, m.message = nil, ""
		return m, m.save(value.Key, next, value.Restart)
	}

	m.editing = true
	m.input.SetValue(value.Current)
	m.input.Placeholder = value.Placeholder
	m.input.CursorEnd()
	m.input.Focus()
	m.err, m.message = nil, ""
	return m, textinput.Blink
}

func (m *Model) handleEditing(key tea.KeyMsg) (tui.Screen, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.editing = false
		m.input.Blur()
		return m, nil

	case "enter":
		value, ok := m.currentField()
		if !ok {
			m.editing = false
			return m, nil
		}
		m.editing = false
		m.input.Blur()
		return m, m.save(value.Key, m.input.Value(), value.Restart)
	}

	var command tea.Cmd
	m.input, command = m.input.Update(key)
	return m, command
}

func (m *Model) move(delta int) {
	if m.focus == zoneSections {
		m.section = clamp(m.section+delta, len(m.sections))
		m.field = 0
		return
	}
	m.field = clamp(m.field+delta, len(m.sectionEntries()))
}

// clampCursor recoloca os cursores depois de uma recarga que mudou as listas.
func (m *Model) clampCursor() {
	m.section = clamp(m.section, len(m.sections))
	m.field = clamp(m.field, len(m.sectionEntries()))
	if len(m.sectionEntries()) == 0 {
		m.focus = zoneSections
	}
}

func clamp(value, length int) int {
	if value < 0 || length == 0 {
		return 0
	}
	return min(value, length-1)
}
