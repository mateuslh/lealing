package home

import (
	"errors"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	requirementsscreen "github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/requirements"
	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/port/inbound"
	"github.com/mateuslh/lealing/internal/core/selfupdate"
)

// Limites de carga da home.
const (
	// highlightLimit é quantos itens cada painel de destaque pede. Sobra
	// deliberada: a lista recorta pela altura real do painel, então pedir a
	// mais evita um painel alto com espaço vazio no rodapé.
	highlightLimit = 12
	// recentLimit mantém o painel curto e escaneável mesmo quando há espaço
	// vertical sobrando; o histórico completo continua disponível na busca.
	recentLimit = 3
	// searchPageSize é o teto de resultados por busca. A lista rola, mas
	// pedir mais que isso só desperdiça ranqueamento que ninguém vai ler.
	searchPageSize = 60
)

// Update implementa tui.Screen.
func (m *Model) Update(msg tea.Msg) (tui.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.normalizeFocus()
		return m, nil

	case catalogMsg:
		m.loading = false
		if msg.err != nil {
			m.err = msg.err
			m.notify("falha ao carregar o catálogo: "+msg.err.Error(), toneError)
			return m, nil
		}
		m.err = nil
		m.highlights = msg.highlights
		m.redistributeRecent()
		// Categorias sem tool saem da navegação, mas continuam no mapa de
		// consulta: uma tool pode referenciar uma categoria que acabou de
		// esvaziar por um filtro, e a lista ainda precisa do glyph e da cor.
		m.categories = m.categories[:0]
		m.catByID = make(map[domain.CategoryID]domain.Category, len(msg.categories))
		m.categories = append(m.categories, inbound.CategoryView{
			Category: domain.Category{
				Name:        "Todas",
				Glyph:       "⌘",
				Accent:      0,
				Description: "todo o catálogo",
			},
			Count: msg.highlights.TotalTools,
		})
		for _, c := range msg.categories {
			m.catByID[c.ID] = c.Category
			if c.Count > 0 {
				m.categories = append(m.categories, c)
			}
		}
		m.clampCursors()
		if m.hasFilter() {
			m.filterGen++
			m.filterLoading = true
			return m, m.loadFilter()
		}
		return m, nil

	case marketplaceMsg:
		m.marketplaceLoading = false
		m.marketplaceErr = msg.err
		if msg.err == nil {
			m.marketplaceCatalog = msg.catalog
		}
		return m, nil

	case updateMsg:
		m.updateStatus = msg.status
		// A checagem roda em paralelo com o catálogo (veja Init): a home já
		// está aberta e usável quando o aviso aparece, nunca antes dela.
		if msg.status.State == selfupdate.StateOutdated && !m.updateSkipped(msg.status.Latest.Tag) {
			m.updatePrompt = true
			m.updatePromptCursor = updatePromptDefault
		}
		return m, nil

	case updateSkippedMsg:
		if msg.err != nil {
			m.notify("não deu para lembrar essa escolha: "+msg.err.Error(), toneError)
		}
		return m, nil

	case resultsMsg:
		// Descarta respostas de buscas que já foram superadas.
		if msg.gen != m.queryGen {
			return m, nil
		}
		if msg.err != nil {
			m.notify("busca falhou: "+msg.err.Error(), toneError)
			return m, nil
		}
		m.results = msg.page
		m.resultSel = min(m.resultSel, max(m.results.Len()-1, 0))
		return m, nil

	case filterMsg:
		if msg.gen != m.filterGen {
			return m, nil
		}
		m.filterLoading = false
		if msg.err != nil {
			m.notify("filtro falhou: "+msg.err.Error(), toneError)
			return m, nil
		}
		m.filterPage = msg.page
		m.cursor[zoneSuggested] = min(
			m.cursor[zoneSuggested],
			max(m.filterPage.Len()-1, 0),
		)
		return m, nil

	case favoriteMsg:
		if msg.err != nil {
			m.notify("não deu para favoritar: "+msg.err.Error(), toneError)
			return m, nil
		}
		if msg.on {
			m.notify(string(msg.id)+" favoritada", toneSuccess)
		} else {
			m.notify(string(msg.id)+" removida dos favoritos", toneInfo)
		}
		// Favoritar reordena o painel de favoritas e o score das sugestões,
		// então a home precisa ser recarregada.
		return m, m.reload()

	case launchedMsg:
		switch {
		case errors.Is(msg.err, domain.ErrConfirmationRequired):
			m.notify("“"+string(msg.tool)+"” é destrutiva — confirmação ainda não implementada", toneWarn)
		case msg.err != nil:
			m.notify("falha ao executar: "+msg.err.Error(), toneError)
		default:
			m.notify("executando "+string(msg.tool), toneSuccess)
			return m, m.reload()
		}
		return m, nil

	case openedMsg:
		// Sem toast: a tela recém-aberta já é o feedback, e uma mensagem por
		// cima dela seria ruído. Só o painel precisa saber.
		if msg.err != nil {
			m.notify("não deu para registrar o uso: "+msg.err.Error(), toneError)
			return m, nil
		}
		return m, m.reload()

	case requirementsMsg:
		if msg.err != nil {
			m.notify("falha ao verificar pré-requisitos: "+msg.err.Error(), toneError)
			return m, nil
		}
		if len(msg.missing) > 0 {
			return m, tui.Navigate(requirementsscreen.New(m.deps, msg.tool, msg.missing))
		}
		return m, m.confirmOrOpen(msg.tool)

	case tickMsg:
		if m.toast.text != "" && m.now().Sub(m.toast.at) > toastTTL {
			m.toast = toast{}
		}
		return m, tick()

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
}

