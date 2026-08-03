package accountsync

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/component"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	"github.com/mateuslh/lealing/internal/core/usersync"
)

const (
	wideMinWidth  = 96
	wideMinHeight = 34
)

func (m *Model) View(frame tui.Frame) string {
	th := m.deps.Theme
	if frame.Width < 24 || frame.Height < 6 {
		return th.Ghost.Render("janela pequena demais")
	}

	width := max(frame.Width-4, 20)
	height := max(frame.Height-2, 4)
	wide := frame.Width >= wideMinWidth && frame.Height >= wideMinHeight &&
		m.loaded && m.confirmation == nil && m.device == nil

	var body string
	if wide {
		if m.status.Connected {
			body = m.viewConnectedWide(th, width, height)
		} else {
			body = m.viewDisconnectedWide(th, width, height)
		}
	} else {
		content := m.viewCompactContent(th, width-2, height-2)
		body = component.Panel{
			Title: "sincronização", Glyph: "☁", Accent: th.Primary,
			Focused: true, Footer: m.footer(), Width: width, Height: height,
		}.Render(th, content)
	}

	return lipgloss.NewStyle().
		Padding(1, 2).MaxWidth(frame.Width).MaxHeight(frame.Height).
		Render(body)
}

func (m *Model) viewCompactContent(th *theme.Theme, width, height int) string {
	switch {
	case m.confirmation != nil:
		return m.viewConfirmation(th, width, height)
	case m.device != nil:
		return m.viewDeviceFlow(th, width, height)
	case !m.loaded && m.busy != "":
		return component.Center(width, height, th.Dim.Render(m.busy))
	case !m.loaded && m.err != nil:
		return component.Center(width, height,
			lipgloss.NewStyle().Foreground(th.Danger).Render("✗ "+firstLine(m.err.Error())))
	case !m.status.Connected:
		return m.viewDisconnectedCompact(th, width)
	default:
		return m.viewConnectedCompact(th, width, height)
	}
}

func (m *Model) viewConnectedWide(th *theme.Theme, width, height int) string {
	overviewHeight, controlsHeight := 8, 11
	snapshotsHeight := min(max(height-overviewHeight-controlsHeight-2, 9), 25)

	overview := m.viewAccountOverview(th, width, overviewHeight)
	leftWidth := (width - 1) / 2
	rightWidth := width - leftWidth - 1
	local, remote := m.status.Local, m.status.Remote
	scale := maxStateCount(local, remote)
	snapshots := lipgloss.JoinHorizontal(lipgloss.Top,
		m.viewStatePanel(th, "esta máquina", "⌂", th.Secondary,
			local, scale, leftWidth, snapshotsHeight, false),
		" ",
		m.viewStatePanel(th, "GitHub", "☁", remoteTone(th, m.status),
			remote, scale, rightWidth, snapshotsHeight, true),
	)
	controls := lipgloss.JoinHorizontal(lipgloss.Top,
		m.viewScopePanel(th, leftWidth, controlsHeight),
		" ",
		m.viewActionsPanel(th, rightWidth, controlsHeight),
	)
	return lipgloss.JoinVertical(lipgloss.Left, overview, "", snapshots, "", controls)
}

func (m *Model) viewAccountOverview(th *theme.Theme, width, height int) string {
	inner := width - 2
	identity := "@" + m.status.Identity.Login
	if m.status.Identity.Name != "" {
		identity += " · " + m.status.Identity.Name
	}
	tone := th.Success
	badge := "● CONECTADA"
	if m.status.RemoteErr != nil {
		tone, badge = th.Danger, "✗ REMOTO INDISPONÍVEL"
	} else if m.status.Diverged {
		tone, badge = th.Warning, "▲ ALTERAÇÕES REMOTAS"
	}
	head := component.Spread(
		th.Strong.Render(component.TruncateTail(identity, inner/2)),
		lipgloss.NewStyle().Foreground(tone).Bold(true).Render(badge), inner)
	escope := enabledLabels(m.status.Selection)
	rows := component.FieldList{Rows: []component.Row{
		{Label: "repositório privado", Value: m.status.Identity.Login + "/" + m.status.Repository},
		{Label: "última sincronização", Value: formatTime(m.status.LastSync)},
		{Label: "escopo", Value: escope},
	}, Width: inner, LabelWidth: 22}.Render(th)
	note := "As credenciais ficam no cofre do sistema; binários de tools nunca são enviados."
	if m.status.RemoteErr != nil {
		note = "A leitura remota falhou: " + firstLine(m.status.RemoteErr.Error())
	} else if m.status.Diverged {
		note = "O GitHub mudou desde a última sincronização desta máquina; revise os dois lados antes de sobrescrever."
	}
	content := strings.Join([]string{head, "", rows, toneText(th, note, tone)}, "\n")
	return component.Panel{
		Title: "conta GitHub", Glyph: "●", Accent: tone, Width: width, Height: height,
	}.Render(th, content)
}

