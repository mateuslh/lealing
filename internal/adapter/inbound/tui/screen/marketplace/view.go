package marketplace

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/component"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	coremarket "github.com/mateuslh/lealing/internal/core/marketplace"
)

// Pontos de quebra do layout. Cada um marca a largura ou a altura abaixo da
// qual um bloco deixa de caber com dignidade e sai de cena — encolher todos
// ao mesmo tempo produziria três colunas de reticências.
const (
	detailMinWidth  = 82
	detailMinHeight = 10
	searchMinHeight = 14
	listMinWidth    = 30
	// itemRows é a altura de um cartão com resumo; sem espaço, a lista cai
	// para uma linha por tool.
	itemRows = 2
)

func (m *Model) View(frame tui.Frame) string {
	th := m.deps.Theme
	if frame.Width < 24 || frame.Height < 6 {
		return th.Ghost.Render("janela pequena demais")
	}

	inner := max(frame.Width-4, 20)
	height := max(frame.Height-2, 4)

	blocks := []string{m.viewTabs(th, inner)}
	used := 1

	// A faixa de retorno fica logo abaixo das abas, e não no rodapé de um
	// painel: "instalada" e "origem cadastrada" são o desfecho da ação que o
	// usuário acabou de tomar, e um painel cheio empurraria essa linha para
	// fora do recorte justamente quando ela mais importa.
	if feedback := m.viewFeedback(th, inner); feedback != "" {
		blocks = append(blocks, feedback)
		used += lipgloss.Height(feedback)
	}

	if m.tab == tabTools && height >= searchMinHeight {
		field := m.viewSearchField(th, inner)
		blocks = append(blocks, "", field)
		used += lipgloss.Height(field) + 1
	}

	body := m.viewBody(th, inner, max(height-used-1, 3))
	blocks = append(blocks, "", body)

	content := lipgloss.NewStyle().
		Padding(1, 2).
		MaxWidth(frame.Width).
		MaxHeight(frame.Height).
		Render(lipgloss.JoinVertical(lipgloss.Left, blocks...))

	// Formulário e confirmação flutuam sobre a tela em vez de substituí-la:
	// a lista atrás é justamente o contexto que a pergunta exige.
	switch {
	case m.form != nil:
		return component.Overlay(content, m.viewForm(th, min(inner, 68)), frame.Width, frame.Height)
	case m.pendingRemoval != "":
		return component.Overlay(content, m.viewRemoval(th, min(inner, 60)), frame.Width, frame.Height)
	}
	return content
}

// --- Cabeçalho ---------------------------------------------------------

func (m *Model) viewTabs(th *theme.Theme, width int) string {
	tabs := []string{
		m.viewTab(th, tabTools, "✦", "tools", len(m.visible)),
		m.viewTab(th, tabSources, "⇄", "origens", len(m.origins)),
	}
	left := strings.Join(tabs, th.Ghost.Render("   "))

	right := ""
	if failed := m.failedSources(); failed > 0 {
		right = lipgloss.NewStyle().Foreground(th.Warning).
			Render(fmt.Sprintf("▲ %s indisponível", plural(failed, "origem", "origens")))
	} else if m.tab == tabTools && m.filter != filterAll {
		right = th.Ghost.Render("filtro: ") + lipgloss.NewStyle().Foreground(th.Accent).Render(m.filter.label())
	}

	rule := th.Divider.Render(strings.Repeat("╌", width))
	return lipgloss.JoinVertical(lipgloss.Left, component.Spread(left, right, width), rule)
}

func (m *Model) viewTab(th *theme.Theme, target tab, glyph, label string, count int) string {
	text := fmt.Sprintf("%s %s", glyph, label)
	if count > 0 {
		text += fmt.Sprintf(" %d", count)
	}
	if m.tab == target {
		return lipgloss.NewStyle().Foreground(th.OnPrimary).Background(th.Primary).
			Bold(true).Padding(0, 1).Render(text)
	}
	return th.Pill.Render(text)
}

