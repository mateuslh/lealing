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
	"strings"
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
	"github.com/mateuslh/lealing/internal/core/selfupdate"
)

// zone é a região da home que detém o foco do teclado.
type zone uint8

const (
	zoneSidebar zone = iota
	zoneSearch
	// zoneMarketplace é a vitrine. Ela não tem cursor interno: o bloco todo
	// é um único alvo, e Enter abre a loja.
	zoneMarketplace
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
	interactive interactive.Opener
	hostActions hostaction.Actions
	marketplace coremarket.Manager
	// marketplaceScreen abre a loja. Ela chega como factory, e não como item
	// do catálogo, porque a loja não é uma tool entre as outras: é de onde as
	// outras vêm.
	marketplaceScreen tui.ScreenFactory
	// settingsScreen abre a configuração da engine, pelo mesmo motivo:
	// configurar o lealing não é usar uma tool do lealing.
	settingsScreen tui.ScreenFactory

	width, height int

	// Dados do catálogo, preenchidos de forma assíncrona.
	highlights domain.Highlights
	categories []inbound.CategoryView
	catByID    map[domain.CategoryID]domain.Category

	focus zone
	// cursor guarda a seleção de cada zona separadamente, para que trocar de
	// painel e voltar não perca o lugar.
	cursor [zoneCount]int
	// drawnPanels são os painéis que o último render de fato desenhou, na
	// ordem em que apareceram. O layout descarta painéis quando a janela
	// encolhe e ainda os reordena; a navegação lê esta lista para nunca focar
	// algo que o usuário não está vendo.
	drawnPanels []zone

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
	// greetingName e marketplaceOnHome são consultados a cada uso, e não
	// lidos na construção, porque a tela de configuração muda os dois e o
	// usuário volta para cá esperando ver o efeito.
	greetingName      func() string
	marketplaceOnHome func() bool

	marketplaceCatalog coremarket.Catalog
	marketplaceLoading bool
	marketplaceErr     error

	updateManager        selfupdate.Manager
	updateCheckHome      func() bool
	updateStatus         selfupdate.Status
	updateScreen         tui.ScreenFactory
	updateSkippedVersion func() string
	skipUpdateVersion    func(string) error
	// updatePrompt mostra o aviso de atualização sobre a home, sem bloquear
	// o carregamento: a checagem que o liga roda em paralelo com o catálogo.
	updatePrompt bool
	// updatePromptCursor é a opção selecionada no aviso — atualizar, ignorar
	// até a próxima ou ignorar —, navegável pelas setas como qualquer lista
	// da engine, e não só por atalho de letra.
	updatePromptCursor int
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
	// Interactive abre qualquer manifest screen-v1 pela mesma tela genérica.
	Interactive interactive.Opener
	HostActions hostaction.Actions
	// Marketplace é opcional para manter a home testável e permitir que a
	// engine inicie mesmo quando o índice público estiver indisponível.
	Marketplace coremarket.Manager
	// MarketplaceScreen constrói a tela da loja sob demanda.
	MarketplaceScreen tui.ScreenFactory
	// SettingsScreen constrói a tela de configuração sob demanda.
	SettingsScreen tui.ScreenFactory
	// GreetingName resolve o nome da saudação a cada render, para que
	// trocá-lo na configuração valha sem reiniciar.
	GreetingName func() string
	// MarketplaceOnHome decide se a vitrine consulta a rede ao carregar.
	MarketplaceOnHome func() bool
	// UpdateManager verifica o último release publicado. Opcional pelo mesmo
	// motivo do Marketplace: a home sobe mesmo sem essa checagem.
	UpdateManager selfupdate.Manager
	// UpdateCheckOnHome decide se essa checagem acontece ao carregar.
	UpdateCheckOnHome func() bool
	// UpdateScreen constrói a tela de atualização sob demanda. O aviso de
	// startup e a ação "atualizar" abrem o mesmo fluxo da configuração.
	UpdateScreen tui.ScreenFactory
	// UpdateSkippedVersion lê a tag que o usuário mandou ignorar no aviso.
	// Vazio (ou nil) nunca casa com uma tag publicada, então nada é suprimido.
	UpdateSkippedVersion func() string
	// SkipUpdateVersion grava a tag ignorada, para o aviso não voltar antes
	// de um release mais novo existir.
	SkipUpdateVersion func(string) error
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
		deps:                 cfg.Deps,
		catalog:              cfg.Catalog,
		prefs:                cfg.Prefs,
		launch:               cfg.Launcher,
		prereqs:              cfg.Prerequisites,
		now:                  now,
		interactive:          cfg.Interactive,
		hostActions:          cfg.HostActions,
		marketplace:          cfg.Marketplace,
		marketplaceScreen:    cfg.MarketplaceScreen,
		settingsScreen:       cfg.SettingsScreen,
		greetingName:         cfg.GreetingName,
		marketplaceOnHome:    cfg.MarketplaceOnHome,
		updateManager:        cfg.UpdateManager,
		updateCheckHome:      cfg.UpdateCheckOnHome,
		updateScreen:         cfg.UpdateScreen,
		updateSkippedVersion: cfg.UpdateSkippedVersion,
		skipUpdateVersion:    cfg.SkipUpdateVersion,
		input:                in,
		catByID:              map[domain.CategoryID]domain.Category{},
		loading:              true,
		marketplaceLoading:   cfg.Marketplace != nil,
		user:                 fallbackUser(cfg.User),
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
	if m.marketplaceEnabled() {
		cmds = append(cmds, m.loadMarketplace())
	}
	if m.updateCheckEnabled() {
		cmds = append(cmds, m.checkUpdate())
	}
	return tea.Batch(cmds...)
}

