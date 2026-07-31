package gitinsight

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/component"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	core "github.com/mateuslh/lealing/internal/core/gitinsight"
)

// View implementa tui.Screen.
func (m *Model) View(f tui.Frame) string {
	th := m.deps.Theme
	if m.loading {
		return component.Center(f.Width, f.Height,
			th.Dim.Render("mapeando clones e branches em "+shortPath(m.report.Root)+"…"))
	}
	if m.err != nil && len(m.report.Repositories) == 0 {
		return component.Center(f.Width, f.Height,
			lipgloss.NewStyle().Foreground(th.Danger).Render("✗ "+m.err.Error()))
	}
	if len(m.report.Repositories) == 0 {
		return component.Center(f.Width, f.Height,
			th.Ghost.Render("nenhum repositório Git encontrado em "+shortPath(m.report.Root)))
	}

	inner := max(f.Width-4, 20)
	kpis := m.kpiView(th, inner)
	filters := m.filterView(th, inner)
	available := max(f.Height-2-lipgloss.Height(kpis)-lipgloss.Height(filters)-2, 3)

	var workspace string
	if inner >= 96 {
		listWidth := max(inner*42/100, 38)
		workspace = lipgloss.JoinHorizontal(lipgloss.Top,
			m.repositoryPanel(th, listWidth, available),
			" ",
			m.detailPanel(th, inner-listWidth-1, available),
		)
	} else {
		listHeight := max(available/2, 3)
		workspace = lipgloss.JoinVertical(lipgloss.Left,
			m.repositoryPanel(th, inner, listHeight),
			"",
			m.detailPanel(th, inner, max(available-listHeight-1, 3)),
		)
	}

	body := lipgloss.JoinVertical(lipgloss.Left, kpis, filters, "", workspace)
	base := lipgloss.NewStyle().
		Padding(1, 2).
		MaxWidth(f.Width).
		MaxHeight(f.Height).
		Render(body)
	if m.mode == modeConfirm {
		return m.confirmationPopup(th, f, base)
	}
	if m.mode == modeRunning && m.operation.action == actionUpdateAll {
		return m.updateRunningPopup(th, f, base)
	}
	if m.mode == modeResults {
		return m.updateResultsPopup(th, f, base)
	}
	return base
}

type kpi struct {
	title, glyph, value, label string
	tone                       lipgloss.TerminalColor
}

func (m *Model) kpiView(th *theme.Theme, width int) string {
	stats := m.report.Stats()
	items := []kpi{
		{"Clones", "⌘", strconv.Itoa(stats.Repositories), strconv.Itoa(stats.Branches) + " branches", th.Primary},
		{
			"Para push", "↑", strconv.Itoa(stats.UnpushedCommits),
			fmt.Sprintf("em %d branches", stats.NeedPush), th.Danger,
		},
		{"Publicadas", "✓", strconv.Itoa(stats.Cleanup), "já no remoto", th.Success},
		{"Sem upstream", "?", strconv.Itoa(stats.NoUpstream), "pedem revisão", th.Secondary},
		{"Alterados", "◆", strconv.Itoa(stats.DirtyRepos), "working trees", th.Warning},
	}

	if width < 92 {
		parts := make([]string, 0, len(items))
		for _, item := range items {
			parts = append(parts, lipgloss.NewStyle().Foreground(item.tone).Bold(true).
				Render(item.glyph+" "+item.value+" "+strings.ToLower(item.title)))
		}
		return component.TruncateTail(strings.Join(parts, th.Ghost.Render("  ·  ")), width)
	}

	const gap = 1
	base := (width - gap*(len(items)-1)) / len(items)
	rest := width - gap*(len(items)-1) - base*len(items)
	blocks := make([]string, len(items))
	for i, item := range items {
		itemWidth := base
		if i == len(items)-1 {
			itemWidth += rest
		}
		content := lipgloss.NewStyle().Foreground(item.tone).Bold(true).Render(item.value) +
			th.Ghost.Render("  "+item.label)
		blocks[i] = component.Panel{
			Title: item.title, Glyph: item.glyph, Accent: item.tone,
			Width: itemWidth, Height: 3,
		}.Render(th, content)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, intersperse(blocks, " ")...)
}