func (m *Model) viewStatePanel(
	th *theme.Theme,
	title, glyph string,
	accent lipgloss.TerminalColor,
	state usersync.State,
	scale, width, height int,
	remote bool,
) string {
	inner := width - 2
	if remote && m.status.RemoteErr != nil {
		content := component.Center(inner, height-2,
			lipgloss.NewStyle().Foreground(th.Danger).Render("não foi possível consultar o GitHub"),
			th.Ghost.Render(component.TruncateTail(firstLine(m.status.RemoteErr.Error()), inner)))
		return component.Panel{Title: title, Glyph: glyph, Accent: accent, Width: width, Height: height}.
			Render(th, content)
	}
	if remote && m.status.RemoteMissing {
		content := component.Center(inner, height-2,
			th.Strong.Render("nenhum estado publicado"),
			th.Ghost.Render("Use “Enviar esta máquina” para criar o primeiro snapshot."))
		return component.Panel{Title: title, Glyph: glyph, Accent: accent, Width: width, Height: height}.
			Render(th, content)
	}

	content := []string{
		sectionHeader(th, "volume sincronizável", inner),
		stateChart(th, state, scale, inner),
		"",
		sectionHeader(th, "detalhes", inner),
	}
	rows := stateDetailRows(state)
	if remote {
		publisher := state.Device
		if publisher == "" {
			publisher = "não informado"
		}
		rows = append([]component.Row{
			{Label: "publicado por", Value: publisher},
			{Label: "atualizado", Value: formatTime(state.UpdatedAt)},
			{Label: "versão da engine", Value: valueOr(state.Engine, "não informada")},
		}, rows...)
	} else {
		rows = append(rows,
			component.Row{Label: "seções ativas", Value: fmt.Sprintf("%d de %d", enabledCount(m.status.Selection), len(usersync.AllSections))},
			component.Row{Label: "coletado", Value: "ao abrir e após cada operação"},
		)
	}
	content = append(content, component.FieldList{Rows: rows, Width: inner, LabelWidth: 18}.Render(th))
	if previews := statePreviews(th, state, inner); len(previews) > 0 {
		content = append(content, "", sectionHeader(th, "amostra", inner))
		content = append(content, previews...)
	}
	return component.Panel{
		Title: title, Glyph: glyph, Accent: accent, Width: width, Height: height,
	}.Render(th, strings.Join(content, "\n"))
}

func (m *Model) viewScopePanel(th *theme.Theme, width, height int) string {
	inner := width - 2
	rows := m.scopeRows(th, inner)
	description := "Enter alterna se a seção pode sair desta máquina e ser aplicada num download."
	content := strings.Join([]string{
		component.FieldList{
			Rows: rows, Width: inner, Selected: m.cursor,
			Focusable: true, Focused: m.cursor < len(usersync.AllSections) && m.busy == "",
			LabelWidth: 22,
		}.Render(th),
		"",
		wrap(description, inner, 2),
	}, "\n")
	return component.Panel{
		Title: "o que sincroniza", Glyph: "✓", Accent: th.Secondary,
		Focused: m.cursor < len(usersync.AllSections),
		Footer:  "↵ ligar/desligar", Width: width, Height: height,
	}.Render(th, content)
}