func (m *Model) viewSearchField(th *theme.Theme, width int) string {
	prompt := lipgloss.NewStyle().Foreground(th.Primary).Bold(true).Render("❯ ")
	right := th.Ghost.Render("/ buscar · f filtrar")
	if m.searching || m.query.Value() != "" {
		right = th.Counter.Render(fmt.Sprintf("%d/%d", len(m.visible), len(m.catalog.Tools)))
	}

	m.query.Width = max(width-lipgloss.Width(prompt)-lipgloss.Width(right)-6, 8)
	field := component.Spread(prompt+m.query.View(), right, width-2)
	return component.Panel{
		Title: "busca", Glyph: "⌕", Accent: th.Primary,
		Focused: m.searching, Width: width, Height: 3,
	}.Render(th, field)
}

// --- Corpo -------------------------------------------------------------

func (m *Model) viewBody(th *theme.Theme, width, height int) string {
	if m.loading && len(m.catalog.Tools) == 0 && len(m.origins) == 0 {
		return component.Center(width, height, th.Dim.Render("consultando as origens de tools…"))
	}
	if m.tab == tabSources {
		return m.viewSplit(th, width, height, m.viewSourceList, m.viewSourceDetail)
	}
	if m.err != nil && len(m.catalog.Tools) == 0 {
		return component.Center(width, height,
			lipgloss.NewStyle().Foreground(th.Danger).Render("✗ "+firstLine(m.err.Error())),
			"",
			th.Ghost.Render("r tenta de novo · ⇄ origens mostra o que falhou"))
	}
	// A ficha aberta toma a largura toda: é leitura, e texto longo em meia
	// coluna é o que faz ninguém ler.
	if m.sheetOpen && len(m.visible) > 0 {
		return m.viewToolDetail(th, width, height)
	}
	return m.viewSplit(th, width, height, m.viewToolList, m.viewToolDetail)
}

type paneRenderer func(th *theme.Theme, width, height int) string

// viewSplit divide a área entre lista e detalhe, ou entrega tudo à lista
// quando não há largura para as duas colunas.
func (m *Model) viewSplit(th *theme.Theme, width, height int, list, detail paneRenderer) string {
	if width < detailMinWidth || height < detailMinHeight {
		return list(th, width, height)
	}
	listWidth := max(width*1/2, listMinWidth)
	detailWidth := width - listWidth - 1
	return lipgloss.JoinHorizontal(lipgloss.Top,
		list(th, listWidth, height),
		" ",
		detail(th, detailWidth, height),
	)
}

func (m *Model) viewToolList(th *theme.Theme, width, height int) string {
	if len(m.visible) == 0 {
		return component.Panel{
			Title: "catálogo", Glyph: "✦", Accent: th.Muted, Width: width, Height: height,
		}.Render(th, component.Center(width-2, height-2, th.Ghost.Render(m.emptyToolsMessage())))
	}

	inner := width - 2
	rows := height - 2
	perItem := 1
	if rows >= 6 {
		perItem = itemRows
	}
	visibleItems := max(rows/perItem, 1)
	start := scrollStart(m.toolCursor, visibleItems, len(m.visible))

	lines := make([]string, 0, rows)
	for index := start; index < min(start+visibleItems, len(m.visible)); index++ {
		lines = append(lines, m.viewToolItem(th, m.visible[index], index == m.toolCursor, inner, perItem)...)
	}

	footer := fmt.Sprintf("%d de %d", len(m.visible), len(m.catalog.Tools))
	if m.filter != filterAll {
		footer = m.filter.label() + " · " + footer
	}
	return component.Panel{
		Title: "catálogo", Glyph: "✦", Accent: th.Primary, Focused: !m.searching,
		Footer: footer, Width: width, Height: height,
	}.Render(th, strings.Join(lines, "\n"))
}

