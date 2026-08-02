// Package home implementa a tela inicial do lealing.
//
// A home coordena as portas de entrada de catálogo, preferências, execução e
// pré-requisitos, porque é onde o usuário decide o que fazer. Toda a
// comunicação é assíncrona: nenhuma chamada de porta acontece dentro de
// Update ou View — só dentro de tea.Cmd, em goroutine.
package home

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	confirmationscreen "github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/confirmation"
	pluginscreen "github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/plugin"
	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/hostaction"
	"github.com/mateuslh/lealing/internal/core/interactive"
	coremarket "github.com/mateuslh/lealing/internal/core/marketplace"
	"github.com/mateuslh/lealing/internal/core/port/inbound"
)

// zone é a região da home que detém o foco do teclado.
type zone uint8

const (
	zoneSidebar zone = iota
	zoneSearch
	zoneFavorites
	zoneRecent
	zoneSuggested
	zoneCount // sentinela para o ciclo do Tab
)

// panelZones são as zonas de conteúdo, na ordem em que aparecem na tela.
var panelZones = [...]zone{zoneFavorites, zoneRecent, zoneSuggested}

// Model é o estado da home.
type Model struct {
	deps        tui.Deps
	catalog     inbound.Catalog
	prefs       inbound.Preferences
	launch      inbound.Launcher
	prereqs     inbound.Prerequisites
	now         func() time.Time
	screens     tui.Screens
	interactive interactive.Opener
	hostActions hostaction.Actions
	marketplace coremarket.Manager

	width, height int

	// Dados do catálogo, preenchidos de forma assíncrona.
	highlights domain.Highlights
	categories []inbound.CategoryView
	catByID    map[domain.CategoryID]domain.Category

	focus zone
	// cursor guarda a seleção de cada zona separadamente, para que trocar de
	// painel e voltar não perca o lugar.
	cursor [zoneCount]int

	// Modo busca.
	searching bool
	input     textinput.Model
	results   domain.Page
	resultSel int
	// queryGen descarta respostas de buscas obsoletas: digitar rápido dispara
	// várias queries e a mais antiga pode voltar por último.
	queryGen int

	// O recorte dos filtros é carregado sob demanda ao mover a seleção na
	// sidebar. A geração impede uma resposta lenta de repintar o filtro
	// anterior quando o usuário percorre a lista rapidamente.
	filterPage    domain.Page
	filterGen     int
	filterLoading bool

	loading bool
	err     error
	toast   toast
	user    string

	marketplaceTools   []coremarket.Listing
	marketplaceLoading bool
	marketplaceErr     error
}

// toast é uma mensagem efêmera na barra de status.
type toast struct {
	text string
	tone tone
	at   time.Time
}

type tone uint8

const (
	toneInfo tone = iota
	toneSuccess
	toneWarn
	toneError
)

// toastTTL é quanto tempo uma mensagem permanece visível.
const toastTTL = 4 * time.Second

var _ tui.Screen = (*Model)(nil)

// Config agrupa as dependências da home.
type Config struct {
	Deps     tui.Deps
	Catalog  inbound.Catalog
	Prefs    inbound.Preferences
	Launcher inbound.Launcher
	// Prerequisites valida os executáveis pela porta de entrada da aplicação.
	Prerequisites inbound.Prerequisites
	Now           func() time.Time
	// User é o rótulo já resolvido pelo composition root para a saudação.
	User string
	// Screens são as tools com tela própria dentro da TUI. As demais caem
	// no Launcher.
	Screens tui.Screens
	// Interactive abre qualquer manifest screen-v1 pela mesma tela genérica.
	Interactive interactive.Opener
	HostActions hostaction.Actions
	// Marketplace é opcional para manter a home testável e permitir que a
	// engine inicie mesmo quando o índice público estiver indisponível.
	Marketplace coremarket.Manager
}