func (m *Model) viewActionsPanel(th *theme.Theme, width, height int) string {
	inner := width - 2
	selected := m.cursor - len(usersync.AllSections)
	description := "Envie, baixe ou desconecte sempre com uma revisão explícita antes de alterar dados."
	if selected >= 0 {
		description = m.selectedDescription()
	}
	content := strings.Join([]string{
		component.FieldList{
			Rows: m.actionRows(th), Width: inner, Selected: selected,
			Focusable: true, Focused: selected >= 0 && m.busy == "", LabelWidth: 22,
		}.Render(th),
		"",
		wrap(description, inner, 2),
	}, "\n")
	if feedback := feedback(th, inner, m.busy, m.message, m.err); feedback != "" {
		content += "\n" + feedback
	}
	return component.Panel{
		Title: "operações", Glyph: "⇄", Accent: th.Accent,
		Focused: selected >= 0,
		Footer:  "↵ revisar e executar", Width: width, Height: height,
	}.Render(th, content)
}

func (m *Model) viewDisconnectedWide(th *theme.Theme, width, height int) string {
	headerHeight := 8
	contentHeight := min(max(height-headerHeight-1, 10), 25)
	inner := width - 2
	headerContent := strings.Join([]string{
		component.Spread(th.Strong.Render("Nenhuma conta conectada"),
			lipgloss.NewStyle().Foreground(th.Faint).Bold(true).Render("○ DESLIGADA"), inner),
		"",
		"A sincronização guarda preferências num repositório privado da sua própria conta.",
		"Você decide as seções antes do primeiro envio e pode revisar cada operação.",
		th.Ghost.Render("Token no cofre do sistema · conflitos não são mesclados em silêncio · binários nunca viajam"),
	}, "\n")
	header := component.Panel{
		Title: "conta GitHub", Glyph: "○", Accent: th.Faint, Width: width, Height: headerHeight,
	}.Render(th, headerContent)

	leftWidth := (width - 1) / 2
	rightWidth := width - leftWidth - 1
	local := m.viewStatePanel(th, "pronto para enviar", "⌂", th.Secondary,
		m.status.Local, maxStateCount(m.status.Local, usersync.State{}), leftWidth, contentHeight, false)
	benefits := []string{
		sectionHeader(th, "ao conectar", rightWidth-2),
		"✓ cria ou reutiliza “lealing-state” como privado",
		"✓ permite escolher favoritos, origens e referências de tools",
		"✓ detecta revisão remota antes de sobrescrever",
		"✓ mantém instalação de binários como decisão separada",
		"",
		component.FieldList{Rows: []component.Row{{
			Label: "Conectar ao GitHub", Value: "abrir device flow →", Tone: th.Accent,
		}}, Width: rightWidth - 2, Selected: 0, Focusable: true, Focused: true}.Render(th),
	}
	if value := feedback(th, rightWidth-2, m.busy, m.message, m.err); value != "" {
		benefits = append(benefits, "", value)
	}
	connect := component.Panel{
		Title: "conectar", Glyph: "→", Accent: th.Primary, Focused: true,
		Footer: "↵ iniciar login", Width: rightWidth, Height: contentHeight,
	}.Render(th, strings.Join(benefits, "\n"))
	return lipgloss.JoinVertical(lipgloss.Left, header, "",
		lipgloss.JoinHorizontal(lipgloss.Top, local, " ", connect))
}

func (m *Model) viewConnectedCompact(th *theme.Theme, width, height int) string {
	identity := "@" + m.status.Identity.Login
	if m.status.Identity.Name != "" {
		identity += " · " + m.status.Identity.Name
	}
	header := component.FieldList{Rows: []component.Row{
		{Label: "conta", Value: identity, Tone: th.Success},
		{Label: "repositório", Value: m.status.Identity.Login + "/" + m.status.Repository},
		{Label: "último sync", Value: formatTime(m.status.LastSync)},
		{Label: "GitHub", Value: remoteLabel(m.status), Tone: remoteTone(th, m.status)},
	}, Width: width, LabelWidth: 14}.Render(th)
	selectedAction := m.cursor - len(usersync.AllSections)
	lines := []string{
		header,
		"",
		sectionHeader(th, "escopo", width),
		component.FieldList{
			Rows: m.scopeRows(th, width), Width: width, Selected: m.cursor,
			Focusable: true, Focused: m.cursor < len(usersync.AllSections), LabelWidth: 22,
		}.Render(th),
		"",
		sectionHeader(th, "operações", width),
		component.FieldList{
			Rows: m.actionRows(th), Width: width, Selected: selectedAction,
			Focusable: true, Focused: selectedAction >= 0, LabelWidth: 22,
		}.Render(th),
	}
	if height >= 22 {
		lines = append(lines, "", th.Ghost.Render(component.TruncateTail(m.selectedDescription(), width)))
	}
	return appendFeedback(th, width, lines, m.busy, m.message, m.err)
}