func (m *Model) viewToolItem(th *theme.Theme, listing coremarket.Listing, selected bool, width, rows int) []string {
	caret, name := "  ", th.Item.Render(listing.Name)
	if selected {
		caret = lipgloss.NewStyle().Foreground(th.Primary).Render("▎") + " "
		name = th.ItemSelected.Render(listing.Name)
	}
	glyph := lipgloss.NewStyle().Foreground(channelColor(th, listing.DistributionTier)).Render(toolGlyph(listing.Glyph))

	head := component.Spread(caret+glyph+" "+name, m.stateChip(th, listing), width)
	lines := []string{component.TruncateTail(head, width)}
	if rows < itemRows {
		return lines
	}

	meta := th.Ghost.Render(listing.Origin.Name) + th.Ghost.Render(" · ") +
		lipgloss.NewStyle().Foreground(channelColor(th, listing.DistributionTier)).Render(string(listing.DistributionTier)) +
		th.Ghost.Render(" · ") + th.ItemDesc.Render(listing.Summary)
	lines = append(lines, component.TruncateTail("    "+meta, width))
	return lines
}

// stateChip resume a relação entre o que está publicado e o que está
// instalado — a única informação que o usuário procura ao correr os olhos
// pela lista.
func (m *Model) stateChip(th *theme.Theme, listing coremarket.Listing) string {
	switch {
	case m.installing == listing.Ref():
		return lipgloss.NewStyle().Foreground(th.Accent).Render("◐ instalando")
	case listing.UpdateAvailable:
		return lipgloss.NewStyle().Foreground(th.Warning).
			Render("↑ " + listing.InstalledVersion + "→" + listing.Version)
	case listing.InstalledVersion != "":
		return lipgloss.NewStyle().Foreground(th.Success).Render("✓ " + listing.Version)
	default:
		return th.Ghost.Render(listing.Version)
	}
}

func (m *Model) viewToolDetail(th *theme.Theme, width, height int) string {
	listing, ok := m.currentTool()
	if !ok {
		return component.Panel{
			Title: "ficha", Glyph: "◆", Accent: th.Muted, Width: width, Height: height,
		}.Render(th, component.Center(width-2, height-2, th.Ghost.Render("selecione uma tool")))
	}

	inner, rows := width-2, height-2
	lines := m.toolSheet(th, listing, inner)

	// O scroll é fixado aqui, e não na tecla, porque só o render sabe quantas
	// linhas o texto ocupou nesta largura.
	m.sheetScroll = min(max(m.sheetScroll, 0), max(len(lines)-rows, 0))
	visible := lines[min(m.sheetScroll, len(lines)):]
	if len(visible) > rows {
		visible = visible[:rows]
	}

	footer := hintFor(listing)
	if len(lines) > rows {
		footer = fmt.Sprintf("%d%% · ⇞⇟ rolar · %s",
			scrollPercent(m.sheetScroll, len(lines), rows), footer)
	}
	title := "ficha"
	if m.sheetOpen {
		title = "ficha · ← lista"
	}
	return component.Panel{
		Title: title, Glyph: "◆", Accent: channelColor(th, listing.DistributionTier),
		Focused: m.sheetOpen, Footer: footer, Width: width, Height: height,
	}.Render(th, strings.Join(visible, "\n"))
}