// redistributeRecent mantém só os três usos mais novos em "recentes" e
// reaproveita o excedente em "sugeridas". Itens já visíveis em favoritas ou
// nos três recentes não são repetidos em outro painel.
func (m *Model) redistributeRecent() {
	if len(m.highlights.Recent) <= recentLimit {
		return
	}

	overflow := m.highlights.Recent[recentLimit:]
	m.highlights.Recent = m.highlights.Recent[:recentLimit]

	seen := make(map[domain.ToolID]bool, len(m.highlights.Favorites)+recentLimit)
	for _, match := range m.highlights.Favorites {
		seen[match.Tool.ID] = true
	}
	for _, match := range m.highlights.Recent {
		seen[match.Tool.ID] = true
	}

	suggested := make([]domain.Match, 0, highlightLimit)
	appendUnique := func(matches []domain.Match) {
		for _, match := range matches {
			if len(suggested) >= highlightLimit {
				return
			}
			if seen[match.Tool.ID] {
				continue
			}
			seen[match.Tool.ID] = true
			suggested = append(suggested, match)
		}
	}
	appendUnique(overflow)
	appendUnique(m.highlights.Suggested)
	m.highlights.Suggested = suggested
}

// reload recarrega destaques e a busca corrente, se houver.
func (m *Model) reload() tea.Cmd {
	cmds := []tea.Cmd{m.loadCatalog()}
	if m.searching && m.input.Value() != "" {
		m.queryGen++
		cmds = append(cmds, m.runQuery(m.input.Value()))
	}
	return tea.Batch(cmds...)
}

// handleKey despacha o teclado conforme o modo ativo.
func (m *Model) handleKey(msg tea.KeyMsg) (tui.Screen, tea.Cmd) {
	if m.updatePrompt {
		return m.handleUpdatePromptKey(msg)
	}
	if m.searching {
		return m.handleSearchKey(msg)
	}
	// A conferência acontece aqui, e não no redimensionamento: é depois do
	// render que se sabe o que coube na tela, e é a tecla seguinte que o
	// usuário vai usar esperando que o foco esteja em algo visível.
	m.normalizeFocus()
	return m.handleBrowseKey(msg)
}

