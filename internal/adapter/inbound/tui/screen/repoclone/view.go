package repoclone

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wordwrap"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/component"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	core "github.com/mateuslh/lealing/internal/core/repoclone"
)

// View implementa tui.Screen.
func (m *Model) View(f tui.Frame) string {
	th := m.deps.Theme
	switch m.phase {
	case phaseDiscovering:
		return component.Center(f.Width, f.Height,
			th.Dim.Render("consultando os repositórios no GitHub…"))
	case phaseResolving:
		return component.Center(f.Width, f.Height,
			th.Dim.Render("consultando detalhes do repositório…"))
	case phaseCloning:
		return component.Center(f.Width, f.Height,
			th.Dim.Render("clonando a família de projetos e atualizando o IntelliJ…"))
	}

	inner := max(f.Width-4, 20)
	var body string
	switch m.phase {
	case phaseConfirm:
		body = m.confirmView(th, inner, f.Height-2)
	case phaseAdding:
		body = m.addView(th, inner)
	case phaseDone:
		body = m.doneView(th, inner, f.Height-2)
	default:
		body = m.inputView(th, inner)
	}

	return lipgloss.NewStyle().
		Padding(1, 2).
		MaxWidth(f.Width).
		MaxHeight(f.Height).
		Render(body)
}

func (m *Model) inputView(th *theme.Theme, width int) string {
	instruction := component.TruncateTail(
		"Cole o link da página ou a URL HTTPS/SSH de clone.", width-4)
	field := th.Cursor.Render("› ") + m.input.View()
	message := th.Ghost.Render("o prefixo será revisado antes de clonar")
	if m.err != nil {
		message = lipgloss.NewStyle().Foreground(th.Danger).
			Render(component.TruncateTail("✗ "+firstLine(m.err.Error()), width-4))
	}

	return component.Panel{
		Title: "Repositório inicial", Glyph: "⇣", Accent: th.Primary,
		Width: width, Height: 5, Focused: true,
	}.Render(th, strings.Join([]string{instruction, field, message}, "\n"))
}

func (m *Model) addView(th *theme.Theme, width int) string {
	instruction := component.TruncateTail(
		"Adicione outro repositório do owner “"+m.plan.Source.Owner+"”.", width-4)
	field := th.Cursor.Render("+ ") + m.addInput.View()
	message := th.Ghost.Render("aceita o nome simples ou uma URL completa do GitHub")
	if m.err != nil {
		message = lipgloss.NewStyle().Foreground(th.Danger).
			Render(component.TruncateTail("✗ "+firstLine(m.err.Error()), width-4))
	}
	return component.Panel{
		Title: "Adicionar repositório", Glyph: "+", Accent: th.Secondary,
		Width: width, Height: 5, Focused: true,
	}.Render(th, strings.Join([]string{instruction, field, message}, "\n"))
}

func (m *Model) confirmView(th *theme.Theme, width, height int) string {
	selectionTone := lipgloss.TerminalColor(th.Success)
	if m.selectedCount() == 0 {
		selectionTone = th.Warning
	}
	rows := []component.Row{
		{Label: "GitHub", Value: m.plan.Source.Owner + "/" + m.plan.Source.Repository},
		{Label: "Destino", Value: shortPath(m.plan.Destination)},
		{
			Label: "Seleção",
			Value: fmt.Sprintf("%d incluídos · %d disponíveis",
				m.selectedCount(), len(m.plan.Repositories)),
			Tone: selectionTone,
		},
	}
	summary := component.Panel{
		Title: "Plano de clone", Glyph: "◇", Accent: th.Primary,
		Width: width, Height: len(rows) + 2,
	}.Render(th, component.FieldList{Rows: rows, Width: width - 4}.Render(th))

	feedback := m.feedbackView(th, width)
	feedbackHeight := 0
	if feedback != "" {
		feedbackHeight = 2 // linha em branco + mensagem
	}
	available := max(height-lipgloss.Height(summary)-1-feedbackHeight, 3)

	var workspace string
	if width >= 96 {
		listWidth := max(width*44/100, 38)
		detailWidth := width - listWidth - 1
		workspaceHeight := min(available, max(
			min(len(m.plan.Repositories)+2, available),
			m.repositoryDetailsHeight(),
		))
		workspace = lipgloss.JoinHorizontal(lipgloss.Top,
			m.repositoriesPanel(th, listWidth, workspaceHeight),
			" ",
			m.repositoryDetailsPanel(th, detailWidth, workspaceHeight),
		)
	} else {
		listHeight := max(min(available/2, len(m.plan.Repositories)+2), 3)
		detailHeight := max(available-listHeight-1, 3)
		workspace = lipgloss.JoinVertical(lipgloss.Left,
			m.repositoriesPanel(th, width, listHeight),
			"",
			m.repositoryDetailsPanel(th, width, detailHeight),
		)
	}

	blocks := []string{summary, "", workspace}
	if feedback != "" {
		blocks = append(blocks, "", feedback)
	}
	return lipgloss.JoinVertical(lipgloss.Left, blocks...)
}

