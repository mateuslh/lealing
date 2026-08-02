package usersync

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	core "github.com/mateuslh/lealing/internal/core/usersync"
)

// errDisabled explica a ausência do recurso em vez de mostrar um painel vazio.
var errDisabled = errors.New(
	"a sincronização está desligada nesta sessão (-ephemeral não persiste preferências)")

func (m *Model) Update(message tea.Msg) (tui.Screen, tea.Cmd) {
	switch message := message.(type) {
	case statusMsg:
		m.phase = phaseReady
		m.status, m.err = message.status, message.err
		return m, nil

	case deviceMsg:
		if message.err != nil {
			m.phase, m.err = phaseReady, message.err
			return m, nil
		}
		m.phase, m.code, m.err = phaseAwaiting, message.code, nil
		m.message, m.success = "", false
		// O polling começa junto com a exibição do código: o usuário aprova
		// no browser enquanto esta tela espera.
		return m, m.waitLogin(message.code)

	case loggedMsg:
		if message.err != nil {
			m.phase, m.err = phaseReady, message.err
			return m, nil
		}
		m.phase, m.err, m.success = phaseLoading, nil, true
		m.message = "conectado como " + message.identity.Login
		return m, m.load()

	case syncMsg:
		if errors.Is(message.err, core.ErrConflict) {
			// Divergência não vira erro: vira pergunta. Sobrescrever é uma
			// escolha legítima, desde que consciente.
			m.phase = phaseReady
			m.confirm = &pending{push: message.push, remote: message.remote}
			m.err, m.message = nil, ""
			return m, nil
		}
		if message.err != nil {
			m.phase, m.err, m.message = phaseReady, message.err, ""
			return m, nil
		}
		m.phase, m.err, m.success = phaseLoading, nil, true
		m.message = describeResult(message)
		return m, m.load()

	case actionMsg:
		if message.err != nil {
			m.err, m.message = message.err, ""
			return m, nil
		}
		m.err, m.success, m.message = nil, true, message.message
		return m, m.load()

	case tea.KeyMsg:
		return m.handleKey(message)
	}
	return m, nil
}

func (m *Model) handleKey(key tea.KeyMsg) (tui.Screen, tea.Cmd) {
	if m.manager == nil {
		return m, nil
	}
	if m.confirm != nil {
		return m.handleConfirm(key)
	}
	if m.phase == phaseAwaiting {
		return m.handleAwaiting(key)
	}
	if m.phase == phaseWorking {
		// Enquanto a rede trabalha o teclado fica inerte: navegar durante uma
		// escrita remota produziria uma tela que não corresponde ao estado.
		return m, nil
	}

	switch key.String() {
	case "up", "k":
		m.cursor = max(m.cursor-1, 0)
	case "down", "j":
		m.cursor = min(m.cursor+1, len(core.AllSections)-1)

	case "r", "ctrl+r":
		m.phase, m.err, m.message = phaseLoading, nil, ""
		return m, m.load()

	case "enter":
		if !m.status.Connected {
			m.phase, m.err, m.message = phaseWorking, nil, ""
			return m, m.startLogin()
		}
		return m.toggleCurrent()

	case " ", "space":
		if m.status.Connected {
			return m.toggleCurrent()
		}

	case "s":
		if !m.status.Connected {
			return m, nil
		}
		m.phase, m.err, m.message = phaseWorking, nil, ""
		return m, m.sync(true, false)

	case "b":
		if !m.status.Connected {
			return m, nil
		}
		m.phase, m.err, m.message = phaseWorking, nil, ""
		return m, m.sync(false, false)

	case "x":
		if m.status.Connected {
			return m, m.logout()
		}
	}
	return m, nil
}

func (m *Model) toggleCurrent() (tui.Screen, tea.Cmd) {
	section, ok := m.currentSection()
	if !ok {
		return m, nil
	}
	return m, m.toggleSection(section, !m.status.Selection.Enabled(section))
}

func (m *Model) handleAwaiting(key tea.KeyMsg) (tui.Screen, tea.Cmd) {
	switch key.String() {
	case "c":
		return m, m.copyCode()
	case "o":
		return m, m.openBrowser()
	case "esc", "q":
		// Desistir só solta esta tela; o código continua válido no GitHub até
		// expirar, e dizer isso é papel da mensagem.
		m.phase = phaseReady
		m.message, m.success = "login cancelado; o código expira sozinho", false
		return m, nil
	}
	return m, nil
}

func (m *Model) handleConfirm(key tea.KeyMsg) (tui.Screen, tea.Cmd) {
	switch strings.ToLower(key.String()) {
	case "y", "s", "enter":
		confirm := m.confirm
		m.confirm, m.phase = nil, phaseWorking
		return m, m.sync(confirm.push, true)
	case "n", "esc":
		m.confirm = nil
		m.message, m.success = "nada foi sobrescrito", false
	}
	return m, nil
}

// describeResult conta o que a operação fez, em vez de um "ok" que não
// permite conferir nada.
func describeResult(message syncMsg) string {
	if message.push {
		summary := message.result.State.Summary()
		return "enviado: " + join(
			count(summary[core.SectionUsage], "tool com uso", "tools com uso"),
			count(summary[core.SectionSources], "origem", "origens"),
			count(summary[core.SectionTools], "tool instalada", "tools instaladas"),
		)
	}
	applied := message.result.Applied
	return "aplicado: " + join(
		count(applied[core.SectionUsage], "tool com uso", "tools com uso"),
		count(applied[core.SectionSources], "origem", "origens"),
	)
}

func count(value int, singular, plural string) string {
	switch value {
	case 0:
		return ""
	case 1:
		return "1 " + singular
	default:
		return fmt.Sprintf("%d %s", value, plural)
	}
}

func join(parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			kept = append(kept, part)
		}
	}
	if len(kept) == 0 {
		return "nada a sincronizar"
	}
	return strings.Join(kept, " · ")
}