func (m *Model) filterView(th *theme.Theme, width int) string {
	parts := make([]string, 0, filterCount)
	for i := filter(0); i < filterCount; i++ {
		text := strconv.Itoa(int(i)+1) + " " + filterLabels[i] + " " + strconv.Itoa(m.filterCount(i))
		tone := filterTone(th, i)
		style := lipgloss.NewStyle().
			Foreground(tone).
			Background(th.Overlay).
			Padding(0, 1)
		if i == m.filter {
			style = lipgloss.NewStyle().
				Foreground(th.OnPrimary).
				Background(tone).
				Bold(true).
				Padding(0, 1)
		}
		parts = append(parts, style.Render(text))
	}
	return component.TruncateTail(strings.Join(parts, " "), width)
}

func filterTone(th *theme.Theme, active filter) lipgloss.TerminalColor {
	switch active {
	case filterPush:
		return th.Danger
	case filterCleanup:
		return th.Success
	case filterUntracked:
		return th.Secondary
	case filterDirty:
		return th.Warning
	default:
		return th.Primary
	}
}

func (m *Model) repositoryPanel(th *theme.Theme, width, height int) string {
	repos := m.repositories()
	visible := max((height-2)/2, 1)
	start := 0
	if len(repos) > visible {
		start = min(max(m.cursor-visible/2, 0), len(repos)-visible)
	}
	end := min(start+visible, len(repos))

	lines := make([]string, 0, (end-start)*2)
	for i := start; i < end; i++ {
		lines = append(lines, m.repositoryLines(th, repos[i], i, i == m.cursor, width-2)...)
	}
	if len(lines) == 0 {
		lines = append(lines, th.Ghost.Render("  nenhum clone neste filtro"))
	}

	footer := fmt.Sprintf("%d/%d", min(m.cursor+1, len(repos)), len(repos))
	return component.Panel{
		Title: "Repositórios", Glyph: "⌘", Accent: th.Primary, Focused: true,
		Footer: footer, Width: width, Height: height,
	}.Render(th, strings.Join(lines, "\n"))
}

func (m *Model) repositoryLines(
	th *theme.Theme,
	repo core.Repository,
	index int,
	selected bool,
	width int,
) []string {
	tone, mark := repositoryTone(th, repo)
	cursor := "  "
	if selected {
		cursor = th.Cursor.Render("▎") + " "
	}
	left := cursor + lipgloss.NewStyle().Foreground(tone).Render(mark) + " " +
		th.SpectrumStyle(index).Bold(selected).Render(repo.Relative)
	right := th.Ghost.Render("⑂ " + repo.CurrentBranch())
	primary := component.Spread(
		component.TruncateTail(left, max(width-lipgloss.Width(right)-1, 4)),
		right, width)

	detail := "    " + repoSummary(th, repo)
	if repo.Err != "" {
		detail = "    " + lipgloss.NewStyle().Foreground(th.Danger).
			Render("✗ "+firstLine(repo.Err))
	}
	return []string{primary, component.TruncateTail(detail, width)}
}

func repositoryTone(th *theme.Theme, repo core.Repository) (lipgloss.TerminalColor, string) {
	switch {
	case repo.Err != "":
		return th.Danger, "✗"
	case len(repo.PushBranches()) > 0:
		return th.Danger, "↑"
	case len(repo.UntrackedBranches()) > 0:
		return th.Secondary, "?"
	case repo.DirtyFiles > 0:
		return th.Warning, "◆"
	case len(repo.CleanupBranches()) > 0:
		return th.Success, "✓"
	default:
		return th.Primary, "●"
	}
}