func (m *Model) viewDisconnectedCompact(th *theme.Theme, width int) string {
	summary := m.status.Local.Summary()
	rows := []component.Row{
		{Label: "favoritos e uso", Value: fmt.Sprintf("%d nesta máquina", summary[usersync.SectionUsage])},
		{Label: "origens", Value: fmt.Sprintf("%d nesta máquina", summary[usersync.SectionSources])},
		{Label: "tools", Value: fmt.Sprintf("%d nesta máquina", summary[usersync.SectionTools])},
	}
	lines := []string{
		th.Strong.Render("Nenhuma conta conectada"),
		th.Ghost.Render(component.TruncateTail("Preferências ficam num repositório privado da sua conta.", width)),
		"",
		sectionHeader(th, "pronto para enviar", width),
		component.FieldList{Rows: rows, Width: width, LabelWidth: 18}.Render(th),
		"",
		component.FieldList{Rows: []component.Row{{
			Label: "Conectar ao GitHub", Value: "abrir device flow →", Tone: th.Accent,
		}}, Width: width, Selected: 0, Focusable: true, Focused: true}.Render(th),
	}
	return appendFeedback(th, width, lines, m.busy, m.message, m.err)
}

func (m *Model) scopeRows(th *theme.Theme, width int) []component.Row {
	local, remote := m.status.Local.Summary(), m.status.Remote.Summary()
	labelWidth := min(22, width/2)
	controlWidth := max(width-labelWidth-4, 4)
	rows := make([]component.Row, 0, len(usersync.AllSections))
	for index, section := range usersync.AllSections {
		value := fmt.Sprintf("%d aqui · %d lá", local[section], remote[section])
		if m.status.RemoteErr != nil {
			value = fmt.Sprintf("%d aqui · ? lá", local[section])
		}
		rows = append(rows, component.Row{
			Label: section.Label(), Value: value,
			Control: component.Spread(value,
				component.Toggle(th, m.status.Selection.Enabled(section), m.cursor == index), controlWidth),
		})
	}
	return rows
}

func (m *Model) actionRows(th *theme.Theme) []component.Row {
	return []component.Row{
		{Label: "Enviar esta máquina", Value: "push →", Tone: th.Accent},
		{Label: "Baixar do GitHub", Value: "pull →", Tone: th.Accent},
		{Label: "Desconectar", Value: "remover credencial →", Tone: th.Danger},
	}
}

func stateChart(th *theme.Theme, state usersync.State, scale, width int) string {
	summary := state.Summary()
	rows := make([]component.BarRow, 0, len(usersync.AllSections))
	for index, section := range usersync.AllSections {
		value := summary[section]
		rows = append(rows, component.BarRow{
			Label: section.Label(), Value: fmt.Sprintf("%d", value),
			Fraction: float64(value) / float64(max(scale, 1)), Tone: th.SpectrumAt(index),
		})
	}
	return component.BarChart{Rows: rows, Width: width, LabelWidth: 18}.Render(th)
}

func stateDetailRows(state usersync.State) []component.Row {
	favorites, runs := 0, 0
	var latest time.Time
	for _, usage := range state.Usage {
		if usage.Favorite {
			favorites++
		}
		runs += usage.Runs
		if usage.LastRun.After(latest) {
			latest = usage.LastRun
		}
	}
	activeOrigins := 0
	for _, origin := range state.Sources {
		if origin.Enabled {
			activeOrigins++
		}
	}
	return []component.Row{
		{Label: "registros de uso", Value: fmt.Sprintf("%d", len(state.Usage))},
		{Label: "favoritos", Value: fmt.Sprintf("%d", favorites)},
		{Label: "execuções", Value: fmt.Sprintf("%d", runs)},
		{Label: "origens ativas", Value: fmt.Sprintf("%d de %d", activeOrigins, len(state.Sources))},
		{Label: "última atividade", Value: formatTime(latest)},
	}
}