// toolSheet monta a ficha completa da tool, em seções.
//
// É aqui que o marketplace deixa de ser uma lista de nomes: antes de instalar
// código de terceiros, o usuário precisa ler o que a tool faz, de onde ela
// vem e o que ela vai poder tocar na máquina — e isso não cabe num resumo de
// uma linha.
func (m *Model) toolSheet(th *theme.Theme, listing coremarket.Listing, width int) []string {
	lines := []string{
		th.Strong.Render(component.TruncateTail(toolGlyph(listing.Glyph)+"  "+listing.Name, width)),
		th.Ghost.Render(component.TruncateTail(listing.Ref()+" · "+listing.Version, width)),
		"",
		m.toolBadges(th, listing, width),
		"",
	}
	lines = append(lines, splitLines(wrap(th.Body, listing.Summary, width, 3))...)

	detail := strings.TrimSpace(listing.Detail)
	if detail == "" {
		detail = "Esta origem não publicou uma descrição longa para a tool."
	}
	lines = append(lines, m.sheetSection(th, "sobre", width,
		splitLines(wrap(th.ItemDesc, detail, width, 12))...)...)

	origin := listing.Origin.Title() + " · " + kindLabel(listing.Origin.Kind)
	if !listing.Origin.Trusted {
		origin += " · não verificada"
	}
	provenance := []component.Row{
		{Label: "origem", Value: origin},
		{Label: "publicador", Value: listing.Publisher},
		{Label: "canal", Value: string(listing.DistributionTier),
			Tone: channelColor(th, listing.DistributionTier)},
		{Label: "estado", Value: installState(listing), Tone: installTone(th, listing)},
	}
	if len(listing.Shadowed) > 0 {
		provenance = append(provenance, component.Row{
			Label: "atenção", Value: "mesmo ID em " + strings.Join(listing.Shadowed, ", "),
			Tone: th.Warning,
		})
	}
	lines = append(lines, m.sheetSection(th, "procedência", width,
		splitLines(component.FieldList{Rows: provenance, Width: width, LabelWidth: 12}.Render(th))...)...)

	requirements := []component.Row{
		{Label: "protocolo", Value: fmt.Sprintf("%d–%d", listing.Protocol.Min, listing.Protocol.Max)},
	}
	if listing.MinimumEngine != "" {
		requirements = append(requirements, component.Row{Label: "engine", Value: "≥ " + listing.MinimumEngine})
	}
	if platforms := artifactPlatforms(listing); platforms != "" {
		requirements = append(requirements, component.Row{Label: "plataformas", Value: platforms})
	}
	lines = append(lines, m.sheetSection(th, "requisitos", width,
		splitLines(component.FieldList{Rows: requirements, Width: width, LabelWidth: 12}.Render(th))...)...)

	// Os caminhos aparecem inteiros, não contados: "2 leituras" não deixa
	// ninguém decidir nada, e é exatamente nesta lista que mora o risco.
	permissions := pathRows("leitura", listing.Permissions.Filesystem.Read)
	permissions = append(permissions, pathRows("escrita", listing.Permissions.Filesystem.Write)...)
	permissions = append(permissions, []component.Row{
		{Label: "rede", Value: yesNo(listing.Permissions.Network),
			Tone: permissionTone(th, listing.Permissions.Network)},
		{Label: "subprocesso", Value: yesNo(listing.Permissions.Subprocess),
			Tone: permissionTone(th, listing.Permissions.Subprocess)},
		{Label: "risco", Value: listing.Risk, Tone: riskColor(th, listing.Risk)},
	}...)
	lines = append(lines, m.sheetSection(th, "permissões", width,
		splitLines(component.FieldList{Rows: permissions, Width: width, LabelWidth: 12}.Render(th))...)...)

	return lines
}

// sheetSection precede um bloco com seu título e uma linha de respiro.
func (m *Model) sheetSection(th *theme.Theme, title string, width int, body ...string) []string {
	label := th.PanelTitle.Render(strings.ToUpper(title))
	// A régua é medida com lipgloss.Width: len() conta bytes, e um título
	// acentuado como "PROCEDÊNCIA" desalinharia a linha inteira.
	fill := max(width-lipgloss.Width(label)-1, 0)
	header := label + " " + th.Divider.Render(strings.Repeat("╌", fill))
	return append([]string{"", component.TruncateTail(header, width)}, body...)
}

// toolBadges é a faixa de selos: canal preenchido, risco e estado.
func (m *Model) toolBadges(th *theme.Theme, listing coremarket.Listing, width int) string {
	channel := lipgloss.NewStyle().
		Foreground(th.OnPrimary).Background(channelColor(th, listing.DistributionTier)).
		Bold(true).Padding(0, 1).Render(string(listing.DistributionTier))

	// O risco vem nomeado, não em glyph: "·" ao lado de um selo preenchido
	// parece separador, e este é o campo que não pode ser lido pela metade.
	risk := lipgloss.NewStyle().Foreground(riskColor(th, listing.Risk)).
		Padding(0, 1).Render("risco " + listing.Risk)

	state := th.Pill.Render(installState(listing))
	if listing.UpdateAvailable {
		state = lipgloss.NewStyle().Foreground(th.OnPrimary).Background(th.Warning).
			Bold(true).Padding(0, 1).Render("atualização")
	}
	return component.TruncateTail(strings.Join([]string{channel, risk, state}, " "), width)
}

// --- Origens -----------------------------------------------------------