func repoSummary(th *theme.Theme, repo core.Repository) string {
	var states []string
	if n := len(repo.PushBranches()); n > 0 {
		states = append(states, lipgloss.NewStyle().Foreground(th.Danger).Bold(true).
			Render("↑"+strconv.Itoa(n)+" enviar"))
	}
	if n := len(repo.CleanupBranches()); n > 0 {
		states = append(states, lipgloss.NewStyle().Foreground(th.Success).
			Render("✓"+strconv.Itoa(n)+" local publicada"))
	}
	if n := len(repo.UntrackedBranches()); n > 0 {
		states = append(states, lipgloss.NewStyle().Foreground(th.Secondary).
			Render("?"+strconv.Itoa(n)+" sem upstream"))
	}
	if repo.DirtyFiles > 0 {
		states = append(states, lipgloss.NewStyle().Foreground(th.Warning).
			Render("◆"+strconv.Itoa(repo.DirtyFiles)+" alterações"))
	}
	if len(states) == 0 {
		states = append(states, lipgloss.NewStyle().Foreground(th.Success).
			Render("● sincronizado"))
	}
	return strings.Join(states, th.Ghost.Render(" · "))
}

func (m *Model) detailPanel(th *theme.Theme, width, height int) string {
	repo, ok := m.selected()
	if !ok {
		return component.Panel{
			Title: "Branches", Glyph: "⑂", Accent: th.Accent,
			Width: width, Height: height,
		}.Render(th, th.Ghost.Render("nenhum repositório neste filtro"))
	}
	if m.mode == modePick || m.mode == modeRunning {
		return m.actionPanel(th, repo, width, height)
	}

	header := []component.Row{
		{Label: "Atual", Value: repo.CurrentBranch(), Tone: th.Primary},
		{Label: "Branches", Value: strconv.Itoa(len(repo.Branches))},
		{Label: "Visão", Value: filterLabels[m.filter], Tone: filterTone(th, m.filter)},
		{Label: "Working tree", Value: workingTreeLabel(repo), Tone: workingTreeTone(th, repo)},
	}
	content := component.FieldList{Rows: header, Width: width - 4}.Render(th)
	lines := m.branchLines(th, repo, width-4)
	visible := max(height-2-lipgloss.Height(content)-1, 0)
	start := min(m.detailOffset, max(len(lines)-visible, 0))
	end := min(start+visible, len(lines))
	if visible > 0 && end > start {
		content += "\n" + strings.Join(lines[start:end], "\n")
	}

	footer := ""
	if visible > 0 && len(lines) > visible {
		footer = fmt.Sprintf("%d-%d/%d", min(start+1, len(lines)), end, len(lines))
	}
	title := repo.Name
	if m.filter != filterAll {
		title += " · " + filterLabels[m.filter]
	}
	return component.Panel{
		Title: title, Glyph: filterGlyph(m.filter), Accent: filterTone(th, m.filter),
		Footer: footer, Width: width, Height: height,
	}.Render(th, content)
}

func filterGlyph(active filter) string {
	switch active {
	case filterPush:
		return "↑"
	case filterCleanup:
		return "✓"
	case filterUntracked:
		return "?"
	case filterDirty:
		return "◆"
	default:
		return "⑂"
	}
}