func (m *Model) repositoriesPanel(th *theme.Theme, width, height int) string {
	visible := max((height-2)/2, 1)
	start := 0
	if len(m.plan.Repositories) > visible {
		start = min(max(m.cursor-visible/2, 0), len(m.plan.Repositories)-visible)
	}
	end := min(start+visible, len(m.plan.Repositories))

	lines := make([]string, 0, (end-start)*2)
	for i := start; i < end; i++ {
		lines = append(lines, m.repositoryLines(th, i, width-2)...)
	}
	if len(lines) == 0 {
		lines = append(lines, th.Ghost.Render("  lista vazia · use “a” para adicionar"))
	}

	footer := fmt.Sprintf("%d/%d · %d marcados",
		min(m.cursor+1, len(m.plan.Repositories)), len(m.plan.Repositories), m.selectedCount())
	return component.Panel{
		Title: "Repositórios", Glyph: "⌘", Accent: th.Accent, Focused: true,
		Footer: footer, Width: width, Height: height,
	}.Render(th, strings.Join(lines, "\n"))
}

func (m *Model) repositoryLines(th *theme.Theme, index, width int) []string {
	repo := m.plan.Repositories[index]
	selected := index < len(m.included) && m.included[index]

	cursor := "  "
	if index == m.cursor {
		cursor = th.Cursor.Render("▎") + " "
	}
	mark := lipgloss.NewStyle().Foreground(th.Success).Render("●")
	nameTone := th.SpectrumAt(index)
	if !selected {
		mark = th.Ghost.Render("○")
		nameTone = th.Faint
	}
	if repo.Archived {
		mark = lipgloss.NewStyle().Foreground(th.Warning).Render("◆")
	}
	left := cursor + mark + " " +
		lipgloss.NewStyle().Foreground(nameTone).Bold(index == m.cursor).Render(repo.Name)
	right := visibilityLabel(th, repo.Visibility)
	primary := component.Spread(
		component.TruncateTail(left, max(width-lipgloss.Width(right)-1, 4)),
		right, width)

	meta := make([]string, 0, 4)
	for _, value := range []string{repo.Language, repo.DefaultBranch, formatDiskUsage(repo.DiskUsageKB)} {
		if value != "" && value != "—" {
			meta = append(meta, value)
		}
	}
	if repo.Archived {
		meta = append(meta, "arquivado")
	}
	secondaryStyle := th.Dim
	if !selected {
		secondaryStyle = th.Ghost
	}
	secondary := secondaryStyle.Render(component.TruncateTail(
		"    "+strings.Join(meta, " · "), width))
	return []string{primary, secondary}
}

