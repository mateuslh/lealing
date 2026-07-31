package home

import (
	"errors"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/core/domain"
)

// Limites de carga da home.
const (
	// highlightLimit é quantos itens cada painel de destaque pede. Sobra
	// deliberada: a lista recorta pela altura real do painel, então pedir a
	// mais evita um painel alto com espaço vazio no rodapé.
	highlightLimit = 12
	// searchPageSize é o teto de resultados por busca. A lista rola, mas
	// pedir mais que isso só desperdiça ranqueamento que ninguém vai ler.
	searchPageSize = 60
)

// Update implementa tui.Screen.
func (m *Model) Update(msg tea.Msg) (tui.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
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
		// Categorias sem tool saem da navegação, mas continuam no mapa de
		// consulta: uma tool pode referenciar uma categoria que acabou de
		// esvaziar por um filtro, e a lista ainda precisa do glyph e da cor.
		m.categories = m.categories[:0]
		m.catByID = make(map[domain.CategoryID]domain.Category, len(msg.categories))
		for _, c := range msg.categories {
			m.catByID[c.ID] = c.Category
			if c.Count > 0 {
				m.categories = append(m.categories, c)
			}
		}
		m.clampCursors()
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
		case msg.silent:
			return m, m.reload()
		default:
			m.notify("executando "+string(msg.tool), toneSuccess)
			return m, m.reload()
		}
		return m, nil

	case tickMsg:
		if m.toast.text != "" && m.clock.Now().Sub(m.toast.at) > toastTTL {
			m.toast = toast{}
		}
		return m, tick()

	case tea.KeyMsg:
		return m.handleKey(msg)
	}

	return m, nil
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
	if m.searching {
		return m.handleSearchKey(msg)
	}
	return m.handleBrowseKey(msg)
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
		m.focus = (m.focus + 1) % zoneCount
		return m, nil

	case "shift+tab":
		m.focus = (m.focus + zoneCount - 1) % zoneCount
		return m, nil

	case "left", "h":
		m.moveZone(-1)
		return m, nil

	case "right", "l":
		m.moveZone(1)
		return m, nil

	case "up", "k":
		m.moveCursor(-1)
		return m, nil

	case "down", "j":
		m.moveCursor(1)
		return m, nil

	case "g", "home":
		m.cursor[m.focus] = 0
		return m, nil

	case "G", "end":
		m.cursor[m.focus] = max(m.zoneLen(m.focus)-1, 0)
		return m, nil

	case "f":
		if t, ok := m.selectedTool(); ok {
			return m, m.toggleFavorite(t.ID)
		}
		return m, nil

	case "enter":
		if t, ok := m.selectedTool(); ok {
			return m, m.openTool(t)
		}
		return m, nil

	case "r", "ctrl+r":
		m.loading = true
		m.notify("recarregando catálogo…", toneInfo)
		return m, m.loadCatalog()
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
}

// moveZone caminha entre os painéis de conteúdo, entrando na sidebar quando
// sai pela esquerda do primeiro painel.
func (m *Model) moveZone(delta int) {
	if m.focus == zoneSidebar {
		if delta > 0 {
			m.focus = zoneFavorites
		}
		return
	}
	idx := 0
	for i, z := range panelZones {
		if z == m.focus {
			idx = i
			break
		}
	}
	next := idx + delta
	switch {
	case next < 0:
		m.focus = zoneSidebar
	case next >= len(panelZones):
		// Fica no último painel: dar a volta para a sidebar confundiria mais
		// do que ajudaria.
	default:
		m.focus = panelZones[next]
	}
}

// moveCursor move a seleção dentro da zona focada, sem wrap-around.
func (m *Model) moveCursor(delta int) {
	n := m.zoneLen(m.focus)
	if n == 0 {
		m.cursor[m.focus] = 0
		return
	}
	m.cursor[m.focus] = min(max(m.cursor[m.focus]+delta, 0), n-1)
}

// clampCursors garante que nenhuma seleção aponte além do fim da sua lista
// depois de uma recarga que encurtou os painéis.
func (m *Model) clampCursors() {
	for z := zone(0); z < zoneCount; z++ {
		m.cursor[z] = min(m.cursor[z], max(m.zoneLen(z)-1, 0))
	}
}

// zoneLen devolve quantos itens a zona contém.
func (m *Model) zoneLen(z zone) int {
	switch z {
	case zoneSidebar:
		return len(m.categories)
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
	m.toast = toast{text: text, tone: t, at: m.clock.Now()}
}