func (m *Model) actionPanel(
	th *theme.Theme,
	repo core.Repository,
	width, height int,
) string {
	var tone lipgloss.TerminalColor = th.Accent
	title := repo.Name
	glyph := "⑂"
	footer := ""
	contentWidth := max(width-4, 1)
	var lines []string

	switch m.mode {
	case modePick:
		branches := m.actionBranches()
		title = "Escolher branch"
		if m.action == actionPush {
			tone, glyph = th.Danger, "↑"
			lines = append(lines,
				lipgloss.NewStyle().Foreground(tone).Bold(true).Render("PUBLICAR NO UPSTREAM"),
				th.Dim.Render(component.TruncateTail(repo.Relative, contentWidth)),
				"",
			)
		} else {
			tone, glyph = th.Success, "✓"
			lines = append(lines,
				lipgloss.NewStyle().Foreground(tone).Bold(true).Render("REMOVER SOMENTE DO CLONE"),
				th.Dim.Render(component.TruncateTail(repo.Relative, contentWidth)),
				"",
			)
		}
		visible := max(height-5, 1)
		start := 0
		if len(branches) > visible {
			start = min(max(m.actionCursor-visible/2, 0), len(branches)-visible)
			footer = fmt.Sprintf("%d/%d", m.actionCursor+1, len(branches))
		}
		end := min(start+visible, len(branches))
		for i := start; i < end; i++ {
			branch := branches[i]
			cursor := "  "
			style := lipgloss.NewStyle().Foreground(tone)
			if i == m.actionCursor {
				cursor = th.Cursor.Render("▎") + " "
				style = style.Bold(true)
			}
			right := branch.Upstream
			if m.action == actionPush {
				right = "↑" + strconv.Itoa(branch.Ahead)
			}
			lines = append(lines, component.Spread(
				cursor+style.Render(branch.Name),
				th.Ghost.Render(right),
				contentWidth,
			))
		}

	case modeRunning:
		title = "Executando " + actionLabel(m.operation.action)
		tone = th.Primary
		lines = []string{
			"",
			lipgloss.NewStyle().Foreground(tone).Bold(true).Render(
				"◌ " + runningLabel(m.operation)),
			"",
			th.Dim.Render(component.TruncateTail(
				"aguarde a conclusão do comando Git…", contentWidth)),
		}
	}

	visible := max(height-2, 0)
	if len(lines) > visible {
		lines = lines[:visible]
	}
	return component.Panel{
		Title: title, Glyph: glyph, Accent: tone, Focused: true,
		Footer: footer, Width: width, Height: height,
	}.Render(th, strings.Join(lines, "\n"))
}

func confirmationControls(th *theme.Theme, width int) string {
	yes, no := "[ s / enter ] confirmar", "[ n ] cancelar"
	if width < 32 {
		yes, no = "s confirmar", "n cancelar"
	}
	return component.Spread(
		lipgloss.NewStyle().Foreground(th.Success).Bold(true).Render(yes),
		th.Dim.Render(no),
		width,
	)
}

func (m *Model) confirmationPopup(th *theme.Theme, f tui.Frame, background string) string {
	if m.operation.action == actionUpdateAll {
		return m.updateAllConfirmationPopup(th, f, background)
	}

	width := min(max(f.Width*3/5, 48), max(f.Width-4, 20))
	height := min(13, max(f.Height-2, 5))
	contentWidth := max(width-4, 1)
	tone := lipgloss.TerminalColor(th.Warning)
	glyph := "⌫"
	title := "CONFIRMAR REMOÇÃO LOCAL"
	verb := "REMOVER BRANCH DESTE CLONE"
	impact := "O remoto não será apagado e o Git recusará commits não integrados."

	if m.operation.action == actionPush {
		tone = th.Danger
		glyph = "↑"
		title = "CONFIRMAR PUSH"
		verb = "PUBLICAR COMMITS NO UPSTREAM"
		impact = "Esta ação altera o repositório remoto configurado."
	}

	lines := []string{
		"",
		lipgloss.NewStyle().Foreground(tone).Bold(true).
			Render(component.TruncateTail(glyph+"  "+verb, contentWidth)),
		"",
		component.Spread(th.Dim.Render("Repositório"), m.operation.repo.Relative, contentWidth),
		component.Spread(th.Dim.Render("Branch local"), m.operation.branch.Name, contentWidth),
		component.Spread(th.Dim.Render("Upstream"), m.operation.branch.Upstream, contentWidth),
	}
	if m.operation.action == actionPush {
		lines = append(lines, component.Spread(
			th.Dim.Render("Commits pendentes"),
			strconv.Itoa(m.operation.branch.Ahead),
			contentWidth,
		))
	}
	lines = append(lines,
		"",
		lipgloss.NewStyle().Foreground(tone).
			Render(component.TruncateTail("! "+impact, contentWidth)),
		"",
		confirmationControls(th, contentWidth),
	)

	available := max(height-2, 0)
	if len(lines) > available {
		lines = compactPopupLines(th, m.operation, tone, contentWidth)
	}
	if len(lines) > available {
		lines = lines[:available]
	}

	content := strings.Join(lines, "\n")
	modal := component.ColorFrame{
		Title: title, Width: width, Height: height,
	}.Render(th, content)
	return popupOverlay(th, f, background, modal)
}