// Opções do aviso de atualização, na ordem em que aparecem na lista.
const (
	updatePromptUpdate  = iota // atualizar
	updatePromptSkip           // ignorar até a próxima
	updatePromptIgnore         // ignorar
	updatePromptOptions        // sentinela: quantidade de opções
)

// updatePromptDefault é a opção pré-selecionada ao abrir o aviso.
const updatePromptDefault = updatePromptUpdate

// handleUpdatePromptKey trata o aviso de atualização. As setas navegam entre
// as três opções como qualquer lista da engine; as letras continuam
// funcionando como atalho direto, sem precisar navegar até a opção.
//
// Nenhuma saída pede uma segunda confirmação, porque nenhuma delas muda nada
// sozinha — "atualizar" só abre a tela que ainda vai perguntar de novo antes
// de trocar o binário.
func (m *Model) handleUpdatePromptKey(msg tea.KeyMsg) (tui.Screen, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		m.updatePromptCursor = max(m.updatePromptCursor-1, 0)
		return m, nil
	case "down", "j":
		m.updatePromptCursor = min(m.updatePromptCursor+1, updatePromptOptions-1)
		return m, nil
	case "enter", " ":
		return m.activateUpdatePrompt(m.updatePromptCursor)
	case "a":
		return m.activateUpdatePrompt(updatePromptUpdate)
	case "p":
		return m.activateUpdatePrompt(updatePromptSkip)
	case "i", "esc":
		return m.activateUpdatePrompt(updatePromptIgnore)
	}
	return m, nil
}

// activateUpdatePrompt executa a opção escolhida no aviso, por navegação ou
// por atalho de letra — os dois caminhos terminam aqui.
func (m *Model) activateUpdatePrompt(option int) (tui.Screen, tea.Cmd) {
	m.updatePrompt = false
	switch option {
	case updatePromptUpdate:
		if m.updateScreen == nil {
			return m, nil
		}
		return m, tui.Navigate(m.updateScreen())
	case updatePromptSkip:
		return m, m.skipUpdate(m.updateStatus.Latest.Tag)
	default:
		return m, nil
	}
}

// handleSearchKey trata o teclado no modo busca.
//
// O textinput recebe a tecla por último e apenas quando nenhum atalho a
// consumiu — do contrário, "/" e as setas apareceriam no campo de texto.
func (m *Model) handleSearchKey(msg tea.KeyMsg) (tui.Screen, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.exitSearch()
		return m, nil

	case "up", "ctrl+p":
		m.resultSel = max(m.resultSel-1, 0)
		return m, nil

	case "down", "ctrl+n":
		m.resultSel = min(m.resultSel+1, max(m.results.Len()-1, 0))
		return m, nil

	case "enter":
		if t, ok := m.selectedResult(); ok {
			m.exitSearch()
			return m, m.openTool(t)
		}
		return m, nil

	case "ctrl+f":
		if t, ok := m.selectedResult(); ok {
			return m, m.toggleFavorite(t.ID)
		}
		return m, nil
	}

	before := m.input.Value()
	in, cmd := m.input.Update(msg)
	m.input = in

	if m.input.Value() == before {
		return m, cmd
	}

	// Cada alteração no termo invalida a busca anterior.
	m.queryGen++
	m.resultSel = 0
	if m.input.Value() == "" {
		m.results = domain.Page{}
		return m, cmd
	}
	return m, tea.Batch(cmd, m.runQuery(m.input.Value()))
}

