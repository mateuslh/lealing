package accountsync

import (
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/core/toolinstall"
	"github.com/mateuslh/lealing/internal/core/usersync"
)

var errNotConfigured = errors.New("sincronização indisponível nesta sessão")
var errHostActionsUnavailable = errors.New("integrações do navegador e clipboard indisponíveis")

func (m *Model) Update(message tea.Msg) (tui.Screen, tea.Cmd) {
	switch message := message.(type) {
	case statusMsg:
		m.cancelIO = nil
		m.busy = ""
		m.err = message.err
		if message.err == nil {
			m.status, m.loaded = message.status, true
			m.cursor = clamp(m.cursor, m.rowCount())
		}
		return m, nil

	case loginCodeMsg:
		if message.attempt != m.loginAttempt {
			return m, nil
		}
		m.cancelIO = nil
		if message.err != nil {
			m.busy, m.err = "", message.err
			return m, nil
		}
		m.device = &message.code
		m.clipboardCopied = false
		m.deviceFeedback, m.deviceActionErr = "copiando o código…", nil
		m.busy = "aguardando sua aprovação no GitHub…"
		return m, tea.Batch(
			m.copyDeviceCode(message.code.UserCode, message.attempt),
			m.awaitLogin(message.code, message.attempt),
		)

	case deviceActionMsg:
		if message.attempt != m.loginAttempt || m.device == nil {
			return m, nil
		}
		if message.err != nil {
			m.deviceFeedback, m.deviceActionErr = "", message.err
			return m, nil
		}
		m.deviceActionErr = nil
		if message.action == deviceActionOpen {
			if m.clipboardCopied {
				m.deviceFeedback = "navegador aberto; o código já está no clipboard"
			} else {
				m.deviceFeedback = "navegador aberto; copie o código exibido"
			}
		} else {
			m.clipboardCopied = true
			m.deviceFeedback = "código copiado para o clipboard"
		}
		return m, nil

	case loginFinishedMsg:
		if message.attempt != m.loginAttempt {
			return m, nil
		}
		m.cancelIO, m.device, m.busy = nil, nil, ""
		m.deviceFeedback, m.deviceActionErr = "", nil
		m.clipboardCopied = false
		if message.err != nil {
			m.err = message.err
			return m, nil
		}
		m.err = nil
		m.message = "conta @" + message.identity.Login + " conectada"
		m.busy = "carregando preferências…"
		return m, m.loadStatus()

	case sectionMsg:
		m.cancelIO = nil
		m.busy = ""
		if message.err != nil {
			m.err = message.err
			return m, nil
		}
		m.err = nil
		state := "desligada"
		if message.enabled {
			state = "ligada"
		}
		m.message = message.section.Label() + " " + state
		m.status.Selection[message.section] = message.enabled
		return m, nil

	case syncMsg:
		m.cancelIO = nil
		m.busy = ""
		if errors.Is(message.err, usersync.ErrConflict) {
			m.err = nil
			m.confirmation = conflictConfirmation(message.operation)
			return m, nil
		}
		var escalation *toolinstall.PermissionEscalationError
		if errors.As(message.err, &escalation) {
			m.err = nil
			m.confirmation = permissionEscalationConfirmation(message.operation, escalation)
			return m, nil
		}
		if message.err != nil {
			m.err = message.err
			return m, nil
		}
		m.err = nil
		if message.operation == operationPush {
			summary := message.result.State.Summary()
			m.message = fmt.Sprintf("enviado: %d usos, %d origens e %d tools",
				summary[usersync.SectionUsage], summary[usersync.SectionSources], summary[usersync.SectionTools])
		} else {
			m.message = fmt.Sprintf("aplicado: %d usos, %d origens e %d tools",
				message.result.Applied[usersync.SectionUsage], message.result.Applied[usersync.SectionSources],
				message.result.Applied[usersync.SectionTools])
		}
		m.busy = "conferindo o resultado…"
		return m, m.loadStatus()

	case logoutMsg:
		m.cancelIO = nil
		m.busy = ""
		if message.err != nil {
			m.err = message.err
			return m, nil
		}
		m.err = nil
		m.message = "conta desconectada desta máquina; o acesso ainda pode ser revogado no GitHub"
		m.busy = "atualizando conta…"
		return m, m.loadStatus()

	case tea.KeyMsg:
		return m.handleKey(message)
	}
	return m, nil
}

func (m *Model) handleKey(key tea.KeyMsg) (tui.Screen, tea.Cmd) {
	if m.confirmation != nil {
		return m.handleConfirmation(key)
	}
	if m.device != nil {
		switch key.String() {
		case "esc":
			m.cancelLoginFlow()
		case "c":
			m.deviceFeedback, m.deviceActionErr = "copiando o código…", nil
			return m, m.copyDeviceCode(m.device.UserCode, m.loginAttempt)
		case "o":
			m.deviceFeedback, m.deviceActionErr = "abrindo o navegador…", nil
			return m, m.openLoginURL(m.device.VerificationURL, m.loginAttempt)
		}
		return m, nil
	}
	if m.busy != "" || m.manager == nil {
		return m, nil
	}

	switch key.String() {
	case "up", "k":
		m.cursor = clamp(m.cursor-1, m.rowCount())
	case "down", "j":
		m.cursor = clamp(m.cursor+1, m.rowCount())
	case "right", "l":
		if m.cursor < len(usersync.AllSections) {
			m.cursor = clamp(m.cursor+len(usersync.AllSections), m.rowCount())
		}
	case "left", "h":
		if m.cursor >= len(usersync.AllSections) {
			m.cursor = clamp(m.cursor-len(usersync.AllSections), m.rowCount())
		}
	case "enter", " ", "space":
		return m.activate()
	case "ctrl+r", "r":
		m.err, m.message = nil, ""
		m.busy = "atualizando sincronização…"
		return m, m.loadStatus()
	}
	return m, nil
}

