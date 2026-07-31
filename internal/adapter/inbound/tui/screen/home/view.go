package home

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/component"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	"github.com/mateuslh/lealing/internal/core/domain"
)

// Constantes de layout. São os pontos de quebra do design responsivo: cada
// uma marca a largura abaixo da qual um elemento deixa de caber com dignidade
// e precisa sair ou encolher.
const (
	sidebarWidth    = 26
	sidebarMinTotal = 94 // abaixo disso a sidebar rouba espaço demais do miolo
	panelMinWidth   = 34
	heroMinHeight   = 28 // abaixo disso busca e catálogo têm prioridade
	spectrumMinRows = 20
)

// View implementa tui.Screen.
func (m *Model) View(f tui.Frame) string {
	th := m.deps.Theme
	if f.Width < 24 || f.Height < 6 {
		return th.Ghost.Render("janela pequena demais")
	}

	innerW, innerH := f.Width-2, f.Height-2
	sidebarW := 0
	mainW := innerW
	if innerW >= sidebarMinTotal && len(m.categories) > 0 {
		sidebarW = sidebarWidth
		mainW = innerW - sidebarW - 1 // -1 do divisor vertical
	}

	main := m.viewMain(th, mainW, innerH)
	body := main
	if sidebarW == 0 {
		return component.ColorFrame{
			Title: "LEALING / HOME", Width: f.Width, Height: f.Height,
		}.Render(th, body)
	}

	side := m.viewSidebar(th, sidebarW, innerH)
	divider := m.viewDivider(th, innerH)
	body = lipgloss.JoinHorizontal(lipgloss.Top, side, divider, main)
	return component.ColorFrame{
		Title: "LEALING / HOME", Width: f.Width, Height: f.Height,
	}.Render(th, body)
}

// viewDivider desenha a linha vertical entre sidebar e conteúdo, esmaecendo
// nas pontas para não parecer uma cicatriz cortando a tela.
func (m *Model) viewDivider(th *theme.Theme, height int) string {
	lines := make([]string, height)
	for i := range lines {
		style := th.Divider
		if i == 0 || i == height-1 {
			style = th.Ghost
		}
		lines[i] = style.Render("│")
	}
	return strings.Join(lines, "\n")
}

// --- Sidebar -----------------------------------------------------------

func (m *Model) viewSidebar(th *theme.Theme, width, height int) string {
	inner := width - 2
	pad := lipgloss.NewStyle().PaddingLeft(1).PaddingRight(1)

	footerText := fmt.Sprintf("%d tools no total", m.highlights.TotalTools)
	if category, ok := m.selectedCategory(); ok {
		count := m.filterPage.Total
		if m.filterLoading {
			count = m.categoryCount(category.ID)
		}
		footerText = fmt.Sprintf("%d em %s", count, category.Name)
	}
	footer := th.Ghost.Render(component.TruncateTail(footerText, inner))

	// 3 linhas de cabeçalho (título + régua + respiro) e 2 de rodapé.
	listH := max(height-6, 1)
	list := component.Sidebar{
		Items:    m.categorySidebarItems(),
		Selected: m.cursor[zoneSidebar],
		Focused:  m.focus == zoneSidebar && !m.searching,
		Width:    inner,
		Height:   listH,
	}.Render(th)

	body := lipgloss.JoinVertical(lipgloss.Left,
		th.PanelTitle.Render("CATEGORIAS"),
		th.Divider.Render(strings.Repeat("╌", inner)),
		"",
		list,
	)

	// Empurra o rodapé para a base da coluna.
	filled := lipgloss.NewStyle().Width(inner).Height(height - 2).Render(body)
	return pad.Render(lipgloss.JoinVertical(lipgloss.Left, filled, footer))
}