// handleBrowseKey trata o teclado no modo navegação.
func (m *Model) handleBrowseKey(msg tea.KeyMsg) (tui.Screen, tea.Cmd) {
	switch msg.String() {
	case "/", "ctrl+k":
		m.enterSearch()
		return m, textinputBlink()

	case "tab":
		m.cycleZone(1)
		return m, nil

	case "shift+tab":
		m.cycleZone(-1)
		return m, nil

	case "left", "h":
		return m, m.moveZone(-1)

	case "right", "l":
		return m, m.moveZone(1)

	case "up", "k":
		return m, m.moveVertical(-1)

	case "down", "j":
		return m, m.moveVertical(1)

	case "g", "home":
		return m, m.jumpCursor(0)

	case "G", "end":
		return m, m.jumpCursor(max(m.zoneLen(m.focus)-1, 0))

	case "f":
		if t, ok := m.selectedTool(); ok {
			return m, m.toggleFavorite(t.ID)
		}
		return m, nil

	case "enter":
		switch m.focus {
		case zoneSearch:
			m.enterSearch()
			return m, textinputBlink()
		case zoneMarketplace:
			return m, m.openMarketplace()
		}
		if t, ok := m.selectedTool(); ok {
			return m, m.openTool(t)
		}
		return m, nil

	case "r", "ctrl+r":
		m.loading = true
		m.notify("recarregando catálogo…", toneInfo)
		cmds := []tea.Cmd{m.loadCatalog()}
		if m.marketplaceEnabled() {
			m.marketplaceLoading = true
			m.marketplaceErr = nil
			cmds = append(cmds, m.loadMarketplace())
		}
		return m, tea.Batch(cmds...)

	case "m":
		return m, m.openMarketplace()

	case "c":
		return m, m.openSettings()
	}

	return m, nil
}

// textinputBlink expõe o cursor piscante do bubbles como tea.Cmd.
func textinputBlink() tea.Cmd { return textinput.Blink }

func (m *Model) enterSearch() {
	m.searching = true
	m.input.Focus()
	m.resultSel = 0
}

func (m *Model) exitSearch() {
	m.searching = false
	m.input.Blur()
	m.input.SetValue("")
	m.results = domain.Page{}
	m.queryGen++
	m.focus = zoneSearch
}

// moveZone caminha entre os painéis de conteúdo, entrando na sidebar quando
// sai pela esquerda do primeiro painel.
func (m *Model) moveZone(delta int) tea.Cmd {
	if m.focus == zoneSidebar {
		if delta > 0 {
			m.focus = zoneSearch
		}
		return nil
	}
	if m.focus == zoneSearch {
		if delta < 0 && m.sidebarVisible() {
			m.focus = zoneSidebar
		} else if delta > 0 {
			m.focus = m.primaryContentZone()
		}
		return nil
	}
	// A vitrine ocupa a largura inteira: não há vizinho lateral dentro dela,
	// então a seta horizontal sai para a coluna ao lado ou para os painéis.
	if m.focus == zoneMarketplace {
		if delta < 0 && m.sidebarVisible() {
			m.focus = zoneSidebar
		} else if delta > 0 {
			m.focus = m.primaryContentZone()
		}
		return nil
	}

	zones := m.contentZones()
	idx := 0
	for i, z := range zones {
		if z == m.focus {
			idx = i
			break
		}
	}
	next := idx + delta
	switch {
	case next < 0:
		if m.sidebarVisible() {
			m.focus = zoneSidebar
		} else {
			m.focus = zoneSearch
		}
	case next >= len(zones):
		// Fica no último painel: dar a volta para a sidebar confundiria mais
		// do que ajudaria.
	default:
		m.focus = zones[next]
	}
	return nil
}