func (m *Model) activate() (tui.Screen, tea.Cmd) {
	m.err, m.message = nil, ""
	if !m.status.Connected {
		m.busy = "pedindo um código ao GitHub…"
		return m, m.beginLogin()
	}

	if m.cursor < len(usersync.AllSections) {
		section := usersync.AllSections[m.cursor]
		enabled := !m.status.Selection.Enabled(section)
		m.busy = "salvando seleção…"
		return m, m.setSection(section, enabled)
	}
	switch m.cursor - len(usersync.AllSections) {
	case 0:
		if m.status.Selection.Empty() {
			m.err = errors.New("ligue pelo menos uma seção antes de enviar")
			return m, nil
		}
		m.confirmation = normalConfirmation(operationPush, m.status)
	case 1:
		if m.status.Selection.Empty() {
			m.err = errors.New("ligue pelo menos uma seção antes de baixar")
			return m, nil
		}
		if m.status.RemoteMissing {
			m.err = usersync.ErrNoRemote
			return m, nil
		}
		m.confirmation = normalConfirmation(operationPull, m.status)
	case 2:
		m.confirmation = normalConfirmation(operationLogout, m.status)
	}
	return m, nil
}

func (m *Model) handleConfirmation(key tea.KeyMsg) (tui.Screen, tea.Cmd) {
	switch key.String() {
	case "esc", "n":
		m.confirmation = nil
		return m, nil
	case "enter", "y", "s":
		selected := *m.confirmation
		m.confirmation = nil
		m.err, m.message = nil, ""
		switch selected.operation {
		case operationPush:
			m.busy = "enviando preferências…"
			return m, m.synchronize(operationPush, selected.force, false)
		case operationPull:
			m.busy = "baixando e aplicando preferências…"
			return m, m.synchronize(operationPull, selected.force, selected.acceptPermissions)
		case operationLogout:
			m.busy = "removendo a credencial desta máquina…"
			return m, m.logout()
		}
	}
	return m, nil
}

func (m *Model) cancelLoginFlow() {
	if m.cancelIO != nil {
		m.cancelIO()
	}
	m.cancelIO = nil
	m.loginAttempt++
	m.device, m.busy = nil, ""
	m.deviceFeedback, m.deviceActionErr = "", nil
	m.clipboardCopied = false
	m.message = "login cancelado"
}

func normalConfirmation(selected operation, status usersync.Status) *confirmation {
	switch selected {
	case operationPush:
		message := "Os dados desta máquina substituirão as seções habilitadas no repositório privado."
		if status.Diverged || (!status.RemoteMissing && status.LastSync.IsZero()) {
			message += " O GitHub contém um estado que esta máquina ainda não sincronizou."
		}
		return &confirmation{operation: selected, title: "Enviar para o GitHub?", message: message}
	case operationPull:
		message := "As seções habilitadas serão reproduzidas nesta máquina. Tools serão instaladas, atualizadas ou removidas até corresponder exatamente ao estado remoto."
		return &confirmation{operation: selected, title: "Aplicar o estado remoto?", message: message}
	default:
		return &confirmation{operation: selected, title: "Desconectar esta conta?", message: "A credencial será removida desta máquina. O repositório e a autorização do OAuth App permanecerão no GitHub."}
	}
}

// permissionEscalationConfirmation é o segundo diálogo do Pull declarativo:
// aplicar o estado remoto pode atualizar tools, e uma delas pode pedir
// permissão que a versão ativa não tinha. Nada muda em disco até o usuário
// ver esta lista e confirmar de novo.
func permissionEscalationConfirmation(selected operation, escalation *toolinstall.PermissionEscalationError) *confirmation {
	var parts []string
	for _, item := range escalation.Escalations {
		parts = append(parts, item.ID+" pede "+addedPermissionsSummary(item.Added))
	}
	message := "O estado remoto amplia o que estas tools podem fazer: " + strings.Join(parts, "; ") +
		". A versão ativa de cada uma continua no lugar até você aprovar."
	return &confirmation{
		operation: selected, acceptPermissions: true,
		title: "Conceder novas permissões?", message: message,
	}
}

func addedPermissionsSummary(added toolinstall.ToolPermissions) string {
	var parts []string
	if total := len(added.ReadPaths) + len(added.WritePaths); total > 0 {
		parts = append(parts, fmt.Sprintf("%d caminho(s) de arquivo", total))
	}
	if added.Network {
		parts = append(parts, "acesso à rede")
	}
	if added.Subprocess {
		parts = append(parts, "subprocessos")
	}
	if added.WorkingDir != "" {
		parts = append(parts, "diretório de trabalho ("+added.WorkingDir+")")
	}
	if len(parts) == 0 {
		return "novas permissões"
	}
	return strings.Join(parts, ", ")
}

func conflictConfirmation(selected operation) *confirmation {
	message := "Há mudanças dos dois lados. Continuar substituirá dados sem fazer uma mesclagem automática."
	if selected == operationPush {
		message += " O estado remoto será substituído pelo desta máquina."
	} else {
		message += " O estado desta máquina será substituído pelo remoto."
	}
	return &confirmation{operation: selected, force: true, title: "Resolver conflito sobrescrevendo?", message: message}
}