// Refresh implementa o contrato opcional do App. Não repete o tick: o relógio
// da home nunca parou, e um segundo ticker faria o toast expirar cedo demais.
// Também não repete a verificação de atualização: ela muda no ritmo de um
// release, não a cada volta à home, e refazê-la a cada esc só gastaria a
// cota de requisições da API do GitHub sem nenhum ganho.
func (m *Model) Refresh() tea.Cmd {
	cmds := []tea.Cmd{m.loadCatalog()}
	if m.marketplaceEnabled() {
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
	catalog coremarket.Catalog
	err     error
}

// updateMsg entrega o resultado da checagem de atualização. Um erro é
// silencioso na home — a checagem é conveniência, não algo que o usuário
// veio resolver aqui — e por isso a mensagem não carrega err adiante do
// Update: falhar em verificar só deixa o selo de fora deste render.
type updateMsg struct {
	status selfupdate.Status
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

// openSettings empilha a configuração da engine.
func (m *Model) openSettings() tea.Cmd {
	if m.settingsScreen == nil {
		return nil
	}
	return tui.Navigate(m.settingsScreen())
}

// userName resolve o nome da saudação, com o do sistema como plano B.
func (m *Model) userName() string {
	if m.greetingName != nil {
		if name := strings.TrimSpace(m.greetingName()); name != "" {
			return name
		}
	}
	return m.user
}

// marketplaceEnabled informa se a vitrine pode falar com a rede.
func (m *Model) marketplaceEnabled() bool {
	return m.marketplace != nil && (m.marketplaceOnHome == nil || m.marketplaceOnHome())
}

// openMarketplace empilha a loja.
//
// Não passa por openReady porque a loja não está no catálogo: contá-la entre
// as tools a colocaria disputando espaço com o que ela mesma instala, e
// "recentes" acabaria dominado por ela.
func (m *Model) openMarketplace() tea.Cmd {
	if m.marketplaceScreen == nil {
		return nil
	}
	return tui.Navigate(m.marketplaceScreen())
}

// loadMarketplace consulta apenas metadados; nenhum artefato é baixado ou
// executado. A chamada ocorre em uma Cmd para que a rede nunca congele a Home.
func (m *Model) loadMarketplace() tea.Cmd {
	manager := m.marketplace
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		catalog, err := manager.Catalog(ctx)
		return marketplaceMsg{catalog: catalog, err: err}
	}
}

// updateCheckEnabled informa se a checagem de atualização pode falar com a
// rede. Mesmo desenho de marketplaceEnabled: nulo por padrão liga, e quem
// desligou na configuração é respeitado sem a home saber por quê.
func (m *Model) updateCheckEnabled() bool {
	return m.updateManager != nil && (m.updateCheckHome == nil || m.updateCheckHome())
}

// checkUpdate consulta o último release publicado fora da thread de render.
// Silenciar o erro aqui — e não só não propagá-lo — é deliberado: a home não
// tem onde mostrá-lo sem competir com o catálogo, e "não verificou" já é o
// que a ausência do selo diz sozinha.
func (m *Model) checkUpdate() tea.Cmd {
	manager := m.updateManager
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		status, err := manager.Check(ctx)
		if err != nil {
			return updateMsg{}
		}
		return updateMsg{status: status}
	}
}

// updateSkipped informa se a tag já foi ignorada num aviso anterior — a
// própria que se está prestes a mostrar de novo, ou uma velha, caso o
// gerenciador tenha voltado a apontar uma versão que já foi recusada.
func (m *Model) updateSkipped(tag string) bool {
	return tag != "" && m.updateSkippedVersion != nil && m.updateSkippedVersion() == tag
}

// updateSkippedMsg confirma que a escolha de ignorar uma versão foi gravada.
type updateSkippedMsg struct{ err error }

// skipUpdate grava a tag para o aviso não voltar antes de um release mais
// novo existir. A escrita é local e rápida, mas ainda assim só acontece
// dentro de um Cmd — nenhuma porta é chamada de dentro de Update.
func (m *Model) skipUpdate(tag string) tea.Cmd {
	skip := m.skipUpdateVersion
	return func() tea.Msg {
		if skip == nil {
			return updateSkippedMsg{}
		}
		return updateSkippedMsg{err: skip(tag)}
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
// Manifests screen-v1 abrem pela tela genérica; as demais execuções vão para
// o Launcher. Quem aperta Enter não precisa conhecer o runtime declarado.
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
	if t.Interactive() {
		screen := pluginscreen.New(m.deps, m.interactive, m.hostActions, t)
		return tea.Batch(tui.Navigate(screen), m.recordOpen(t.ID))
	}
	return m.launchTool(t, confirmed)
}

// recordOpen contabiliza a abertura de uma sessão interativa. Ela não passa
// pelo Launcher comum, por isso o registro é feito diretamente em Preferences.
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
