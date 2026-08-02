package usersync

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/component"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	core "github.com/mateuslh/lealing/internal/core/usersync"
)

const (
	splitMinWidth  = 84
	splitMinHeight = 14
)

func (m *Model) View(frame tui.Frame) string {
	th := m.deps.Theme
	if frame.Width < 24 || frame.Height < 6 {
		return th.Ghost.Render("janela pequena demais")
	}

	inner := max(frame.Width-4, 20)
	height := max(frame.Height-2, 4)

	body := m.viewBody(th, inner, height)
	content := lipgloss.NewStyle().
		Padding(1, 2).MaxWidth(frame.Width).MaxHeight(frame.Height).
		Render(body)

	if m.confirm != nil {
		return component.Overlay(content, m.viewConfirm(th, min(inner, 64)), frame.Width, frame.Height)
	}
	return content
}

func (m *Model) viewBody(th *theme.Theme, width, height int) string {
	switch {
	case m.manager == nil:
		return component.Center(width, height,
			lipgloss.NewStyle().Foreground(th.Warning).Render("sincronização desligada"),
			"",
			th.Ghost.Render(component.TruncateTail(errDisabled.Error(), width)))

	case m.phase == phaseLoading:
		return component.Center(width, height, th.Dim.Render("consultando a conta e o estado remoto…"))

	case m.phase == phaseAwaiting:
		return m.viewDeviceCode(th, width, height)

	case !m.status.Connected:
		return m.viewDisconnected(th, width, height)
	}
	return m.viewConnected(th, width, height)
}

// --- Desconectado ------------------------------------------------------

func (m *Model) viewDisconnected(th *theme.Theme, width, height int) string {
	lines := []string{
		th.Strong.Render("Conecte sua conta do GitHub"),
		"",
		wrap(th.Body, "Suas preferências passam a viver em um repositório privado da sua conta. "+
			"Uma máquina nova entra na conta e chega configurada.", width-4, 3),
		"",
		th.Ghost.Render("o lealing cria " + core.DefaultRepository + " privado na sua conta"),
		th.Ghost.Render("nada é enviado antes de você escolher o que sincronizar"),
		"",
		lipgloss.NewStyle().Foreground(th.Primary).Bold(true).Render("↵ entrar com o GitHub"),
	}
	if message := m.viewFeedback(th, width-4); message != "" {
		lines = append(lines, "", message)
	}
	content := strings.Join(lines, "\n")
	return component.Panel{
		Title: "conta", Glyph: "☁", Accent: th.Primary, Focused: true,
		Width: width, Height: min(lipgloss.Height(content)+2, height),
	}.Render(th, content)
}

// viewDeviceCode dá ao código o destaque que ele precisa: é o único dado que
// o usuário vai copiar para outro dispositivo.
func (m *Model) viewDeviceCode(th *theme.Theme, width, height int) string {
	code := lipgloss.NewStyle().
		Foreground(th.OnPrimary).Background(th.Primary).Bold(true).Padding(0, 2).
		Render(m.code.UserCode)

	remaining := ""
	if !m.code.ExpiresAt.IsZero() {
		if left := time.Until(m.code.ExpiresAt).Round(time.Minute); left > 0 {
			remaining = fmt.Sprintf("o código vale por cerca de %d min", int(left.Minutes()))
		}
	}

	lines := []string{
		th.Strong.Render("Autorize no GitHub"),
		"",
		th.Body.Render("1. abra " + m.code.VerificationURL),
		th.Body.Render("2. informe o código:"),
		"",
		"   " + code,
		"",
		th.Ghost.Render(remaining),
		th.Dim.Render("esperando a aprovação…"),
		"",
		th.Ghost.Render("c copiar código · o abrir browser · esc cancelar"),
	}
	if message := m.viewFeedback(th, width-4); message != "" {
		lines = append(lines, "", message)
	}
	content := strings.Join(lines, "\n")
	return component.Panel{
		Title: "autorização", Glyph: "☁", Accent: th.Accent, Focused: true,
		Width: width, Height: min(lipgloss.Height(content)+2, height),
	}.Render(th, content)
}

// --- Conectado ---------------------------------------------------------

func (m *Model) viewConnected(th *theme.Theme, width, height int) string {
	if width < splitMinWidth || height < splitMinHeight {
		return m.viewAccount(th, width, height)
	}
	left := max(width/2, 34)
	return lipgloss.JoinHorizontal(lipgloss.Top,
		m.viewAccount(th, left, height),
		" ",
		m.viewSections(th, width-left-1, height),
	)
}