func (m *Model) viewSourceList(th *theme.Theme, width, height int) string {
	if len(m.origins) == 0 {
		return component.Panel{
			Title: "origens", Glyph: "⇄", Accent: th.Muted, Width: width, Height: height,
		}.Render(th, component.Center(width-2, height-2,
			th.Ghost.Render("nenhuma origem cadastrada"), "", th.Ghost.Render("a adiciona um repositório")))
	}

	inner := width - 2
	rows := height - 2
	perItem := 1
	if rows >= 8 {
		perItem = itemRows
	}
	visibleItems := max(rows/perItem, 1)
	start := scrollStart(m.sourceCursor, visibleItems, len(m.origins))

	lines := make([]string, 0, rows)
	for index := start; index < min(start+visibleItems, len(m.origins)); index++ {
		lines = append(lines, m.viewSourceItem(th, m.origins[index], index == m.sourceCursor, inner, perItem)...)
	}
	return component.Panel{
		Title: "origens", Glyph: "⇄", Accent: th.Secondary, Focused: true,
		Footer: "a adicionar · espaço ligar", Width: width, Height: height,
	}.Render(th, strings.Join(lines, "\n"))
}

func (m *Model) viewSourceItem(th *theme.Theme, origin coremarket.Origin, selected bool, width, rows int) []string {
	caret, name := "  ", th.Item.Render(origin.Title())
	if selected {
		caret = lipgloss.NewStyle().Foreground(th.Secondary).Render("▎") + " "
		name = th.ItemSelected.Render(origin.Title())
	}

	marker, markerTone := "○", th.Faint
	if origin.Enabled {
		marker, markerTone = "●", th.Success
	}
	status, hasStatus := m.sourceStatus(origin.Name)
	if origin.Enabled && hasStatus && status.Err != nil {
		marker, markerTone = "✗", th.Danger
	}

	right := th.Ghost.Render("desligada")
	switch {
	case origin.Enabled && hasStatus && status.Err != nil:
		right = lipgloss.NewStyle().Foreground(th.Danger).Render("falhou")
	case origin.Enabled && hasStatus:
		// "publicadas", e não "tools": o número conta o que a origem
		// publicou, e parte disso pode não valer para esta plataforma.
		right = th.Counter.Render(plural(status.Tools, "publicada", "publicadas"))
	case origin.Enabled:
		right = th.Ghost.Render("—")
	}

	head := component.Spread(
		caret+lipgloss.NewStyle().Foreground(markerTone).Render(marker)+" "+name, right, width)
	lines := []string{component.TruncateTail(head, width)}
	if rows < itemRows {
		return lines
	}

	scope := "própria"
	if origin.Builtin {
		scope = "embutida"
	}
	meta := th.Ghost.Render(string(origin.Kind) + " · " + scope + " · " + origin.Ref)
	lines = append(lines, component.TruncateTail("    "+meta, width))
	return lines
}

func (m *Model) viewSourceDetail(th *theme.Theme, width, height int) string {
	origin, ok := m.currentSource()
	if !ok {
		return component.Panel{
			Title: "origem", Glyph: "◆", Accent: th.Muted, Width: width, Height: height,
		}.Render(th, "")
	}

	inner := width - 2
	state, tone := "habilitada", th.Success
	if !origin.Enabled {
		state, tone = "desabilitada", th.Faint
	}
	scope := "adicionada por você"
	if origin.Builtin {
		scope = "embutida na engine"
	}
	confidence := "community — o canal declarado é rebaixado"
	if origin.Trusted {
		confidence = "verificada — pode publicar official e verified"
	}

	rows := []component.Row{
		{Label: "estado", Value: state, Tone: tone},
		{Label: "tipo", Value: kindLabel(origin.Kind)},
		{Label: "escopo", Value: scope},
		{Label: "confiança", Value: confidence},
		{Label: "prioridade", Value: fmt.Sprintf("%d", origin.Priority+1)},
	}
	if status, hasStatus := m.sourceStatus(origin.Name); hasStatus && status.Err == nil {
		rows = append(rows, component.Row{
			Label: "publicadas", Value: fmt.Sprintf("%d entradas no índice", status.Tools)})
	}

	blocks := []string{
		th.Strong.Render(component.TruncateTail(origin.Title(), inner)),
		th.Ghost.Render(component.TruncateTail(origin.Name, inner)),
		"",
		wrap(th.ItemDesc, origin.Ref, inner, 2),
		"",
		component.FieldList{Rows: rows, Width: inner, LabelWidth: 12}.Render(th),
	}
	if status, hasStatus := m.sourceStatus(origin.Name); hasStatus && status.Err != nil {
		blocks = append(blocks, "", wrap(
			lipgloss.NewStyle().Foreground(th.Danger), "✗ "+firstLine(status.Err.Error()), inner, 3))
	}
	footer := "espaço ligar/desligar"
	if !origin.Builtin {
		footer += " · d remover"
	}
	return component.Panel{
		Title: "origem", Glyph: "◆", Accent: th.Secondary,
		Footer: footer, Width: width, Height: height,
	}.Render(th, strings.Join(blocks, "\n"))
}