func (m *Model) categorySidebarItems() []component.SidebarItem {
	items := make([]component.SidebarItem, len(m.categories))
	for i, category := range m.categories {
		items[i] = component.SidebarItem{
			Name: category.Name, Glyph: category.Glyph,
			Count: category.Count, Accent: category.Accent,
		}
	}
	return items
}

// --- Conteúdo principal ------------------------------------------------

func (m *Model) viewMain(th *theme.Theme, width, height int) string {
	switch {
	case m.loading && m.highlights.TotalTools == 0:
		return m.viewCentered(th, width, height, th.Dim.Render("carregando catálogo…"))
	case m.err != nil && m.highlights.TotalTools == 0:
		msg := lipgloss.NewStyle().Foreground(th.Danger).Render("✗ " + m.err.Error())
		return m.viewCentered(th, width, height, msg)
	case m.searching:
		return m.viewSearch(th, width, height)
	case m.hasFilter():
		return m.viewFilter(th, width, height)
	default:
		return m.viewBrowse(th, width, height)
	}
}

// viewCentered centraliza um bloco no espaço disponível.
func (m *Model) viewCentered(_ *theme.Theme, width, height int, content string) string {
	return component.Center(width, height, content)
}

// viewBrowse é a home propriamente dita: hero, espectro e destaques.
func (m *Model) viewBrowse(th *theme.Theme, width, height int) string {
	pad := lipgloss.NewStyle().Padding(0, 2)
	inner := max(width-4, 10)

	blocks := []string{""}
	used := 1

	if height >= heroMinHeight {
		hero := m.viewHero(th, inner, height)
		blocks = append(blocks, hero, "")
		used += lipgloss.Height(hero) + 1
	}

	search := m.viewSearchField(th, inner, false)
	blocks = append(blocks, search)
	used += lipgloss.Height(search)

	if height >= spectrumMinRows && len(m.categories) > 2 {
		spectrum := m.viewSpectrum(th, inner)
		blocks = append(blocks, "", spectrum)
		used += lipgloss.Height(spectrum) + 1
	}

	panelsH := max(height-used-1, 6)
	blocks = append(blocks, "", m.viewPanels(th, inner, panelsH))

	body := lipgloss.JoinVertical(lipgloss.Left, blocks...)
	return pad.Render(lipgloss.NewStyle().Width(inner).MaxHeight(height).Render(body))
}

// viewHero monta o bloco de abertura.
func (m *Model) viewHero(th *theme.Theme, width, height int) string {
	logoW := width
	if height < heroMinHeight {
		// Força a variante média quando não há altura para o wordmark cheio.
		logoW = min(logoW, 40)
	}

	stats := []string{
		fmt.Sprintf("%d tools", m.highlights.TotalTools),
		fmt.Sprintf("%d categorias", m.highlights.TotalCategories),
	}
	if n := len(m.highlights.Favorites); n > 0 {
		stats = append(stats, fmt.Sprintf("%d favoritas", n))
	}

	hero := component.Hero{
		Greeting: greeting(m.now(), m.user),
		Tagline:  "seu centro de comando no terminal",
		Stats:    stats,
	}

	// Renderiza o logo na largura calculada, mas centraliza no espaço cheio.
	block := hero.Render(th, min(logoW, width))
	return lipgloss.NewStyle().Width(width).Align(lipgloss.Center).Render(block)
}

// viewSpectrum desenha a composição do catálogo e sua legenda.
func (m *Model) viewSpectrum(th *theme.Theme, width int) string {
	// O primeiro item é "Todas": incluí-lo dobraria visualmente o total do
	// catálogo, então só as categorias reais entram na composição.
	bar := component.SpectrumBar{Items: m.categories[1:], Width: width}
	return lipgloss.JoinVertical(lipgloss.Left, bar.Render(th), "", bar.Legend(th))
}

