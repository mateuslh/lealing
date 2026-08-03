package update

import (
	"errors"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
)

var errNotConfigured = errors.New("atualização indisponível nesta sessão")

func (m *Model) Update(msg tea.Msg) (tui.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case checkedMsg:
		m.phase = phaseReady
		m.status, m.err = msg.status, msg.err
		return m, nil

	case appliedMsg:
		m.phase = phaseDone
		m.outcome, m.err = msg.outcome, msg.err
		if msg.err == nil && msg.outcome.Restart {
			return m, tui.Exit(fmt.Sprintf(
				"lealing atualizado para %s. Você já pode abrir novamente para usar a versão nova.",
				orDash(msg.outcome.To),
			))
		}
		return m, nil

	case tea.KeyMsg:
		// Durante a troca do binário não há tecla que ajude: aceitar uma
		// segunda atualização por cima da primeira é a receita para um
		// executável pela metade.
		if m.phase == phaseApplying {
			return m, nil
		}
		switch msg.String() {
		case "r", "ctrl+r":
			m.phase, m.err = phaseChecking, nil
			return m, m.check()
		case "u", "enter":
			if !m.status.CanApply() {
				return m, nil
			}
			m.phase, m.err = phaseApplying, nil
			return m, m.apply()
		}
	}
	return m, nil
}