// --- Sobreposições -----------------------------------------------------

func (m *Model) viewForm(th *theme.Theme, width int) string {
	inner := width - 2
	for index := range m.form.inputs {
		m.form.inputs[index].Width = max(inner-2, 8)
	}

	blocks := []string{
		th.Dim.Render(component.TruncateTail(
			"Um repositório é um index.json publicado por HTTPS ou um diretório no disco.", inner)),
		"",
		m.viewFormField(th, "endereço do índice", fieldRef, inner),
		"",
		m.viewFormField(th, "nome", fieldName, inner),
	}
	if m.form.err != nil {
		blocks = append(blocks, "", wrap(
			lipgloss.NewStyle().Foreground(th.Danger), "✗ "+firstLine(m.form.err.Error()), inner, 2))
	}
	blocks = append(blocks, "", th.Ghost.Render(
		component.TruncateTail("↹ alterna campo · ↵ cadastrar · esc cancelar", inner)))

	content := strings.Join(blocks, "\n")
	return component.Panel{
		Title: "adicionar origem", Glyph: "＋", Accent: th.Secondary, Focused: true,
		Width: width, Height: lipgloss.Height(content) + 2,
	}.Render(th, content)
}

func (m *Model) viewFormField(th *theme.Theme, label string, field formField, width int) string {
	style := th.Ghost
	prefix := "  "
	if m.form.focus == field {
		style = th.Body
		prefix = th.Cursor.Render("› ")
	}
	return lipgloss.JoinVertical(lipgloss.Left,
		style.Render(label),
		component.TruncateTail(prefix+m.form.inputs[field].View(), width),
	)
}

func (m *Model) viewRemoval(th *theme.Theme, width int) string {
	inner := width - 2
	content := lipgloss.JoinVertical(lipgloss.Left,
		th.Strong.Render(component.TruncateTail("Remover a origem "+m.pendingRemoval+"?", inner)),
		"",
		th.Dim.Render(component.TruncateTail(
			"As tools já instaladas continuam funcionando; elas só deixam de receber atualizações desta origem.", inner)),
	)
	return component.Panel{
		Title: "confirmar", Glyph: "!", Accent: th.Warning, Focused: true,
		Footer: "y remover · n cancelar", Width: width, Height: lipgloss.Height(content) + 2,
	}.Render(th, content)
}

// viewFeedback devolve a última mensagem da tela, já colorida pelo tom.
func (m *Model) viewFeedback(th *theme.Theme, width int) string {
	switch {
	case m.installing != "":
		return lipgloss.NewStyle().Foreground(th.Accent).Render(
			component.TruncateTail("◐ baixando, conferindo o checksum e validando o manifest…", width))
	case m.err != nil:
		return wrap(lipgloss.NewStyle().Foreground(th.Danger), "✗ "+firstLine(m.err.Error()), width, 3)
	case m.message != "" && m.success:
		return wrap(lipgloss.NewStyle().Foreground(th.Success), "✓ "+m.message, width, 2)
	case m.message != "":
		return wrap(lipgloss.NewStyle().Foreground(th.Warning), "▲ "+m.message, width, 2)
	}
	return ""
}