// moveVertical usa a seta para entrar na busca ou voltar ao conteúdo, além de
// mover o cursor da lista. Assim, toda região da home é alcançável sem Tab.
func (m *Model) moveVertical(delta int) tea.Cmd {
	if m.focus == zoneSearch {
		if delta > 0 {
			m.focus = m.zoneBelowSearch()
		}
		return nil
	}
	// A vitrine é um bloco só: a seta atravessa em vez de mover um cursor
	// que não existe.
	if m.focus == zoneMarketplace {
		if delta > 0 {
			m.focus = m.primaryContentZone()
		} else {
			m.focus = zoneSearch
		}
		return nil
	}
	// Subir do topo de um painel passa pela vitrine quando ela está na tela.
	if m.focus != zoneSidebar && delta < 0 && m.cursor[m.focus] == 0 {
		m.focus = zoneSearch
		if m.marketplaceVisible() {
			m.focus = zoneMarketplace
		}
		return nil
	}
	return m.moveCursor(delta)
}

// zoneBelowSearch é o que recebe o foco ao descer da busca: a vitrine quando
// ela está na tela, senão o primeiro painel com conteúdo.
func (m *Model) zoneBelowSearch() zone {
	if m.marketplaceVisible() {
		return zoneMarketplace
	}
	return m.primaryContentZone()
}

// moveCursor move a seleção dentro da zona focada, sem wrap-around.
func (m *Model) moveCursor(delta int) tea.Cmd {
	n := m.zoneLen(m.focus)
	if n == 0 {
		m.cursor[m.focus] = 0
		return nil
	}
	before := m.cursor[m.focus]
	m.cursor[m.focus] = min(max(before+delta, 0), n-1)
	if m.focus != zoneSidebar || m.cursor[m.focus] == before {
		return nil
	}

	return m.filterChanged()
}

func (m *Model) jumpCursor(target int) tea.Cmd {
	if m.focus < 0 || m.focus >= zoneCount {
		return nil
	}
	before := m.cursor[m.focus]
	m.cursor[m.focus] = min(max(target, 0), max(m.zoneLen(m.focus)-1, 0))
	if before == m.cursor[m.focus] || m.focus != zoneSidebar {
		return nil
	}
	return m.filterChanged()
}

// filterChanged invalida o recorte anterior sempre que a categoria muda;
// nenhuma tool do filtro antigo pisca sob o novo rótulo.
func (m *Model) filterChanged() tea.Cmd {
	m.filterGen++
	m.normalizeFocus()
	m.cursor[zoneSuggested] = 0
	if m.hasFilter() {
		m.filterLoading = true
		m.filterPage = domain.Page{}
		return m.loadFilter()
	}
	m.filterLoading = false
	m.filterPage = domain.Page{}
	return nil
}

// clampCursors garante que nenhuma seleção aponte além do fim da sua lista
// depois de uma recarga que encurtou os painéis.
func (m *Model) clampCursors() {
	for z := zone(0); z < zoneCount; z++ {
		m.cursor[z] = min(m.cursor[z], max(m.zoneLen(z)-1, 0))
	}
}

// normalizeFocus tira o foco de uma zona que saiu da tela. Sem isto, filtrar
// por categoria ou encolher a janela deixaria as setas movendo um cursor que
// o usuário não está mais vendo.
func (m *Model) normalizeFocus() {
	if m.focus == zoneMarketplace && !m.marketplaceVisible() {
		m.focus = zoneSearch
		return
	}
	if !isPanelZone(m.focus) {
		return
	}
	for _, z := range m.contentZones() {
		if z == m.focus {
			return
		}
	}
	m.focus = m.primaryContentZone()
}

func isPanelZone(z zone) bool {
	for _, panel := range panelZones {
		if panel == z {
			return true
		}
	}
	return false
}

// zoneLen devolve quantos itens a zona contém.
func (m *Model) zoneLen(z zone) int {
	switch z {
	case zoneSidebar:
		return len(m.categories)
	case zoneSearch:
		return 0
	case zoneFavorites:
		return len(m.highlights.Favorites)
	case zoneRecent:
		return len(m.highlights.Recent)
	case zoneSuggested:
		return len(m.highlights.Suggested)
	default:
		return 0
	}
}