func (m *Model) viewAccount(th *theme.Theme, width, height int) string {
	inner := width - 2
	identity := m.status.Identity.Login
	if name := m.status.Identity.Name; name != "" {
		identity += " · " + name
	}

	rows := []component.Row{
		{Label: "conta", Value: identity},
		{Label: "repositório", Value: m.status.Repository, Hint: "privado"},
		{Label: "esta máquina", Value: summarize(m.status.Local)},
	}
	switch {
	case m.status.RemoteErr != nil:
		rows = append(rows, component.Row{
			Label: "remoto", Value: firstLine(m.status.RemoteErr.Error()), Tone: th.Danger})
	case m.status.RemoteMissing:
		rows = append(rows, component.Row{
			Label: "remoto", Value: "ainda nada publicado", Tone: th.Faint})
	default:
		rows = append(rows, component.Row{Label: "remoto", Value: summarize(m.status.Remote)})
		if device := m.status.Remote.Device; device != "" {
			rows = append(rows, component.Row{
				Label: "enviado por", Value: device + " " + since(m.now(), m.status.Remote.UpdatedAt)})
		}
	}
	if !m.status.LastSync.IsZero() {
		rows = append(rows, component.Row{
			Label: "última sync", Value: since(m.now(), m.status.LastSync), Tone: th.Faint})
	}

	lines := []string{component.FieldList{Rows: rows, Width: inner, LabelWidth: 13}.Render(th)}
	if m.status.Diverged {
		lines = append(lines, "", wrap(lipgloss.NewStyle().Foreground(th.Warning),
			"▲ o remoto mudou desde a última sincronização desta máquina; enviar vai sobrescrever.",
			inner, 2))
	}
	if m.phase == phaseWorking {
		lines = append(lines, "", lipgloss.NewStyle().Foreground(th.Accent).Render("◐ falando com o GitHub…"))
	} else if message := m.viewFeedback(th, inner); message != "" {
		lines = append(lines, "", message)
	}

	return component.Panel{
		Title: "conta", Glyph: "☁", Accent: th.Primary,
		Footer: "s enviar · b baixar · x sair", Width: width, Height: height,
	}.Render(th, strings.Join(lines, "\n"))
}

func (m *Model) viewSections(th *theme.Theme, width, height int) string {
	inner := width - 2
	localSummary := m.status.Local.Summary()
	remoteSummary := m.status.Remote.Summary()

	lines := make([]string, 0, len(core.AllSections)*2+2)
	lines = append(lines,
		component.Spread(th.Ghost.Render("o que sincroniza"),
			th.Ghost.Render("aqui → lá"), inner), "")

	for index, section := range core.AllSections {
		selected := index == m.cursor
		caret := "  "
		label := th.Item.Render(section.Label())
		if selected {
			caret = lipgloss.NewStyle().Foreground(th.Primary).Render("▎") + " "
			label = th.ItemSelected.Render(section.Label())
		}
		enabled := m.status.Selection.Enabled(section)
		mark, tone := "○", th.Faint
		if enabled {
			mark, tone = "●", th.Success
		}
		counts := th.Counter.Render(fmt.Sprintf("%d → %d",
			localSummary[section], remoteSummary[section]))

		lines = append(lines, component.TruncateTail(component.Spread(
			caret+lipgloss.NewStyle().Foreground(tone).Render(mark)+" "+label, counts, inner), inner))
		if selected {
			lines = append(lines, th.Ghost.Render(
				component.TruncateTail("    "+sectionHint(section), inner)))
		}
	}

	return component.Panel{
		Title: "seções", Glyph: "◆", Accent: th.Secondary, Focused: true,
		Footer: "espaço liga/desliga", Width: width, Height: height,
	}.Render(th, strings.Join(lines, "\n"))
}

// sectionHint explica o efeito de cada seção onde ele importa: na hora de
// decidir se ela deve sair da máquina.
func sectionHint(section core.Section) string {
	switch section {
	case core.SectionUsage:
		return "favoritas, contagem e recência das tools"
	case core.SectionSources:
		return "os repositórios de tools cadastrados"
	case core.SectionTools:
		return "a lista do que está instalado — instalar continua manual"
	default:
		return ""
	}
}

func (m *Model) viewConfirm(th *theme.Theme, width int) string {
	inner := width - 2
	action, detail := "Enviar por cima do estado remoto?", "O que está publicado será substituído pelo desta máquina."
	if !m.confirm.push {
		action, detail = "Baixar por cima do estado local?",
			"As preferências desta máquina serão substituídas pelas do repositório."
	}
	lines := []string{
		th.Strong.Render(component.TruncateTail(action, inner)),
		"",
		wrap(th.Dim, detail, inner, 3),
	}
	if device := m.confirm.remote.Device; device != "" {
		lines = append(lines, "", th.Ghost.Render(component.TruncateTail(
			"remoto enviado por "+device+" "+since(m.now(), m.confirm.remote.UpdatedAt), inner)))
	}
	content := strings.Join(lines, "\n")
	return component.Panel{
		Title: "confirmar", Glyph: "!", Accent: th.Warning, Focused: true,
		Footer: "y sobrescrever · n cancelar", Width: width, Height: lipgloss.Height(content) + 2,
	}.Render(th, content)
}