func (m *Model) updateAllConfirmationPopup(
	th *theme.Theme,
	f tui.Frame,
	background string,
) string {
	width := min(max(f.Width*3/5, 52), max(f.Width-4, 20))
	height := min(12, max(f.Height-2, 6))
	if width < 64 {
		height = min(6, max(f.Height-2, 4))
	}
	contentWidth := max(width-4, 1)
	lines := []string{
		"",
		lipgloss.NewStyle().Foreground(th.Primary).Bold(true).
			Render(component.TruncateTail("↻  ATUALIZAR MAIN/MASTER DE TODOS", contentWidth)),
		"",
		component.Spread(th.Dim.Render("Repositórios"),
			strconv.Itoa(m.operation.total), contentWidth),
		component.Spread(th.Dim.Render("Remotos"),
			"fetch --all --prune", contentWidth),
		component.Spread(th.Dim.Render("Avanço"),
			"somente fast-forward", contentWidth),
		"",
		lipgloss.NewStyle().Foreground(th.Warning).Render(component.TruncateTail(
			"! Alterados ou com commits locais serão ignorados.", contentWidth)),
		"",
		confirmationControls(th, contentWidth),
	}
	available := max(height-2, 0)
	if len(lines) > available {
		lines = []string{
			lipgloss.NewStyle().Foreground(th.Primary).Bold(true).
				Render("↻ atualizar " + strconv.Itoa(m.operation.total) + " clones"),
			th.Dim.Render("main/master · somente fast-forward"),
			lipgloss.NewStyle().Foreground(th.Warning).Render("alterados serão ignorados"),
			confirmationControls(th, contentWidth),
		}
	}
	if len(lines) > available {
		lines = lines[:available]
	}
	modal := component.ColorFrame{
		Title: "CONFIRMAR ATUALIZAÇÃO GERAL", Width: width, Height: height,
	}.Render(th, strings.Join(lines, "\n"))
	return popupOverlay(th, f, background, modal)
}

func (m *Model) updateRunningPopup(
	th *theme.Theme,
	f tui.Frame,
	background string,
) string {
	width := min(max(f.Width/2, 46), max(f.Width-4, 20))
	height := min(7, max(f.Height-2, 4))
	contentWidth := max(width-4, 1)
	lines := []string{
		"",
		lipgloss.NewStyle().Foreground(th.Primary).Bold(true).Render(
			component.TruncateTail("◌ atualizando "+strconv.Itoa(m.operation.total)+" clones…", contentWidth)),
		"",
		th.Dim.Render(component.TruncateTail(
			"fetch e fast-forward em até 10 processos paralelos", contentWidth)),
	}
	modal := component.ColorFrame{
		Title: "ATUALIZAÇÃO EM ANDAMENTO", Width: width, Height: height,
	}.Render(th, strings.Join(lines, "\n"))
	return popupOverlay(th, f, background, modal)
}