// zoneItems devolve os matches da zona; nil para a sidebar.
func (m *Model) zoneItems(z zone) []domain.Match {
	if m.hasFilter() {
		if z == zoneSuggested {
			return m.filterPage.Items
		}
		return nil
	}
	switch z {
	case zoneFavorites:
		return m.highlights.Favorites
	case zoneRecent:
		return m.highlights.Recent
	case zoneSuggested:
		return m.highlights.Suggested
	default:
		return nil
	}
}

// selectedCategory devolve o filtro da sidebar. O primeiro item sintético,
// "Todas", tem ID vazio e representa a ausência de filtro.
func (m *Model) selectedCategory() (domain.Category, bool) {
	i := m.cursor[zoneSidebar]
	if i <= 0 || i >= len(m.categories) {
		return domain.Category{}, false
	}
	return m.categories[i].Category, true
}

func (m *Model) hasFilter() bool {
	_, category := m.selectedCategory()
	return category
}

func (m *Model) categoryCount(id domain.CategoryID) int {
	for _, category := range m.categories {
		if category.ID == id {
			return category.Count
		}
	}
	return 0
}

func (m *Model) sidebarVisible() bool {
	return m.width-2 >= sidebarMinTotal && len(m.categories) > 0
}

// marketplaceVisible espelha a condição de render da vitrine. Foco em uma
// zona que não está desenhada deixaria a tecla Enter sem efeito visível.
func (m *Model) marketplaceVisible() bool {
	return m.marketplace != nil && !m.hasFilter() && m.height-2 >= marketplaceMinHeight
}

// contentZones são os painéis alcançáveis pelo teclado: os que o último
// render desenhou. Antes do primeiro frame a lista está vazia, e aí valem os
// três painéis declarados.
func (m *Model) contentZones() []zone {
	if m.hasFilter() {
		return []zone{zoneSuggested}
	}
	if len(m.drawnPanels) > 0 {
		return m.drawnPanels
	}
	return panelZones[:]
}

// primaryContentZone é o painel que recebe o foco ao descer da busca: o
// primeiro desenhado que tenha conteúdo, ou o primeiro desenhado.
func (m *Model) primaryContentZone() zone {
	zones := m.contentZones()
	if len(zones) == 0 {
		// Janela baixa demais para qualquer painel: a busca é o único lugar
		// onde o foco ainda significa alguma coisa.
		return zoneSearch
	}
	for _, z := range zones {
		if m.zoneLen(z) > 0 {
			return z
		}
	}
	return zones[0]
}

// cycleZone preserva o Tab como alternativa, mas percorre só regiões que
// estão realmente visíveis no layout corrente.
func (m *Model) cycleZone(delta int) {
	zones := make([]zone, 0, zoneCount)
	if m.sidebarVisible() {
		zones = append(zones, zoneSidebar)
	}
	zones = append(zones, zoneSearch)
	if m.marketplaceVisible() {
		zones = append(zones, zoneMarketplace)
	}
	zones = append(zones, m.contentZones()...)

	current := 0
	for i, z := range zones {
		if z == m.focus {
			current = i
			break
		}
	}
	current = (current + delta + len(zones)) % len(zones)
	m.focus = zones[current]
}

// selectedTool devolve a tool sob o cursor da zona focada.
func (m *Model) selectedTool() (domain.Tool, bool) {
	items := m.zoneItems(m.focus)
	i := m.cursor[m.focus]
	if i < 0 || i >= len(items) {
		return domain.Tool{}, false
	}
	return items[i].Tool, true
}

// selectedResult devolve a tool sob o cursor da lista de busca.
func (m *Model) selectedResult() (domain.Tool, bool) {
	if m.resultSel < 0 || m.resultSel >= m.results.Len() {
		return domain.Tool{}, false
	}
	return m.results.Items[m.resultSel].Tool, true
}

// notify publica uma mensagem efêmera na barra de status.
func (m *Model) notify(text string, t tone) {
	m.toast = toast{text: text, tone: t, at: m.now()}
}