// --- Barra de status ---------------------------------------------------

func (m *Model) Hints() []tui.Hint {
	switch {
	case m.form != nil:
		return []tui.Hint{{Key: "↵", Label: "cadastrar"}, {Key: "↹", Label: "campo"}, {Key: "esc", Label: "cancelar"}}
	case m.pendingRemoval != "":
		return []tui.Hint{{Key: "y", Label: "remover"}, {Key: "n", Label: "cancelar"}}
	case m.searching:
		return []tui.Hint{{Key: "↵", Label: "aplicar"}, {Key: "esc", Label: "limpar"}}
	case m.sheetOpen:
		return []tui.Hint{
			{Key: "←", Label: "lista"}, {Key: "⇞⇟", Label: "rolar"},
			{Key: "↵", Label: "instalar/atualizar"}, {Key: "esc", Label: "voltar"},
		}
	case m.tab == tabSources:
		return []tui.Hint{
			{Key: "↑↓", Label: "selecionar"}, {Key: "espaço", Label: "ligar/desligar"},
			{Key: "a", Label: "adicionar"}, {Key: "d", Label: "remover"},
			{Key: "↹", Label: "tools"}, {Key: "r", Label: "recarregar"}, {Key: "esc", Label: "voltar"},
		}
	}
	return []tui.Hint{
		{Key: "↑↓", Label: "selecionar"}, {Key: "→", Label: "ficha"},
		{Key: "↵", Label: "instalar/atualizar"}, {Key: "/", Label: "buscar"},
		{Key: "f", Label: "filtrar"}, {Key: "↹", Label: "origens"},
		{Key: "a", Label: "adicionar origem"}, {Key: "r", Label: "recarregar"},
		{Key: "esc", Label: "voltar"},
	}
}

func (m *Model) Meta() []string {
	meta := []string{plural(len(m.origins), "origem", "origens")}
	if tool, ok := m.currentTool(); ok && m.tab == tabTools {
		meta = append(meta, string(tool.DistributionTier), tool.Version)
	}
	return meta
}

func (m *Model) Status() (string, lipgloss.TerminalColor) {
	th := m.deps.Theme
	switch {
	case m.installing != "":
		return "instalando " + m.installing, th.Warning
	case m.err != nil:
		return "falha no marketplace", th.Danger
	case m.loading:
		return "consultando origens", th.Secondary
	case m.failedSources() > 0:
		return fmt.Sprintf("%s · %s fora do ar",
			plural(len(m.catalog.Tools), "tool", "tools"),
			plural(m.failedSources(), "origem", "origens")), th.Warning
	default:
		return fmt.Sprintf("%s em %s",
			plural(len(m.catalog.Tools), "tool", "tools"),
			plural(len(m.origins), "origem", "origens")), th.Success
	}
}

var (
	_ interface {
		Status() (string, lipgloss.TerminalColor)
	} = (*Model)(nil)
	_ interface{ Meta() []string }  = (*Model)(nil)
	_ interface{ Capturing() bool } = (*Model)(nil)
)

// --- Auxiliares --------------------------------------------------------

func (m *Model) failedSources() int {
	failed := 0
	for _, status := range m.catalog.Sources {
		if status.Err != nil {
			failed++
		}
	}
	return failed
}

func (m *Model) emptyToolsMessage() string {
	switch {
	case m.query.Value() != "":
		return "nenhuma tool corresponde a “" + m.query.Value() + "”"
	case m.filter != filterAll:
		return "nenhuma tool " + m.filter.label()
	case m.failedSources() > 0:
		return "as origens habilitadas não responderam"
	default:
		return "nenhuma tool compatível com esta plataforma"
	}
}

// scrollStart mantém o cursor dentro da janela visível sem deslizar a lista
// enquanto ele ainda cabe nela.
func scrollStart(cursor, visible, total int) int {
	if cursor < visible {
		return 0
	}
	return min(cursor-visible+1, max(total-visible, 0))
}

func channelColor(th *theme.Theme, channel coremarket.Channel) lipgloss.TerminalColor {
	switch channel {
	case coremarket.ChannelOfficial:
		return th.Primary
	case coremarket.ChannelVerified:
		return th.Accent
	default:
		return th.Muted
	}
}