func (m *Model) viewFeedback(th *theme.Theme, width int) string {
	switch {
	case m.err != nil:
		return wrap(lipgloss.NewStyle().Foreground(th.Danger), "✗ "+firstLine(m.err.Error()), width, 3)
	case m.message != "" && m.success:
		return wrap(lipgloss.NewStyle().Foreground(th.Success), "✓ "+m.message, width, 2)
	case m.message != "":
		return wrap(lipgloss.NewStyle().Foreground(th.Warning), "▲ "+m.message, width, 2)
	}
	return ""
}

func (m *Model) Hints() []tui.Hint {
	switch {
	case m.manager == nil:
		return []tui.Hint{{Key: "esc", Label: "voltar"}}
	case m.confirm != nil:
		return []tui.Hint{{Key: "y", Label: "sobrescrever"}, {Key: "n", Label: "cancelar"}}
	case m.phase == phaseAwaiting:
		return []tui.Hint{
			{Key: "c", Label: "copiar código"}, {Key: "o", Label: "abrir browser"},
			{Key: "esc", Label: "cancelar"},
		}
	case !m.status.Connected:
		return []tui.Hint{{Key: "↵", Label: "entrar com o GitHub"}, {Key: "esc", Label: "voltar"}}
	}
	return []tui.Hint{
		{Key: "s", Label: "enviar"}, {Key: "b", Label: "baixar"},
		{Key: "espaço", Label: "ligar seção"}, {Key: "↑↓", Label: "seção"},
		{Key: "x", Label: "desconectar"}, {Key: "r", Label: "recarregar"},
		{Key: "esc", Label: "voltar"},
	}
}

func (m *Model) Meta() []string {
	if !m.status.Connected {
		return nil
	}
	return []string{m.status.Identity.Login, m.status.Repository}
}

func (m *Model) Status() (string, lipgloss.TerminalColor) {
	th := m.deps.Theme
	switch {
	case m.manager == nil:
		return "sincronização desligada", th.Warning
	case m.phase == phaseWorking:
		return "sincronizando", th.Accent
	case m.phase == phaseAwaiting:
		return "esperando autorização no GitHub", th.Secondary
	case m.err != nil:
		return "falha na sincronização", th.Danger
	case !m.status.Connected:
		return "nenhuma conta conectada", th.Muted
	case m.status.Diverged:
		return "o remoto mudou desde a última sincronização", th.Warning
	default:
		return "conectado como " + m.status.Identity.Login, th.Success
	}
}

var (
	_ interface {
		Status() (string, lipgloss.TerminalColor)
	} = (*Model)(nil)
	_ interface{ Meta() []string }   = (*Model)(nil)
	_ interface{ Refresh() tea.Cmd } = (*Model)(nil)
)

// --- Auxiliares --------------------------------------------------------

func summarize(state core.State) string {
	summary := state.Summary()
	parts := make([]string, 0, 3)
	for _, section := range core.AllSections {
		if value := summary[section]; value > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", value, shortLabel(section)))
		}
	}
	if len(parts) == 0 {
		return "nada guardado"
	}
	return strings.Join(parts, " · ")
}

func shortLabel(section core.Section) string {
	switch section {
	case core.SectionUsage:
		return "usos"
	case core.SectionSources:
		return "origens"
	case core.SectionTools:
		return "tools"
	default:
		return string(section)
	}
}

// since escreve a distância no tempo em palavras. "há 3 min" responde à
// pergunta que um carimbo ISO não responde sem contas.
func since(now, moment time.Time) string {
	if moment.IsZero() {
		return "nunca"
	}
	elapsed := now.Sub(moment)
	switch {
	case elapsed < time.Minute:
		return "agora"
	case elapsed < time.Hour:
		return fmt.Sprintf("há %d min", int(elapsed.Minutes()))
	case elapsed < 24*time.Hour:
		return fmt.Sprintf("há %dh", int(elapsed.Hours()))
	default:
		return fmt.Sprintf("há %d dias", int(elapsed.Hours()/24))
	}
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	return line
}

// wrap quebra o texto em até maxLines linhas, cortando a última com
// reticências quando ainda sobra conteúdo.
func wrap(style lipgloss.Style, text string, width, maxLines int) string {
	if width <= 0 || maxLines <= 0 {
		return ""
	}
	var lines []string
	current := ""
	for _, word := range strings.Fields(text) {
		candidate := word
		if current != "" {
			candidate = current + " " + word
		}
		if lipgloss.Width(candidate) > width && current != "" {
			lines = append(lines, current)
			current = word
			continue
		}
		current = candidate
	}
	if current != "" {
		lines = append(lines, current)
	}
	if len(lines) == 0 {
		return ""
	}
	if len(lines) > maxLines {
		lines = append(lines[:maxLines-1], strings.Join(lines[maxLines-1:], " "))
	}
	for index, line := range lines {
		lines[index] = component.TruncateTail(line, width)
	}
	return style.Render(strings.Join(lines, "\n"))
}