// New monta a home. Nada é carregado aqui — a primeira carga acontece em
// Init, para que a janela apareça imediatamente mesmo com catálogo lento.
func New(cfg Config) *Model {
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = "buscar tools…  (kind:process  is:fav)"
	in.CharLimit = 128

	now := cfg.Now
	if now == nil {
		now = time.Now
	}

	return &Model{
		deps:               cfg.Deps,
		catalog:            cfg.Catalog,
		prefs:              cfg.Prefs,
		launch:             cfg.Launcher,
		prereqs:            cfg.Prerequisites,
		now:                now,
		screens:            cfg.Screens,
		interactive:        cfg.Interactive,
		hostActions:        cfg.HostActions,
		marketplace:        cfg.Marketplace,
		input:              in,
		catByID:            map[domain.CategoryID]domain.Category{},
		loading:            true,
		marketplaceLoading: cfg.Marketplace != nil,
		user:               fallbackUser(cfg.User),
		// Abre com o foco em "sugeridas": é o único painel garantidamente
		// preenchido na primeira execução, e focar a sidebar de saída
		// esmaeceria o espectro inteiro menos uma categoria.
		focus: zoneSuggested,
	}
}

// ID implementa tui.Screen.
func (m *Model) ID() tui.ScreenID { return tui.ScreenHome }

// Title implementa tui.Screen.
func (m *Model) Title() string { return "home" }

// Init implementa tui.Screen.
func (m *Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.loadCatalog(), tick()}
	if m.marketplace != nil {
		cmds = append(cmds, m.loadMarketplace())
	}
	return tea.Batch(cmds...)
}

// Refresh implementa o contrato opcional do App. Não repete o tick: o relógio
// da home nunca parou, e um segundo ticker faria o toast expirar cedo demais.
func (m *Model) Refresh() tea.Cmd {
	cmds := []tea.Cmd{m.loadCatalog()}
	if m.marketplace != nil {
		cmds = append(cmds, m.loadMarketplace())
	}
	return tea.Batch(cmds...)
}

func fallbackUser(user string) string {
	if user != "" {
		return user
	}
	return "você"
}

// greeting escolhe a saudação pela hora local.
func greeting(now time.Time, user string) string {
	var period string
	switch h := now.Hour(); {
	case h < 6:
		period = "Boa madrugada"
	case h < 12:
		period = "Bom dia"
	case h < 18:
		period = "Boa tarde"
	default:
		period = "Boa noite"
	}
	if user == "" {
		return period
	}
	return fmt.Sprintf("%s, %s", period, user)
}

// --- Mensagens internas ------------------------------------------------

// catalogMsg carrega destaques e categorias de uma vez só.
type catalogMsg struct {
	highlights domain.Highlights
	categories []inbound.CategoryView
	err        error
}

type marketplaceMsg struct {
	tools []coremarket.Listing
	err   error
}

// resultsMsg traz o resultado de uma busca, carimbado com a geração.
type resultsMsg struct {
	gen  int
	page domain.Page
	err  error
}

// filterMsg traz as tools dos filtros selecionados na sidebar.
type filterMsg struct {
	gen  int
	page domain.Page
	err  error
}

// favoriteMsg confirma o toggle de um favorito.
type favoriteMsg struct {
	id  domain.ToolID
	on  bool
	err error
}

// launchedMsg confirma o início de uma execução.
type launchedMsg struct {
	tool domain.ToolID
	err  error
}

// openedMsg confirma que a abertura de uma tool com tela própria foi
// contabilizada.
type openedMsg struct {
	tool domain.ToolID
	err  error
}

// requirementsMsg entrega a checagem feita antes de iniciar uma tool.
type requirementsMsg struct {
	tool    domain.Tool
	missing []domain.Requirement
	err     error
}

// tickMsg move o relógio da barra de status e expira toasts.
type tickMsg time.Time

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// --- Comandos ----------------------------------------------------------

// loadCatalog busca destaques e categorias em paralelo com o render.
func (m *Model) loadCatalog() tea.Cmd {
	catalog := m.catalog
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		hl, err := catalog.Highlights(ctx, highlightLimit)
		if err != nil {
			return catalogMsg{err: err}
		}
		cats, err := catalog.Categories(ctx)
		if err != nil {
			return catalogMsg{err: err}
		}
		return catalogMsg{highlights: hl, categories: cats}
	}
}

// loadMarketplace consulta apenas metadados; nenhum artefato é baixado ou
// executado. A chamada ocorre em uma Cmd para que a rede nunca congele a Home.
func (m *Model) loadMarketplace() tea.Cmd {
	manager := m.marketplace
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		tools, err := manager.List(ctx)
		return marketplaceMsg{tools: tools, err: err}
	}
}

// runQuery dispara uma busca carimbada com a geração atual.
func (m *Model) runQuery(term string) tea.Cmd {
	catalog := m.catalog
	gen := m.queryGen
	q := domain.Query{Term: term, Limit: searchPageSize}
	if category, ok := m.selectedCategory(); ok {
		q.Categories = []domain.CategoryID{category.ID}
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		page, err := catalog.Browse(ctx, q)
		return resultsMsg{gen: gen, page: page, err: err}
	}
}

