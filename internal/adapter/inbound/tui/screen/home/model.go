// Package home implementa a tela inicial do lealing.
//
// A home é a única tela que fala com três portas ao mesmo tempo (catálogo,
// preferências e launcher), porque é onde o usuário decide o que fazer. Toda
// a comunicação é assíncrona: nenhuma chamada de porta acontece dentro de
// Update ou View — só dentro de tea.Cmd, em goroutine.
package home

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/port"
)

// zone é a região da home que detém o foco do teclado.
type zone uint8

const (
	zoneSidebar zone = iota
	zoneFavorites
	zoneRecent
	zoneSuggested
	zoneCount // sentinela para o ciclo do Tab
)

// panelZones são as zonas de conteúdo, na ordem em que aparecem na tela.
var panelZones = [...]zone{zoneFavorites, zoneRecent, zoneSuggested}

// Model é o estado da home.
type Model struct {
	deps    tui.Deps
	catalog port.Catalog
	prefs   port.Preferences
	launch  port.Launcher
	clock   port.Clock
	screens tui.Screens

	width, height int

	// Dados do catálogo, preenchidos de forma assíncrona.
	highlights domain.Highlights
	categories []port.CategoryView
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

	loading bool
	err     error
	toast   toast
	user    string
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
	Catalog  port.Catalog
	Prefs    port.Preferences
	Launcher port.Launcher
	Clock    port.Clock
	// Screens são as tools com tela própria dentro da TUI. As demais caem
	// no Launcher.
	Screens tui.Screens
}

// New monta a home. Nada é carregado aqui — a primeira carga acontece em
// Init, para que a janela apareça imediatamente mesmo com catálogo lento.
func New(cfg Config) *Model {
	in := textinput.New()
	in.Prompt = ""
	in.Placeholder = "buscar tools…  (tag:git  kind:process  is:fav)"
	in.CharLimit = 128

	clock := cfg.Clock
	if clock == nil {
		clock = port.SystemClock
	}

	return &Model{
		deps:    cfg.Deps,
		catalog: cfg.Catalog,
		prefs:   cfg.Prefs,
		launch:  cfg.Launcher,
		clock:   clock,
		screens: cfg.Screens,
		input:   in,
		catByID: map[domain.CategoryID]domain.Category{},
		loading: true,
		user:    currentUser(),
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
	return tea.Batch(m.loadCatalog(), tick())
}

// currentUser devolve um nome amigável para a saudação.
func currentUser() string {
	if u := os.Getenv("USER"); u != "" {
		return u
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
	categories []port.CategoryView
	err        error
}

// resultsMsg traz o resultado de uma busca, carimbado com a geração.
type resultsMsg struct {
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
	// silent suprime o aviso de sucesso: abrir uma tela já é feedback
	// suficiente, e um toast por cima dela seria ruído.
	silent bool
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

// runQuery dispara uma busca carimbada com a geração atual.
func (m *Model) runQuery(term string) tea.Cmd {
	catalog := m.catalog
	gen := m.queryGen
	q := domain.Query{Term: term, Limit: searchPageSize}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		page, err := catalog.Browse(ctx, q)
		return resultsMsg{gen: gen, page: page, err: err}
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
	if screen, ok := m.screens.Open(t.ID); ok {
		// Abrir conta como uso: é o que alimenta "recentes" e as sugestões.
		return tea.Batch(tui.Navigate(screen), m.recordOpen(t.ID))
	}
	return m.launchTool(t)
}

// recordOpen contabiliza a abertura de uma tool com tela própria.
func (m *Model) recordOpen(id domain.ToolID) tea.Cmd {
	launcher := m.launch
	if launcher == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		// Detached: a tela já está aberta na TUI; isto só registra o uso.
		_, err := launcher.Launch(ctx, id, nil, port.LaunchOptions{Confirmed: true, Detached: true})
		return launchedMsg{tool: id, err: err, silent: true}
	}
}

// launchTool inicia a execução da tool selecionada.
func (m *Model) launchTool(t domain.Tool) tea.Cmd {
	launcher := m.launch
	if launcher == nil {
		return func() tea.Msg {
			return launchedMsg{tool: t.ID, err: fmt.Errorf("launcher indisponível")}
		}
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		// Confirmed vem falso de propósito: tools destrutivas devem devolver
		// ErrConfirmationRequired e abrir o diálogo, não executar direto.
		_, err := launcher.Launch(ctx, t.ID, nil, port.LaunchOptions{})
		return launchedMsg{tool: t.ID, err: err}
	}
}