// viewFilter substitui os destaques por uma lista única. Manter favoritas
// e recentes na tela faria a sidebar parecer apenas decorativa, pois ainda
// haveria itens fora do filtro visíveis.
func (m *Model) viewFilter(th *theme.Theme, width, height int) string {
	pad := lipgloss.NewStyle().Padding(0, 2)
	inner := max(width-4, 10)

	search := m.viewSearchField(th, inner, false)
	panelH := max(height-lipgloss.Height(search)-2, 3)
	empty := "nenhuma tool corresponde ao filtro"
	if m.filterLoading {
		empty = "aplicando filtro…"
	}

	list := component.ToolList{
		Items:      m.filterPage.Items,
		Selected:   m.cursor[zoneSuggested],
		Focused:    m.focus == zoneSuggested,
		Width:      inner - 2,
		Height:     panelH - 2,
		Categories: m.catByID,
		Detailed:   panelH >= 8,
		Empty:      empty,
	}.Render(th)

	footer := fmt.Sprintf("%d tools", m.filterPage.Total)
	if m.filterLoading {
		footer = "carregando"
	}
	if m.filterPage.HasMore() {
		footer = fmt.Sprintf("%d de %d", m.filterPage.Len(), m.filterPage.Total)
	}
	title, glyph, accent := m.filterPresentation(th)
	panel := component.Panel{
		Title:   title,
		Glyph:   glyph,
		Accent:  accent,
		Focused: m.focus == zoneSuggested,
		Footer:  footer,
		Width:   inner,
		Height:  panelH,
	}.Render(th, list)

	body := lipgloss.JoinVertical(lipgloss.Left, "", search, "", panel)
	return pad.Render(lipgloss.NewStyle().MaxHeight(height).Render(body))
}

func (m *Model) filterPresentation(th *theme.Theme) (string, string, lipgloss.TerminalColor) {
	category, hasCategory := m.selectedCategory()
	if hasCategory {
		return category.Name, category.Glyph, th.SpectrumAt(category.Accent)
	}
	return "tools", "⌘", th.Primary
}

func (m *Model) filterLabel() string {
	category, hasCategory := m.selectedCategory()
	if hasCategory {
		return category.Name
	}
	return "todas"
}

func (m *Model) activeFilterCount() int {
	category, hasCategory := m.selectedCategory()
	if hasCategory {
		return m.categoryCount(category.ID)
	}
	return m.highlights.TotalTools
}

// panelSpec descreve um painel de destaque antes de ser renderizado.
type panelSpec struct {
	title string
	glyph string
	zone  zone
	items []domain.Match
	empty string
	tone  lipgloss.TerminalColor
}

// viewPanels distribui os três painéis de destaque no espaço disponível.
//
// O número de colunas cai conforme a largura: três lado a lado é o ideal,
// mas empilhar é sempre melhor do que espremer um painel até o nome da tool
// virar reticências.
func (m *Model) viewPanels(th *theme.Theme, width, height int) string {
	specs := []panelSpec{
		{"favoritas", "★", zoneFavorites, m.highlights.Favorites,
			"marque com f", th.Warning},
		{"recentes", "⟳", zoneRecent, m.highlights.Recent,
			"nada executado ainda", th.Accent},
		{"sugeridas", "✦", zoneSuggested, m.highlights.Suggested,
			"catálogo vazio", th.Secondary},
	}

	cols := min(max(width/panelMinWidth, 1), len(specs))
	rows := (len(specs) + cols - 1) / cols

	// Um painel abaixo de minPanelHeight não mostra nem um item, então não
	// vale a moldura. Quando o espaço vertical não comporta todas as linhas,
	// as excedentes são descartadas em vez de transbordarem para fora do
	// frame — o que arrancaria a borda inferior do último painel.
	const minPanelHeight = 5
	if height < minPanelHeight {
		return ""
	}
	// Cada linha extra consome uma linha de respiro entre painéis.
	maxRows := max((height+1)/(minPanelHeight+1), 1)
	if maxRows < rows {
		// Vai sobrar painel de fora. Nesse caso, e só nesse, os que têm
		// conteúdo passam à frente dos vazios: em uma janela apertada, ver
		// "sugeridas" cheio vale mais do que preservar a posição de um
		// painel que só diria "marque com f".
		rows = maxRows
		sort.SliceStable(specs, func(i, j int) bool {
			return len(specs[i].items) > 0 && len(specs[j].items) == 0
		})
	}
	specs = specs[:min(len(specs), rows*cols)]
	rowH := (height - (rows - 1)) / rows

	renderedRows := make([]string, 0, rows)
	for r := range rows {
		start := r * cols
		end := min(start+cols, len(specs))
		if start >= end {
			break
		}
		n := end - start

		// gaps são as colunas de respiro entre painéis; o último painel
		// absorve o resto da divisão para a linha fechar exatamente em width.
		gaps := n - 1
		colW := (width - gaps) / n

		blocks := make([]string, 0, n+gaps)
		for i := start; i < end; i++ {
			if i > start {
				blocks = append(blocks, " ")
			}
			w := colW
			if i == end-1 {
				w = width - gaps - colW*(n-1)
			}
			blocks = append(blocks, m.viewPanel(th, specs[i], w, rowH))
		}
		renderedRows = append(renderedRows, lipgloss.JoinHorizontal(lipgloss.Top, blocks...))
	}

	return strings.Join(renderedRows, "\n\n")
}