func statePreviews(th *theme.Theme, state usersync.State, width int) []string {
	items := make([]string, 0, len(state.Usage)+len(state.Sources)+len(state.Tools))
	for _, usage := range state.Usage {
		marker := "uso"
		if usage.Favorite {
			marker = "★ favorito"
		}
		items = append(items, fmt.Sprintf("%s · %s/%s · %d execuções", marker, usage.Host, usage.ID, usage.Runs))
	}
	for _, source := range state.Sources {
		status := "desligada"
		if source.Enabled {
			status = "ligada"
		}
		items = append(items, fmt.Sprintf("origem · %s · %s", source.Name, status))
	}
	for _, tool := range state.Tools {
		items = append(items, fmt.Sprintf("tool · %s/%s · %s", tool.Host, tool.ID, tool.Version))
	}
	if len(items) == 0 {
		return nil
	}
	limit := min(len(items), 2)
	lines := make([]string, 0, limit)
	for index := range limit {
		text := items[index]
		if index == limit-1 && len(items) > limit {
			text = fmt.Sprintf("%s  ·  +%d referências", text, len(items)-limit)
		}
		lines = append(lines, th.Ghost.Render(component.TruncateTail(text, width)))
	}
	return lines
}

func maxStateCount(states ...usersync.State) int {
	maximum := 1
	for _, state := range states {
		for _, count := range state.Summary() {
			maximum = max(maximum, count)
		}
	}
	return maximum
}

func sectionHeader(th *theme.Theme, title string, width int) string {
	label := th.PanelTitle.Render(strings.ToUpper(title))
	fill := max(width-lipgloss.Width(label)-1, 0)
	return component.TruncateTail(label+" "+th.Divider.Render(strings.Repeat("╌", fill)), width)
}

func (m *Model) viewDeviceFlow(th *theme.Theme, width, height int) string {
	code := lipgloss.NewStyle().Foreground(th.Primary).Bold(true).Render(m.device.UserCode)
	lines := []string{
		th.Strong.Render("Autorize o lealing no GitHub"),
		"",
		th.Dim.Render("1. Abra no navegador:"),
		lipgloss.NewStyle().Foreground(th.Accent).Render(component.TruncateTail(m.device.VerificationURL, width)),
		"",
		th.Dim.Render("2. Informe o código:"),
		code,
		"",
		th.Ghost.Render("Aguardando aprovação… pressione esc para cancelar."),
	}
	return component.Center(width, height, lines...)
}

func (m *Model) viewConfirmation(th *theme.Theme, width, height int) string {
	accent := th.Warning
	if m.confirmation.force || m.confirmation.operation == operationLogout {
		accent = th.Danger
	}
	lines := []string{
		lipgloss.NewStyle().Foreground(accent).Bold(true).Render(m.confirmation.title),
		"",
		wrap(m.confirmation.message, max(width-8, 12), 5),
		"",
		th.Ghost.Render("enter/y confirma · n/esc cancela"),
	}
	return component.Center(width, height, lines...)
}

func (m *Model) selectedDescription() string {
	if m.cursor < len(usersync.AllSections) {
		return "Enter alterna se esta seção pode sair desta máquina e ser aplicada ao baixar."
	}
	switch m.cursor - len(usersync.AllSections) {
	case 0:
		return "Publica no repositório privado somente as seções habilitadas."
	case 1:
		return "Aplica favoritos, uso e origens; tools ausentes nunca são instaladas em silêncio."
	default:
		return "Remove o token local; repositório e autorização permanecem no GitHub."
	}
}

func (m *Model) footer() string {
	switch {
	case m.confirmation != nil:
		return "↵ confirmar · esc cancelar"
	case m.device != nil:
		return "esc cancelar"
	case m.busy != "":
		return "aguarde"
	default:
		return "↵ agir · r atualizar"
	}
}

func (m *Model) Hints() []tui.Hint {
	switch {
	case m.confirmation != nil:
		return []tui.Hint{{Key: "y/↵", Label: "confirmar"}, {Key: "n/esc", Label: "cancelar"}}
	case m.device != nil:
		return []tui.Hint{{Key: "esc", Label: "cancelar login"}}
	case m.busy != "":
		return []tui.Hint{{Key: "esc", Label: "voltar"}}
	default:
		return []tui.Hint{{Key: "↑↓", Label: "ação"}, {Key: "↵", Label: "selecionar"},
			{Key: "r", Label: "atualizar"}, {Key: "esc", Label: "voltar"}}
	}
}