func (m *Model) updateResultsPopup(
	th *theme.Theme,
	f tui.Frame,
	background string,
) string {
	width := min(max(f.Width*4/5, 60), max(f.Width-4, 20))
	height := min(max(f.Height-4, 8), 24)
	contentWidth := max(width-4, 1)
	stats := m.updateReport.Stats()
	summary := strings.Join([]string{
		lipgloss.NewStyle().Foreground(th.Success).Bold(true).
			Render("↑ " + strconv.Itoa(stats.Updated) + " atualizados"),
		lipgloss.NewStyle().Foreground(th.Primary).
			Render("● " + strconv.Itoa(stats.Current) + " em dia"),
		lipgloss.NewStyle().Foreground(th.Warning).
			Render("◇ " + strconv.Itoa(stats.Skipped) + " ignorados"),
		lipgloss.NewStyle().Foreground(th.Danger).
			Render("✗ " + strconv.Itoa(stats.Failed) + " falhas"),
	}, th.Ghost.Render("  ·  "))

	compact := height < 13
	rowHeight := 2
	if compact {
		rowHeight = 1
	}
	availableRows := max(height-6, 1)
	visible := max(availableRows/rowHeight, 1)
	start := 0
	results := m.updateReport.Results
	if len(results) > visible {
		start = min(max(m.resultCursor-visible/2, 0), len(results)-visible)
	}
	end := min(start+visible, len(results))

	lines := []string{component.TruncateTail(summary, contentWidth)}
	if compact && len(results) > 0 {
		selected := results[min(m.resultCursor, len(results)-1)]
		lines = append(lines, th.Dim.Render(component.TruncateTail(
			"selecionado: "+selected.Detail, contentWidth)))
	}
	lines = append(lines, "")
	for i := start; i < end; i++ {
		result := results[i]
		tone, glyph := updateResultTone(th, result.State)
		cursor := "  "
		if i == m.resultCursor {
			cursor = th.Cursor.Render("▎") + " "
		}
		right := result.Branch
		if right == "" {
			right = "—"
		}
		lines = append(lines, component.Spread(
			cursor+lipgloss.NewStyle().Foreground(tone).Bold(i == m.resultCursor).
				Render(glyph+" "+result.Repository),
			th.Ghost.Render(right),
			contentWidth,
		))
		if !compact {
			lines = append(lines, th.Dim.Render(component.TruncateTail(
				"    "+result.Detail, contentWidth)))
		}
	}
	lines = append(lines, "", component.Spread(
		th.Ghost.Render("↑↓ navegar"),
		th.Ghost.Render("enter / esc fechar"),
		contentWidth,
	))
	if len(lines) > max(height-2, 0) {
		lines = lines[:max(height-2, 0)]
	}

	title := "ATUALIZAÇÃO CONCLUÍDA"
	if stats.Failed > 0 {
		title = "ATUALIZAÇÃO CONCLUÍDA COM FALHAS"
	}
	modal := component.ColorFrame{
		Title: title, Width: width, Height: height,
	}.Render(th, strings.Join(lines, "\n"))
	return popupOverlay(th, f, background, modal)
}

func popupOverlay(
	th *theme.Theme,
	f tui.Frame,
	background, modal string,
) string {
	if f.Width < 70 {
		return component.Center(f.Width, f.Height, modal)
	}
	if lipgloss.Width(modal)+2 <= f.Width && lipgloss.Height(modal)+2 <= f.Height {
		modal = lipgloss.NewStyle().
			Background(th.Base).
			Padding(1).
			Render(modal)
	}
	return component.Overlay(background, modal, f.Width, f.Height)
}

func updateResultTone(
	th *theme.Theme,
	state core.UpdateState,
) (lipgloss.TerminalColor, string) {
	switch state {
	case core.UpdateUpdated:
		return th.Success, "↑"
	case core.UpdateSkipped:
		return th.Warning, "◇"
	case core.UpdateFailed:
		return th.Danger, "✗"
	default:
		return th.Primary, "●"
	}
}

func updateSummaryTone(
	th *theme.Theme,
	stats core.UpdateStats,
) lipgloss.TerminalColor {
	if stats.Failed > 0 {
		return th.Danger
	}
	if stats.Skipped > 0 {
		return th.Warning
	}
	return th.Success
}

func compactPopupLines(
	th *theme.Theme,
	operation operation,
	tone lipgloss.TerminalColor,
	width int,
) []string {
	detail := "somente local · git branch -d"
	if operation.action == actionPush {
		detail = operation.branch.Upstream + " · ↑" + strconv.Itoa(operation.branch.Ahead) + " commits"
	}
	return []string{
		lipgloss.NewStyle().Foreground(tone).Bold(true).
			Render(component.TruncateTail(operation.branch.Name, width)),
		th.Dim.Render(component.TruncateTail(operation.repo.Relative, width)),
		th.Dim.Render(component.TruncateTail(detail, width)),
		confirmationControls(th, width),
	}
}

func runningLabel(operation operation) string {
	switch operation.action {
	case actionPush:
		return "publicando " + operation.branch.Name
	case actionDelete:
		return "removendo " + operation.branch.Name
	case actionFetch:
		return "atualizando remotos de " + operation.repo.Relative
	case actionUpdateAll:
		return "atualizando " + strconv.Itoa(operation.total) + " repositórios"
	default:
		return "processando"
	}
}