// viewPanel renderiza um painel de destaque com sua lista dentro.
func (m *Model) viewPanel(th *theme.Theme, spec panelSpec, width, height int) string {
	focused := m.focus == spec.zone && !m.searching

	// Cada item ocupa duas linhas (nome + resumo) quando há altura folgada.
	innerH := height - 2
	detailed := innerH >= 6

	list := component.ToolList{
		Items:      spec.items,
		Selected:   m.cursor[spec.zone],
		Focused:    focused,
		Width:      width - 2,
		Height:     innerH,
		Categories: m.catByID,
		Detailed:   detailed,
		Empty:      spec.empty,
	}.Render(th)

	footer := ""
	if n := len(spec.items); n > 0 {
		footer = fmt.Sprintf("%d", n)
	}

	return component.Panel{
		Title:   spec.title,
		Glyph:   spec.glyph,
		Accent:  spec.tone,
		Focused: focused,
		Footer:  footer,
		Width:   width,
		Height:  height,
	}.Render(th, list)
}

// --- Modo busca --------------------------------------------------------

func (m *Model) viewSearch(th *theme.Theme, width, height int) string {
	pad := lipgloss.NewStyle().Padding(0, 2)
	inner := max(width-4, 10)

	box := m.viewSearchField(th, inner, true)

	resultsH := max(height-lipgloss.Height(box)-2, 5)
	results := component.ToolList{
		Items:      m.results.Items,
		Selected:   m.resultSel,
		Focused:    true,
		Width:      inner - 2,
		Height:     resultsH - 2,
		Categories: m.catByID,
		Detailed:   true,
		Empty:      m.searchEmptyMessage(),
	}.Render(th)

	footer := ""
	if m.results.HasMore() {
		footer = fmt.Sprintf("mostrando %d de %d", m.results.Len(), m.results.Total)
	}

	list := component.Panel{
		Title:  "resultados",
		Glyph:  "≡",
		Accent: th.Muted,
		Footer: footer,
		Width:  inner,
		Height: resultsH,
	}.Render(th, results)

	body := lipgloss.JoinVertical(lipgloss.Left, "", box, "", list)
	return pad.Render(lipgloss.NewStyle().MaxHeight(height).Render(body))
}

