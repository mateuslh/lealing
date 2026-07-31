package ccaccount

import (
	"errors"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	core "github.com/mateuslh/lealing/internal/core/ccaccount"
)

// Update implementa tui.Screen.
func (m *Model) Update(msg tea.Msg) (tui.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case stateMsg:
		m.loading = false
		if msg.err != nil {
			m.status = status{msg.err.Error(), toneErr}
			return m, nil
		}
		m.state = msg.state
		m.cursor = min(m.cursor, max(len(m.state.Profiles)-1, 0))
		return m, nil

	case actionMsg:
		return m, m.handleAction(msg)

	case tea.KeyMsg:
		switch m.mode {
		case modeNaming:
			return m, m.keyNaming(msg)
		case modeConfirm:
			return m, m.keyConfirm(msg)
		default:
			return m, m.keyList(msg)
		}
	}
	return m, nil
}

// handleAction traduz o resultado de uma ação em mensagem e recarga.
func (m *Model) handleAction(msg actionMsg) tea.Cmd {
	m.busy = false

	switch {
	case msg.err == nil:
		m.status = status{"perfil “" + msg.name + "” " + msg.verb, toneOK}
		m.loading = true
		return m.load()

	// Os dois casos abaixo não são falhas: são perguntas que só o usuário
	// pode responder, e que o núcleo se recusa a responder sozinho.
	case errors.Is(msg.err, core.ErrProfileExists):
		return m.ask(intentOverwriteProfile, msg.name,
			"“"+msg.name+"” já guarda outra conta. Substituir?")

	case errors.Is(msg.err, core.ErrActiveUnsaved):
		return m.ask(intentActivateUnsaved, msg.name,
			"a sessão ativa não está salva e será perdida. Trocar mesmo assim?")

	default:
		m.status = status{msg.err.Error(), toneErr}
		return nil
	}
}

// ask entra no modo de confirmação.
func (m *Model) ask(what intent, target, question string) tea.Cmd {
	m.mode = modeConfirm
	m.pending = what
	m.target = target
	m.question = question
	return nil
}

// reset volta ao modo de lista.
func (m *Model) reset() {
	m.mode = modeList
	m.pending = intentNone
	m.target = ""
	m.question = ""
	m.input = ""
}

// --- Teclas ------------------------------------------------------------

func (m *Model) keyList(msg tea.KeyMsg) tea.Cmd {
	if m.busy {
		return nil
	}

	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}

	case "down", "j":
		if m.cursor < len(m.state.Profiles)-1 {
			m.cursor++
		}

	case "enter":
		profile, ok := m.selected()
		if !ok {
			m.status = status{"nenhum perfil guardado ainda — use “s” para salvar a sessão atual", toneWarn}
			return nil
		}
		if profile.Name == m.state.ActiveProfile {
			m.status = status{"“" + profile.Name + "” já é a conta ativa", toneNeutral}
			return nil
		}
		m.busy = true
		return m.activate(profile.Name, false)

	case "s":
		if !m.state.HasActive {
			m.status = status{core.ErrNoActiveSession.Error(), toneWarn}
			return nil
		}
		m.mode = modeNaming
		m.input = m.suggestName()

	case "d":
		profile, ok := m.selected()
		if !ok {
			return nil
		}
		return m.ask(intentForget, profile.Name, "remover o perfil “"+profile.Name+"”?")

	case "r", "ctrl+r":
		m.loading = true
		return m.load()
	}
	return nil
}

func (m *Model) keyNaming(msg tea.KeyMsg) tea.Cmd {
	switch msg.Type {
	case tea.KeyEsc:
		m.reset()
		return nil

	case tea.KeyEnter:
		name := m.input
		if _, err := core.NormalizeName(name); err != nil {
			m.status = status{err.Error(), toneErr}
			return nil
		}
		m.reset()
		m.busy = true
		return m.save(name, false)

	case tea.KeyBackspace:
		if runes := []rune(m.input); len(runes) > 0 {
			m.input = string(runes[:len(runes)-1])
		}
		return nil

	case tea.KeySpace:
		msg.Runes = []rune{' '}
	}

	for _, r := range msg.Runes {
		// Filtrar na entrada, e não só na confirmação, é o que impede o
		// usuário digitar um nome inteiro para descobrir no fim que uma
		// barra o invalidou.
		if core.AllowedInName(r) && len([]rune(m.input)) < core.NameLimit {
			m.input += string(r)
		}
	}
	return nil
}

func (m *Model) keyConfirm(msg tea.KeyMsg) tea.Cmd {
	switch msg.String() {
	case "s", "y", "enter":
		what, target := m.pending, m.target
		m.reset()
		m.busy = true
		switch what {
		case intentForget:
			return m.forget(target)
		case intentOverwriteProfile:
			return m.save(target, true)
		case intentActivateUnsaved:
			return m.activate(target, true)
		}
		m.busy = false
		return nil

	case "n", "esc":
		m.reset()
		m.status = status{"cancelado", toneNeutral}
	}
	return nil
}

// Hints implementa tui.Screen.
func (m *Model) Hints() []tui.Hint {
	switch m.mode {
	case modeNaming:
		return []tui.Hint{
			{Key: "↵", Label: "salvar"},
			{Key: "esc", Label: "cancelar"},
		}
	case modeConfirm:
		return []tui.Hint{
			{Key: "s", Label: "confirmar"},
			{Key: "n/esc", Label: "cancelar"},
		}
	default:
		return []tui.Hint{
			{Key: "↵", Label: "ativar"},
			{Key: "s", Label: "salvar atual"},
			{Key: "d", Label: "remover"},
			{Key: "r", Label: "atualizar"},
			{Key: "esc", Label: "voltar"},
		}
	}
}

// Meta implementa o contrato opcional do App: o número de perfis guardados.
func (m *Model) Meta() []string {
	if len(m.state.Profiles) == 0 {
		return nil
	}
	return []string{strconv.Itoa(len(m.state.Profiles)) + " perfis"}
}