func (m *Model) Meta() []string {
	if m.status.Connected {
		return []string{"@" + m.status.Identity.Login, m.status.Repository}
	}
	return []string{"sem conta"}
}

func (m *Model) Status() (string, lipgloss.TerminalColor) {
	th := m.deps.Theme
	switch {
	case m.err != nil:
		return firstLine(m.err.Error()), th.Danger
	case m.busy != "":
		return m.busy, th.Accent
	case m.status.Diverged:
		return "o remoto mudou desde a última sincronização", th.Warning
	case m.status.Connected:
		return "sincronização pronta", th.Success
	default:
		return "conecte uma conta para sincronizar", th.Faint
	}
}

var (
	_ interface{ Refresh() tea.Cmd } = (*Model)(nil)
	_ interface{ Close() tea.Cmd }   = (*Model)(nil)
	_ interface{ Capturing() bool }  = (*Model)(nil)
	_ interface{ Meta() []string }   = (*Model)(nil)
	_ interface {
		Status() (string, lipgloss.TerminalColor)
	} = (*Model)(nil)
)

func appendFeedback(th *theme.Theme, width int, lines []string, busy, message string, err error) string {
	if value := feedback(th, width, busy, message, err); value != "" {
		lines = append(lines, "", value)
	}
	return strings.Join(lines, "\n")
}

func feedback(th *theme.Theme, width int, busy, message string, err error) string {
	switch {
	case err != nil:
		return lipgloss.NewStyle().Foreground(th.Danger).
			Render(component.TruncateTail("✗ "+firstLine(err.Error()), width))
	case busy != "":
		return lipgloss.NewStyle().Foreground(th.Accent).
			Render(component.TruncateTail("◌ "+busy, width))
	case message != "":
		return lipgloss.NewStyle().Foreground(th.Success).
			Render(component.TruncateTail("✓ "+message, width))
	}
	return ""
}

func enabledLabels(selection usersync.Selection) string {
	labels := make([]string, 0, len(usersync.AllSections))
	for _, section := range usersync.AllSections {
		if selection.Enabled(section) {
			labels = append(labels, section.Label())
		}
	}
	if len(labels) == 0 {
		return "nenhuma seção habilitada"
	}
	return strings.Join(labels, " · ")
}

func enabledCount(selection usersync.Selection) int {
	count := 0
	for _, section := range usersync.AllSections {
		if selection.Enabled(section) {
			count++
		}
	}
	return count
}

func remoteLabel(status usersync.Status) string {
	switch {
	case status.RemoteErr != nil:
		return "indisponível: " + firstLine(status.RemoteErr.Error())
	case status.RemoteMissing:
		return "ainda não publicado"
	case status.Diverged:
		return "mudou em outro dispositivo"
	default:
		return "disponível"
	}
}

func remoteTone(th *theme.Theme, status usersync.Status) lipgloss.TerminalColor {
	if status.RemoteErr != nil {
		return th.Danger
	}
	if status.Diverged {
		return th.Warning
	}
	return th.Success
}

func toneText(th *theme.Theme, text string, tone lipgloss.TerminalColor) string {
	if tone == nil {
		return th.Ghost.Render(text)
	}
	return lipgloss.NewStyle().Foreground(tone).Render(text)
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return "nunca"
	}
	return value.Format("02/01/2006 15:04 MST")
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	return line
}

func wrap(text string, width, maxLines int) string {
	if width <= 0 || maxLines <= 0 {
		return ""
	}
	words := strings.Fields(text)
	lines := make([]string, 0, maxLines)
	current := ""
	for _, word := range words {
		candidate := strings.TrimSpace(current + " " + word)
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
	if len(lines) > maxLines {
		lines = append(lines[:maxLines-1], strings.Join(lines[maxLines-1:], " "))
	}
	for index := range lines {
		lines[index] = component.TruncateTail(lines[index], width)
	}
	return strings.Join(lines, "\n")
}