// viewSearchField mantém a command palette visível mesmo fora do modo de
// digitação. O foco pode chegar aqui pelas setas; Enter passa a capturar o
// teclado e troca o conteúdo abaixo pelos resultados.
func (m *Model) viewSearchField(th *theme.Theme, width int, editing bool) string {
	prompt := lipgloss.NewStyle().Foreground(th.Primary).Bold(true).Render("❯ ")
	right := th.Ghost.Render("↵ buscar")
	if editing && m.input.Value() != "" {
		total := m.highlights.TotalTools
		if m.hasFilter() {
			total = m.activeFilterCount()
		}
		right = th.Counter.Render(fmt.Sprintf("%d/%d", m.results.Total, total))
	}

	rightW := lipgloss.Width(right)
	m.input.Width = max(width-lipgloss.Width(prompt)-rightW-5, 8)
	field := component.Spread(prompt+m.input.View(), right, width-2)

	return component.Panel{
		Title:   "command palette",
		Glyph:   "⌕",
		Accent:  th.Primary,
		Focused: editing || m.focus == zoneSearch,
		Width:   width,
		Height:  3,
	}.Render(th, field)
}

// searchEmptyMessage distingue "ainda não digitou" de "não achei nada".
func (m *Model) searchEmptyMessage() string {
	if m.input.Value() == "" {
		if m.hasFilter() {
			return "digite para buscar em " + m.filterLabel()
		}
		return "digite para buscar entre todas as tools"
	}
	return "nenhuma tool corresponde a “" + m.input.Value() + "”"
}

// --- Barra de status ---------------------------------------------------

// Hints implementa tui.Screen, em ordem de importância decrescente.
func (m *Model) Hints() []tui.Hint {
	if m.searching {
		return []tui.Hint{
			{Key: "↑↓", Label: "navegar"},
			{Key: "↵", Label: "abrir"},
			{Key: "^f", Label: "favoritar"},
			{Key: "esc", Label: "voltar"},
		}
	}
	if m.focus == zoneSearch {
		return []tui.Hint{
			{Key: "↵", Label: "digitar"},
			{Key: "↓", Label: "catálogo"},
			{Key: "←", Label: "categorias"},
			{Key: "/", Label: "buscar"},
			{Key: "esc", Label: "voltar"},
		}
	}
	return []tui.Hint{
		{Key: "/", Label: "buscar"},
		{Key: "↵", Label: "abrir"},
		{Key: "f", Label: "favoritar"},
		{Key: "↹", Label: "painel"},
		{Key: "↑↓←→", Label: "navegar"},
		{Key: "r", Label: "recarregar"},
		{Key: "esc", Label: "voltar"},
	}
}

// Status devolve a mensagem efêmera e seu tom, consumidos pelo App.
func (m *Model) Status() (string, lipgloss.TerminalColor) {
	th := m.deps.Theme
	if m.toast.text == "" {
		if t, ok := m.selectedTool(); ok && !m.searching {
			return t.Summary, th.Faint
		}
		return "", nil
	}
	switch m.toast.tone {
	case toneSuccess:
		return "✓ " + m.toast.text, th.Success
	case toneWarn:
		return "▲ " + m.toast.text, th.Warning
	case toneError:
		return "✗ " + m.toast.text, th.Danger
	default:
		return m.toast.text, th.Muted
	}
}

// Meta devolve os números exibidos na topbar.
func (m *Model) Meta() []string {
	if m.highlights.TotalTools == 0 {
		return nil
	}
	return []string{
		fmt.Sprintf("%d tools", m.highlights.TotalTools),
		fmt.Sprintf("%d categorias", m.highlights.TotalCategories),
	}
}

// Capturing informa ao App que o teclado está sendo consumido por um campo
// de texto — sem isso, teclar "q" durante a busca fecharia o programa.
func (m *Model) Capturing() bool { return m.searching }

// Compile-time: a home precisa continuar satisfazendo os contratos opcionais
// que o App consulta por type assertion.
var (
	_ interface {
		Status() (string, lipgloss.TerminalColor)
	} = (*Model)(nil)
	_ interface{ Meta() []string }  = (*Model)(nil)
	_ interface{ Capturing() bool } = (*Model)(nil)
)