func riskColor(th *theme.Theme, risk string) lipgloss.TerminalColor {
	switch risk {
	case "destructive":
		return th.Danger
	case "caution":
		return th.Warning
	default:
		return th.Success
	}
}

func kindLabel(kind coremarket.OriginKind) string {
	if kind == coremarket.OriginLocal {
		return "diretório local"
	}
	return "índice remoto"
}

func hintFor(listing coremarket.Listing) string {
	switch {
	case listing.UpdateAvailable:
		return "↵ atualizar"
	case listing.InstalledVersion != "":
		return "instalada"
	default:
		return "↵ instalar"
	}
}

// installState resume a relação entre publicado e instalado.
func installState(listing coremarket.Listing) string {
	switch {
	case listing.UpdateAvailable:
		return listing.InstalledVersion + " → " + listing.Version
	case listing.InstalledVersion != "":
		return "instalada em " + listing.InstalledVersion
	default:
		return "não instalada"
	}
}

func installTone(th *theme.Theme, listing coremarket.Listing) lipgloss.TerminalColor {
	switch {
	case listing.UpdateAvailable:
		return th.Warning
	case listing.InstalledVersion != "":
		return th.Success
	default:
		return th.Faint
	}
}

// permissionTone acende só o que foi concedido: uma permissão negada é o
// estado normal e não deve competir por atenção com uma concedida.
func permissionTone(th *theme.Theme, granted bool) lipgloss.TerminalColor {
	if granted {
		return th.Warning
	}
	return nil
}

// pathRows lista um caminho por linha, com o rótulo só na primeira.
//
// Juntar tudo com vírgula fazia a lista ser cortada no meio de um caminho —
// e é exatamente aqui, ao ver o que a tool vai poder ler, que o usuário
// decide se instala.
func pathRows(label string, paths []string) []component.Row {
	if len(paths) == 0 {
		// O travessão marca a ausência de permissão de forma inequívoca, o
		// que uma linha vazia não faria.
		return []component.Row{{Label: label, Value: "—"}}
	}
	rows := make([]component.Row, len(paths))
	for index, path := range paths {
		rows[index] = component.Row{Value: path}
		if index == 0 {
			rows[index].Label = label
		}
	}
	return rows
}

func artifactPlatforms(listing coremarket.Listing) string {
	platforms := make([]string, 0, len(listing.Artifacts))
	for _, artifact := range listing.Artifacts {
		platforms = append(platforms, artifact.Platform)
	}
	sort.Strings(platforms)
	return strings.Join(platforms, ", ")
}

// scrollPercent traduz a posição da janela em porcentagem lida.
func scrollPercent(offset, total, visible int) int {
	maximum := total - visible
	if maximum <= 0 {
		return 100
	}
	return min(offset*100/maximum, 100)
}

func splitLines(block string) []string {
	if block == "" {
		return nil
	}
	return strings.Split(block, "\n")
}

func permissionLabel(read, write int) string {
	return fmt.Sprintf("%d leitura · %d escrita", read, write)
}

func yesNo(value bool) string {
	if value {
		return "permitido"
	}
	return "não"
}

func toolGlyph(glyph string) string {
	if strings.TrimSpace(glyph) == "" {
		return "◫"
	}
	return glyph
}

func plural(count int, singular, many string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, singular)
	}
	return fmt.Sprintf("%d %s", count, many)
}

func firstLine(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	return line
}

// wrap quebra o texto em até maxLines linhas, sem depender de quantas
// palavras cabem: um resumo cortado no meio de uma frase ainda informa mais
// que uma linha só de reticências.
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
		// O excedente é jogado na última linha visível e cortado ali: as
		// reticências sinalizam que o texto continua, coisa que simplesmente
		// descartar as linhas de baixo não faria.
		lines = append(lines[:maxLines-1], strings.Join(lines[maxLines-1:], " "))
	}
	for index, line := range lines {
		lines[index] = component.TruncateTail(line, width)
	}
	return style.Render(strings.Join(lines, "\n"))
}