func (m *Model) repositoryDetailsPanel(th *theme.Theme, width, height int) string {
	if len(m.plan.Repositories) == 0 {
		return component.Panel{
			Title: "Detalhes", Glyph: "◎", Accent: th.Secondary,
			Width: width, Height: height,
		}.Render(th, th.Ghost.Render("adicione um repositório para ver os detalhes"))
	}

	repo := m.plan.Repositories[m.cursor]
	selected := m.cursor < len(m.included) && m.included[m.cursor]
	state, stateTone := "fora do clone", th.Faint
	if selected {
		state, stateTone = "incluído no clone", th.Success
	}
	if repo.Archived {
		state, stateTone = "arquivado · "+state, th.Warning
	}

	rows := []component.Row{
		{Label: "Estado", Value: state, Tone: stateTone},
		{Label: "Visibilidade", Value: orDash(strings.ToLower(repo.Visibility)), Tone: visibilityTone(th, repo.Visibility)},
		{Label: "Linguagem", Value: orDash(repo.Language), Tone: th.Secondary},
		{Label: "Branch", Value: orDash(repo.DefaultBranch)},
		{Label: "Atualizado", Value: formatUpdated(repo)},
		{Label: "Tamanho", Value: formatDiskUsage(repo.DiskUsageKB)},
		{Label: "Protocolo", Value: protocolLabel(m.plan.Source.Protocol), Tone: th.Accent},
		{Label: "Diretório", Value: shortPath(filepath.Join(m.plan.Destination, repo.Name))},
		{Label: "Clone", Value: orDash(repo.CloneURL)},
	}
	content := component.FieldList{Rows: rows, Width: width - 4}.Render(th)
	if description := strings.TrimSpace(repo.Description); description != "" {
		available := max(width-6, 10)
		wrapped := wordwrap.String(description, available)
		content += "\n" + th.Dim.Render("  Descrição") +
			"\n" + th.Body.Render("  "+strings.ReplaceAll(firstLines(wrapped, 2), "\n", "\n  "))
	}

	return component.Panel{
		Title: "Detalhes", Glyph: "◎", Accent: th.Secondary,
		Footer: "espaço inclui/exclui", Width: width, Height: height,
	}.Render(th, content)
}

func (m *Model) repositoryDetailsHeight() int {
	if len(m.plan.Repositories) == 0 {
		return 3
	}
	height := 11 // nove campos e duas bordas
	if strings.TrimSpace(m.plan.Repositories[m.cursor].Description) != "" {
		height += 4 // respiro, rótulo e até duas linhas de descrição
	}
	return height
}

func (m *Model) feedbackView(th *theme.Theme, width int) string {
	if m.err != nil {
		return lipgloss.NewStyle().Foreground(th.Danger).
			Render(component.TruncateTail("✗ "+firstLine(m.err.Error()), width))
	}
	if m.feedback != "" {
		return lipgloss.NewStyle().Foreground(th.Accent).
			Render(component.TruncateTail("◆ "+m.feedback, width))
	}
	return ""
}

func (m *Model) doneView(th *theme.Theme, width, height int) string {
	lines := make([]string, 0, len(m.result.Outcomes)+2)
	for _, outcome := range m.result.Outcomes {
		switch {
		case outcome.Err != nil:
			lines = append(lines, lipgloss.NewStyle().Foreground(th.Danger).
				Render("✗ "+outcome.Name+" · "+firstLine(outcome.Err.Error())))
		case outcome.Existing:
			lines = append(lines, lipgloss.NewStyle().Foreground(th.Warning).
				Render("• "+outcome.Name+" · já existia"))
		default:
			lines = append(lines, lipgloss.NewStyle().Foreground(th.Success).
				Render("✓ "+outcome.Name+" · clonado"))
		}
	}
	if m.result.RecentWarning != "" {
		lines = append(lines, lipgloss.NewStyle().Foreground(th.Warning).
			Render("! IntelliJ · "+firstLine(m.result.RecentWarning)))
	}
	if m.err != nil {
		lines = append(lines, lipgloss.NewStyle().Foreground(th.Danger).
			Render("✗ "+firstLine(m.err.Error())))
	}
	if len(lines) == 0 {
		lines = append(lines, th.Ghost.Render("nenhum resultado"))
	}

	title, glyph, accent := "Concluído", "✓", th.Success
	if m.err != nil {
		title, glyph, accent = "Concluído com falhas", "!", th.Warning
	}
	panelHeight := max(min(height, len(lines)+2), 3)
	return component.Panel{
		Title: title, Glyph: glyph, Accent: accent,
		Footer: shortPath(m.result.Destination),
		Width:  width, Height: panelHeight,
	}.Render(th, strings.Join(lines, "\n"))
}