func (m *Model) branchLines(th *theme.Theme, repo core.Repository, width int) []string {
	var lines []string
	if m.filter == filterAll || m.filter == filterPush {
		lines = appendBranchSection(lines, th, "↑ COMMITS PARA ENVIAR", th.Danger,
			repo.PushBranches(), width, func(branch core.Branch) string {
				return fmt.Sprintf("↑%d  ↓%d", branch.Ahead, branch.Behind)
			})
	}
	if m.filter == filterAll || m.filter == filterCleanup {
		lines = appendBranchSection(lines, th, "✓ LOCAL JÁ PUBLICADA", th.Success,
			repo.CleanupBranches(), width, func(branch core.Branch) string {
				if branch.Behind > 0 {
					return "remoto +" + strconv.Itoa(branch.Behind)
				}
				return "sincronizada"
			})
	}
	if m.filter == filterAll {
		lines = appendBranchSection(lines, th, "● ATUAL SINCRONIZADA", th.Primary,
			repo.SyncedCurrentBranches(), width, func(branch core.Branch) string {
				if branch.Behind > 0 {
					return "remoto +" + strconv.Itoa(branch.Behind)
				}
				return "em dia"
			})
	}
	if m.filter == filterAll || m.filter == filterUntracked {
		lines = appendBranchSection(lines, th, "? SEM UPSTREAM CONFIÁVEL", th.Secondary,
			repo.UntrackedBranches(), width, func(branch core.Branch) string {
				if branch.Gone {
					return "upstream removido"
				}
				return "publicação incerta"
			})
	}
	if m.filter == filterDirty {
		lines = appendWorkingTree(lines, th, repo, width)
	}
	if len(lines) == 0 {
		empty := "✓ nada neste tipo"
		if m.filter == filterAll {
			empty = "✓ todas as branches sincronizadas"
		}
		lines = append(lines, "",
			lipgloss.NewStyle().Foreground(th.Success).Render(empty))
	}
	return lines
}

func appendWorkingTree(
	lines []string,
	th *theme.Theme,
	repo core.Repository,
	width int,
) []string {
	if repo.Err != "" {
		return append(lines,
			lipgloss.NewStyle().Foreground(th.Danger).Bold(true).Render("✗ ERRO DE LEITURA"),
			th.Dim.Render(component.TruncateTail("  "+firstLine(repo.Err), width)),
		)
	}
	return append(lines,
		lipgloss.NewStyle().Foreground(th.Warning).Bold(true).Render("◆ WORKING TREE ALTERADA"),
		lipgloss.NewStyle().Foreground(th.Warning).Render(
			fmt.Sprintf("  %d arquivos com mudanças locais", repo.DirtyFiles)),
		th.Dim.Render(component.TruncateTail(
			"  revise com `git status` antes de trocar ou remover branches", width)),
	)
}

func appendBranchSection(
	lines []string,
	th *theme.Theme,
	title string,
	tone lipgloss.TerminalColor,
	branches []core.Branch,
	width int,
	status func(core.Branch) string,
) []string {
	if len(branches) == 0 {
		return lines
	}
	if len(lines) > 0 {
		lines = append(lines, "")
	}
	lines = append(lines, lipgloss.NewStyle().Foreground(tone).Bold(true).Render(title))
	for _, branch := range branches {
		left := "  " + branch.Name
		if branch.Current {
			left = "★ " + branch.Name
		}
		lines = append(lines, component.Spread(
			lipgloss.NewStyle().Foreground(tone).Render(left),
			th.Ghost.Render(status(branch)), width))
		detail := branch.Hash
		if branch.Upstream != "" {
			detail += "  " + branch.Upstream
		}
		if branch.Subject != "" {
			detail += "  " + branch.Subject
		}
		lines = append(lines, th.Dim.Render(component.TruncateTail("    "+detail, width)))
	}
	return lines
}

func workingTreeLabel(repo core.Repository) string {
	if repo.Err != "" {
		return "erro de leitura"
	}
	if repo.DirtyFiles == 0 {
		return "limpa"
	}
	return fmt.Sprintf("%d alterações locais", repo.DirtyFiles)
}