// loadFilter busca o recorte selecionado sem bloquear a navegação.
func (m *Model) loadFilter() tea.Cmd {
	catalog := m.catalog
	gen := m.filterGen
	q := domain.Query{
		Sort:  domain.SortAlpha,
		Limit: searchPageSize,
	}
	if category, ok := m.selectedCategory(); ok {
		q.Categories = []domain.CategoryID{category.ID}
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		page, err := catalog.Browse(ctx, q)
		return filterMsg{gen: gen, page: page, err: err}
	}
}

// toggleFavorite inverte o favorito da tool sob o cursor.
func (m *Model) toggleFavorite(id domain.ToolID) tea.Cmd {
	prefs := m.prefs
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		on, err := prefs.ToggleFavorite(ctx, id)
		return favoriteMsg{id: id, on: on, err: err}
	}
}

// openTool decide o que "executar" significa para esta tool.
//
// Tools com tela própria abrem dentro da TUI; as demais vão para o Launcher,
// que as executa como processo. Quem aperta enter não precisa saber a
// diferença.
func (m *Model) openTool(t domain.Tool) tea.Cmd {
	if len(t.Requirements) > 0 {
		return m.checkRequirements(t)
	}
	return m.confirmOrOpen(t)
}

func (m *Model) confirmOrOpen(t domain.Tool) tea.Cmd {
	if t.Risk.NeedsConfirmation() {
		return tui.Navigate(confirmationscreen.New(m.deps, t, func() tea.Cmd { return m.openReady(t, true) }))
	}
	return m.openReady(t, false)
}

// checkRequirements consulta o PATH fora de Update. O executável pode estar
// numa unidade de rede no Windows, então até essa leitura curta fica no Cmd.
func (m *Model) checkRequirements(t domain.Tool) tea.Cmd {
	prereqs := m.prereqs
	return func() tea.Msg {
		if prereqs == nil {
			return requirementsMsg{
				tool: t,
				err:  errors.New("checagem de pré-requisitos não configurada"),
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		missing, err := prereqs.Missing(ctx, t.ID)
		return requirementsMsg{tool: t, missing: missing, err: err}
	}
}

// openReady inicia uma tool cujos pré-requisitos já foram satisfeitos.
func (m *Model) openReady(t domain.Tool, confirmed bool) tea.Cmd {
	if screen, ok := m.screens.Open(t.ID); ok {
		// Abrir conta como uso: é o que alimenta "recentes" e as sugestões.
		return tea.Batch(tui.Navigate(screen), m.recordOpen(t.ID))
	}
	if t.Interactive() {
		screen := pluginscreen.New(m.deps, m.interactive, m.hostActions, t)
		return tea.Batch(tui.Navigate(screen), m.recordOpen(t.ID))
	}
	return m.launchTool(t, confirmed)
}

// recordOpen contabiliza a abertura de uma tool com tela própria.
//
// Vai direto a Preferences, e não ao Launcher: a tool nativa não tem runner
// que a atenda — é a TUI que a "executa", desenhando-a —, e pedir um Launch
// só para marcar o uso devolvia ErrToolNotFound e deixava "recentes" vazio
// para sempre.
//
// A confirmação chega depois de a tela já estar empilhada, então o Router a
// entrega à tool, não à home. É deliberado: a navegação não pode esperar o
// disco. Uma falha aqui não some sem rastro — a home recarrega ao voltar, e a
// tool simplesmente não estará em "recentes".
func (m *Model) recordOpen(id domain.ToolID) tea.Cmd {
	prefs := m.prefs
	if prefs == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return openedMsg{tool: id, err: prefs.RecordRun(ctx, id)}
	}
}

// launchTool inicia a execução da tool selecionada.
func (m *Model) launchTool(t domain.Tool, confirmed bool) tea.Cmd {
	launcher := m.launch
	if launcher == nil {
		return func() tea.Msg {
			return launchedMsg{tool: t.ID, err: fmt.Errorf("launcher indisponível")}
		}
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_, err := launcher.Launch(ctx, t.ID, nil, inbound.LaunchOptions{Confirmed: confirmed})
		return launchedMsg{tool: t.ID, err: err}
	}
}