// Hints implementa tui.Screen.
func (m *Model) Hints() []tui.Hint {
	switch m.phase {
	case phaseInput:
		return []tui.Hint{{Key: "↵", Label: "buscar"}, {Key: "esc", Label: "voltar"}}
	case phaseConfirm:
		return []tui.Hint{
			{Key: "↑↓", Label: "navegar"},
			{Key: "espaço", Label: "incluir"},
			{Key: "a", Label: "adicionar"},
			{Key: "d", Label: "remover"},
			{Key: "↵", Label: "clonar"},
			{Key: "e/esc", Label: "editar"},
		}
	case phaseAdding:
		return []tui.Hint{{Key: "↵", Label: "adicionar"}, {Key: "esc", Label: "cancelar"}}
	case phaseDone:
		return []tui.Hint{{Key: "r", Label: "novo clone"}, {Key: "esc", Label: "voltar"}}
	default:
		return []tui.Hint{{Key: "esc", Label: "aguarde a operação"}}
	}
}

// Meta alimenta a topbar depois da descoberta.
func (m *Model) Meta() []string {
	if len(m.plan.Repositories) == 0 {
		return nil
	}
	return []string{
		strconv.Itoa(m.selectedCount()) + "/" + strconv.Itoa(len(m.plan.Repositories)) + " repos",
	}
}

// Status alimenta a barra de status.
func (m *Model) Status() (string, lipgloss.TerminalColor) {
	th := m.deps.Theme
	switch m.phase {
	case phaseDiscovering:
		return "consultando GitHub…", th.Accent
	case phaseResolving:
		return "buscando detalhes no GitHub…", th.Secondary
	case phaseCloning:
		return "git clone em andamento…", th.Accent
	case phaseDone:
		if m.err != nil || m.result.RecentWarning != "" {
			return "concluído com avisos", th.Warning
		}
		return "projetos prontos no IntelliJ", th.Success
	case phaseConfirm:
		return fmt.Sprintf("%d repositórios selecionados", m.selectedCount()), th.Success
	default:
		return "GitHub · HTTPS ou SSH", th.Faint
	}
}

func shortPath(path string) string {
	if path == "" {
		return ""
	}
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) <= 4 {
		return filepath.ToSlash(path)
	}
	return "…/" + strings.Join(parts[len(parts)-3:], "/")
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}

func firstLines(s string, limit int) string {
	lines := strings.Split(s, "\n")
	return strings.Join(lines[:min(len(lines), limit)], "\n")
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

func visibilityLabel(th *theme.Theme, visibility string) string {
	label := strings.ToUpper(visibility)
	if label == "" {
		label = "—"
	}
	return lipgloss.NewStyle().
		Foreground(visibilityTone(th, visibility)).
		Background(th.Overlay).
		Padding(0, 1).
		Bold(true).
		Render(label)
}

func visibilityTone(th *theme.Theme, visibility string) lipgloss.TerminalColor {
	switch strings.ToUpper(visibility) {
	case "PUBLIC":
		return th.Success
	case "PRIVATE":
		return th.Warning
	case "INTERNAL":
		return th.Accent
	default:
		return th.Faint
	}
}

func formatUpdated(repo core.Repository) string {
	if repo.UpdatedAt.IsZero() {
		return "—"
	}
	return repo.UpdatedAt.Local().Format("02/01/2006 15:04")
}

func formatDiskUsage(kb int) string {
	switch {
	case kb <= 0:
		return "—"
	case kb >= 1024*1024:
		return fmt.Sprintf("%.1f GB", float64(kb)/(1024*1024))
	case kb >= 1024:
		return fmt.Sprintf("%.1f MB", float64(kb)/1024)
	default:
		return strconv.Itoa(kb) + " KB"
	}
}

func protocolLabel(protocol core.Protocol) string {
	if protocol == core.ProtocolSSH {
		return "SSH"
	}
	return "HTTPS"
}