func workingTreeTone(th *theme.Theme, repo core.Repository) lipgloss.TerminalColor {
	if repo.Err != "" {
		return th.Danger
	}
	if repo.DirtyFiles > 0 {
		return th.Warning
	}
	return th.Success
}

// Hints implementa tui.Screen.
func (m *Model) Hints() []tui.Hint {
	switch m.mode {
	case modePick:
		return []tui.Hint{
			{Key: "↑↓", Label: "branch"},
			{Key: "enter", Label: "escolher"},
			{Key: "esc", Label: "cancelar"},
		}
	case modeConfirm:
		return []tui.Hint{
			{Key: "s/enter", Label: "confirmar"},
			{Key: "n/esc", Label: "cancelar"},
		}
	case modeRunning:
		return []tui.Hint{{Key: "esc", Label: "aguarde"}}
	case modeResults:
		return []tui.Hint{
			{Key: "↑↓", Label: "resultado"},
			{Key: "enter/esc", Label: "fechar"},
		}
	}
	if m.width > 0 && m.width < 80 {
		return []tui.Hint{
			{Key: "u", Label: "atualizar todos"},
			{Key: "←→", Label: "tipo"},
			{Key: "p", Label: "push"},
			{Key: "d", Label: "limpar"},
			{Key: "esc", Label: "voltar"},
		}
	}
	return []tui.Hint{
		{Key: "u", Label: "atualizar todos"},
		{Key: "←→", Label: "tipo"},
		{Key: "↑↓", Label: "repositório"},
		{Key: "1-5", Label: "tipo direto"},
		{Key: "p", Label: "push"},
		{Key: "d", Label: "remover local"},
		{Key: "f", Label: "fetch"},
		{Key: "r", Label: "reler"},
		{Key: "esc", Label: "voltar"},
	}
}

// Meta alimenta a topbar.
func (m *Model) Meta() []string {
	stats := m.report.Stats()
	if stats.Repositories == 0 {
		return nil
	}
	return []string{
		strconv.Itoa(stats.Repositories) + " repos",
		strconv.Itoa(stats.Branches) + " branches",
	}
}

// Status informa a origem e a idade do retrato.
func (m *Model) Status() (string, lipgloss.TerminalColor) {
	th := m.deps.Theme
	if m.mode == modeRunning {
		return runningLabel(m.operation), th.Accent
	}
	if m.mode == modePick {
		return "escolha uma branch", th.Accent
	}
	if m.mode == modeConfirm {
		return "confirmação obrigatória", th.Warning
	}
	if m.mode == modeResults {
		stats := m.updateReport.Stats()
		return fmt.Sprintf("%d atualizados · %d ignorados · %d falhas",
			stats.Updated, stats.Skipped, stats.Failed), updateSummaryTone(th, stats)
	}
	if m.loading {
		return "varrendo ~/dev…", th.Accent
	}
	if m.err != nil {
		return firstLine(m.err.Error()), th.Danger
	}
	if m.feedback != "" {
		if m.feedbackErr {
			return m.feedback, th.Danger
		}
		return m.feedback, th.Success
	}
	if m.report.ScannedAt.IsZero() {
		return "refs remotos locais", th.Faint
	}
	if m.width > 0 && m.width < 80 {
		return fmt.Sprintf("tipo %d/%d", m.filter+1, filterCount), filterTone(th, m.filter)
	}
	return filterLabels[m.filter] + " · lido às " +
		m.report.ScannedAt.Local().Format("15:04:05") + " · refs locais", filterTone(th, m.filter)
}

func intersperse(items []string, separator string) []string {
	if len(items) < 2 {
		return items
	}
	out := make([]string, 0, len(items)*2-1)
	for i, item := range items {
		if i > 0 {
			out = append(out, separator)
		}
		out = append(out, item)
	}
	return out
}

func shortPath(path string) string {
	if path == "" {
		return "~/dev"
	}
	clean := filepath.ToSlash(path)
	parts := strings.Split(clean, "/")
	if len(parts) <= 4 {
		return clean
	}
	return "…/" + strings.Join(parts[len(parts)-3:], "/")
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}
