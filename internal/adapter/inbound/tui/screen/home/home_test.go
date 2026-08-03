package home

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	"github.com/mateuslh/lealing/internal/adapter/outbound/persistence"
	"github.com/mateuslh/lealing/internal/adapter/outbound/registry"
	"github.com/mateuslh/lealing/internal/adapter/outbound/search"
	"github.com/mateuslh/lealing/internal/catalog"
	"github.com/mateuslh/lealing/internal/core/domain"
	coremarket "github.com/mateuslh/lealing/internal/core/marketplace"
	"github.com/mateuslh/lealing/internal/core/port/inbound"
	"github.com/mateuslh/lealing/internal/core/port/outbound"
	"github.com/mateuslh/lealing/internal/core/service"
	"github.com/mateuslh/lealing/internal/core/toolinstall"
)

// Estes testes montam o caminho real — registry, buscador fuzzy e serviço —
// sobre um catálogo externo representativo. A engine de produção não publica
// tools padrão; o lote vive aqui apenas para exercitar busca e geometria.

func catalogFixture() []outbound.ToolProvider {
	tool := func(id, name string, category domain.CategoryID) domain.Tool {
		return domain.Tool{
			ID: domain.ToolID(id), Name: name, Summary: "Tool externa usada no teste da home.",
			Category: category, Kind: domain.KindProcess,
			Runtime: &domain.ExternalRuntime{
				Executable: "lealing-tool-example", ProtocolMin: 1, ProtocolMax: 1, UIMode: "screen-v1",
			},
		}
	}
	tools := []domain.Tool{
		tool("system-alpha", "Sistema Alpha", catalog.System.ID),
		tool("system-beta", "Sistema Beta", catalog.System.ID),
		tool("ai-alpha", "IA Alpha", catalog.AI.ID),
		tool("utility-alpha", "Utilitário Alpha", catalog.Utilities.ID),
		tool("utility-beta", "Utilitário Beta", catalog.Utilities.ID),
		tool("development-alpha", "Desenvolvimento Alpha", catalog.Development.ID),
		tool("development-beta", "Desenvolvimento Beta", catalog.Development.ID),
		tool("network-alpha", "Rede Alpha", catalog.Network.ID),
		tool("network-beta", "Rede Beta", catalog.Network.ID),
		tool("development-gamma", "Desenvolvimento Gamma", catalog.Development.ID),
		tool("development-delta", "Desenvolvimento Delta", catalog.Development.ID),
		tool("network-gamma", "Rede Gamma", catalog.Network.ID),
		tool("development-epsilon", "Desenvolvimento Epsilon", catalog.Development.ID),
		tool("development-zeta", "Desenvolvimento Zeta", catalog.Development.ID),
		tool("development-eta", "Desenvolvimento Eta", catalog.Development.ID),
	}
	tools[0].Keywords = []string{"literal", "exato"}
	tools[5].Requirements = []domain.Requirement{{Executable: "gh", Name: "GitHub CLI"}}
	return []outbound.ToolProvider{&registry.Static{
		Label: "fixture externo", Tools: tools, Categories: catalog.Categories(),
	}}
}

func newTestModel(t *testing.T) *Model {
	t.Helper()

	clock := outbound.ClockFunc(func() time.Time {
		return time.Date(2026, 7, 30, 15, 0, 0, 0, time.UTC)
	})

	repo := registry.New(catalogFixture(), registry.WithStrict(true))
	svc := service.NewCatalog(repo, search.NewFuzzy(), persistence.NewMemoryUsage(), clock)

	m := New(Config{
		Deps:          tui.Deps{Theme: theme.Default()},
		Catalog:       svc,
		Prefs:         svc,
		Prerequisites: service.NewPrerequisites(repo, nil),
		Now:           clock.Now,
	})

	// Executa a carga inicial de forma síncrona.
	next, _ := m.Update(m.loadCatalog()())
	m = next.(*Model)

	if m.err != nil {
		t.Fatalf("carga do catálogo falhou: %v", m.err)
	}
	if m.highlights.TotalTools == 0 {
		t.Fatal("catálogo externo de teste veio vazio")
	}
	return m
}

// press envia uma tecla e devolve o comando resultante.
func press(t *testing.T, m *Model, key string) (*Model, tea.Cmd) {
	t.Helper()
	var msg tea.KeyMsg
	switch key {
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "left":
		msg = tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		msg = tea.KeyMsg{Type: tea.KeyRight}
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	next, cmd := m.Update(msg)
	return next.(*Model), cmd
}

// cmdTimeout descarta comandos temporizados. Cada tecla na busca devolve
// Batch(textinput.Blink, runQuery) e o Blink só responde após o intervalo de
// piscada — esperá-lo somaria meio segundo por caractere digitado.
const cmdTimeout = 100 * time.Millisecond

// apply executa um comando e alimenta o model com o que ele produziu.
//
// Desdobra tea.BatchMsg: entregar o Batch cru ao Update descartaria
// silenciosamente a consulta que vem dentro dele.
func apply(t *testing.T, m *Model, cmd tea.Cmd) *Model {
	t.Helper()
	if cmd == nil {
		return m
	}

	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()

	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(cmdTimeout):
		return m
	}

	switch v := msg.(type) {
	case nil:
		return m
	case tea.BatchMsg:
		for _, c := range v {
			m = apply(t, m, c)
		}
		return m
	default:
		// Não segue comandos encadeados: tick() se reagendaria para sempre.
		next, _ := m.Update(msg)
		return next.(*Model)
	}
}

// typeText digita caractere a caractere, drenando a busca a cada tecla —
// que é exatamente o que acontece na interação real.
func typeText(t *testing.T, m *Model, text string) *Model {
	t.Helper()
	for _, r := range text {
		var cmd tea.Cmd
		m, cmd = press(t, m, string(r))
		m = apply(t, m, cmd)
	}
	return m
}

func TestCatalogoExternoValida(t *testing.T) {
	// registry.WithStrict(true) rejeita tool inválida, ID duplicado e
	// categoria não declarada. newTestModel falha se algo disso ocorrer.
	m := newTestModel(t)

	// Toda tool declarada precisa ter uma tela ou um runner; a contagem
	// aqui trava o acervo contra remoção acidental.
	if m.highlights.TotalTools != 15 {
		t.Errorf("tools = %d, quero 15", m.highlights.TotalTools)
	}
	if m.highlights.TotalCategories != 5 {
		t.Errorf("categorias povoadas = %d, quero 5", m.highlights.TotalCategories)
	}
}

func TestBuscaFiltraEExecutaAtalhos(t *testing.T) {
	m := newTestModel(t)

	m, _ = press(t, m, "/")
	if !m.searching {
		t.Fatal("“/” não abriu a busca")
	}
	if !m.Capturing() {
		t.Fatal("Capturing = false durante a busca; “q” fecharia o programa")
	}

	// "literal" só casa pelas keywords, que é o caminho que a busca precisa
	// cobrir.
	m = typeText(t, m, "literal")
	if m.results.Total == 0 {
		t.Fatal("busca por “literal” não achou nada")
	}
	if got := m.results.Items[0].Tool.ID; got != "system-alpha" {
		t.Errorf("primeiro resultado = %s, quero system-alpha", got)
	}

	m, _ = press(t, m, "esc")
	if m.searching || m.input.Value() != "" {
		t.Error("esc não saiu da busca nem limpou o campo")
	}
}

func TestBuscaAceitaFiltroInline(t *testing.T) {
	m := newTestModel(t)
	m, _ = press(t, m, "/")
	m = typeText(t, m, "cat:ai")

	// O fixture possui uma única extensão na categoria de IA.
	if m.results.Total != 1 {
		t.Fatalf("filtro cat:ai devolveu %d, quero 1", m.results.Total)
	}
	got := map[string]bool{}
	for _, item := range m.results.Items {
		got[string(item.Tool.ID)] = true
	}
	for _, want := range []string{"ai-alpha"} {
		if !got[want] {
			t.Errorf("cat:ai não trouxe %s (veio %v)", want, got)
		}
	}
}

func TestBuscaDescartaRespostaObsoleta(t *testing.T) {
	m := newTestModel(t)
	m, _ = press(t, m, "/")

	// Guarda o comando da primeira tecla e só o entrega depois de mais
	// teclas: simula a resposta lenta de uma busca já superada.
	m, stale := press(t, m, "g")
	m = typeText(t, m, "it")
	after := m.results.Total

	m = apply(t, m, stale)
	if m.results.Total != after {
		t.Errorf("resposta obsoleta sobrescreveu os resultados: %d → %d", after, m.results.Total)
	}
}

func TestNavegacaoEntrePaineis(t *testing.T) {
	m := newTestModel(t)
	start := m.focus

	m, _ = press(t, m, "tab")
	if m.focus == start {
		t.Error("tab não trocou o painel focado")
	}

	// O cursor de cada zona é independente: mover em uma não mexe na outra.
	m.focus = zoneSuggested
	m, _ = press(t, m, "down")
	if m.cursor[zoneSuggested] != 1 {
		t.Errorf("cursor de sugeridas = %d, quero 1", m.cursor[zoneSuggested])
	}
	if m.cursor[zoneFavorites] != 0 {
		t.Errorf("cursor de favoritas mudou junto: %d", m.cursor[zoneFavorites])
	}
}

func TestRecentesExcedentesVaoParaSugeridas(t *testing.T) {
	m := newTestModel(t)
	highlights := m.highlights
	highlights.Recent = make([]domain.Match, 5)
	for i := range highlights.Recent {
		highlights.Recent[i].Tool.ID = domain.ToolID(string(rune('a' + i)))
	}

	// Remove o item sintético "Todas": catalogMsg recebe apenas as
	// categorias reais devolvidas pela porta.
	categories := append([]inbound.CategoryView(nil), m.categories[1:]...)
	next, _ := m.Update(catalogMsg{highlights: highlights, categories: categories})
	m = next.(*Model)

	if got := len(m.highlights.Recent); got != recentLimit {
		t.Fatalf("recentes = %d, quero %d", got, recentLimit)
	}
	if got := m.highlights.Suggested[0].Tool.ID; got != "d" {
		t.Errorf("primeira sugerida = %q, quero o quarto recente", got)
	}
	if got := m.highlights.Suggested[1].Tool.ID; got != "e" {
		t.Errorf("segunda sugerida = %q, quero o quinto recente", got)
	}
}

func TestBuscaEAcessivelSomenteComSetas(t *testing.T) {
	m := newTestModel(t)
	m.focus = zoneSuggested
	m.cursor[zoneSuggested] = 0

	m, _ = press(t, m, "up")
	if m.focus != zoneSearch {
		t.Fatalf("subir do primeiro item focou %d, quero a busca", m.focus)
	}

	m, _ = press(t, m, "enter")
	if !m.searching || !m.Capturing() {
		t.Fatal("Enter na barra não ativou a digitação")
	}

	m, _ = press(t, m, "esc")
	if m.focus != zoneSearch {
		t.Fatalf("Esc saiu da busca para %d, quero manter a barra focada", m.focus)
	}

	m, _ = press(t, m, "down")
	if m.focus != zoneSuggested {
		t.Fatalf("descer da busca focou %d, quero o catálogo", m.focus)
	}
}

func TestSidebarFiltraCatalogoAoMoverSelecao(t *testing.T) {
	m := newTestModel(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 150, Height: 38})
	m = next.(*Model)
	m.focus = zoneSidebar

	// "Todas" ocupa a primeira linha; a seta já seleciona Sistema e dispara
	// a consulta sem exigir Enter.
	var cmd tea.Cmd
	m, cmd = press(t, m, "down")
	if cmd == nil {
		t.Fatal("mover para Sistema não disparou o filtro")
	}
	m = apply(t, m, cmd)

	category, ok := m.selectedCategory()
	if !ok || category.ID != "system" {
		t.Fatalf("categoria selecionada = %q, quero system", category.ID)
	}
	if m.filterPage.Total != 2 {
		t.Fatalf("Sistema trouxe %d tools, quero 2", m.filterPage.Total)
	}
	for _, item := range m.filterPage.Items {
		if item.Tool.Category != category.ID {
			t.Errorf("filtro system deixou passar %s (%s)", item.Tool.ID, item.Tool.Category)
		}
	}

	m, cmd = press(t, m, "down")
	m = apply(t, m, cmd)
	category, ok = m.selectedCategory()
	if !ok || category.ID != "ai" {
		t.Fatalf("segunda seleção = %q, quero ai", category.ID)
	}
	for _, item := range m.filterPage.Items {
		if item.Tool.Category != category.ID {
			t.Errorf("filtro ai deixou passar %s (%s)", item.Tool.ID, item.Tool.Category)
		}
	}
}

func TestCursorNaoEstouraEmPainelVazio(t *testing.T) {
	m := newTestModel(t)
	m.focus = zoneFavorites // vazio na primeira execução

	for range 5 {
		m, _ = press(t, m, "down")
	}
	if m.cursor[zoneFavorites] != 0 {
		t.Errorf("cursor = %d em painel vazio, quero 0", m.cursor[zoneFavorites])
	}
	if _, ok := m.selectedTool(); ok {
		t.Error("selectedTool devolveu tool em painel vazio")
	}
}

func TestViewNuncaEstouraOFrame(t *testing.T) {
	sizes := []tui.Frame{
		{Width: 200, Height: 60},
		{Width: 150, Height: 44},
		{Width: 120, Height: 36},
		{Width: 100, Height: 30},
		{Width: 94, Height: 26},
		{Width: 80, Height: 24},
		{Width: 60, Height: 20},
		{Width: 40, Height: 14},
		{Width: 30, Height: 10},
		{Width: 25, Height: 7},
	}

	modes := map[string]func(*testing.T, *Model) *Model{
		"navegação": func(_ *testing.T, m *Model) *Model { return m },
		"busca": func(t *testing.T, m *Model) *Model {
			m, _ = press(t, m, "/")
			return typeText(t, m, "sistema")
		},
		"categoria": func(t *testing.T, m *Model) *Model {
			m.focus = zoneSidebar
			var cmd tea.Cmd
			m, cmd = press(t, m, "down")
			return apply(t, m, cmd)
		},
	}

	for name, setup := range modes {
		for _, f := range sizes {
			t.Run(name, func(t *testing.T) {
				m := setup(t, newTestModel(t))
				next, _ := m.Update(tea.WindowSizeMsg{Width: f.Width, Height: f.Height})
				m = next.(*Model)

				out := m.View(f)
				lines := strings.Split(out, "\n")

				if len(lines) > f.Height {
					t.Errorf("%dx%d [%s]: %d linhas excedem a altura",
						f.Width, f.Height, name, len(lines))
				}
				for i, line := range lines {
					if got := lipgloss.Width(line); got > f.Width {
						t.Errorf("%dx%d [%s]: linha %d tem %d colunas",
							f.Width, f.Height, name, i, got)
					}
				}
			})
		}
	}
}

func TestFavoritarRecarregaOsDestaques(t *testing.T) {
	m := newTestModel(t)
	m.focus = zoneSuggested

	tool, ok := m.selectedTool()
	if !ok {
		t.Fatal("nenhuma tool selecionada em sugeridas")
	}

	_, cmd := press(t, m, "f")
	if cmd == nil {
		t.Fatal("“f” não emitiu comando")
	}
	msg, isFav := cmd().(favoriteMsg)
	if !isFav {
		t.Fatalf("comando devolveu %T, quero favoriteMsg", cmd())
	}
	if msg.err != nil {
		t.Fatalf("favoritar falhou: %v", msg.err)
	}
	if msg.id != tool.ID || !msg.on {
		t.Errorf("favoriteMsg = {%s, %v}, quero {%s, true}", msg.id, msg.on, tool.ID)
	}

	// A confirmação precisa disparar a recarga, senão o painel de favoritas
	// continua mostrando o estado anterior.
	next, reload := m.Update(msg)
	m = next.(*Model)
	if reload == nil {
		t.Error("favoriteMsg não disparou recarga dos destaques")
	}
	if m.toast.text == "" {
		t.Error("favoritar não publicou mensagem de status")
	}
}

func TestSaudacaoSegueAHora(t *testing.T) {
	base := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)
	tests := map[int]string{3: "Boa madrugada", 9: "Bom dia", 15: "Boa tarde", 21: "Boa noite"}

	for hour, want := range tests {
		got := greeting(base.Add(time.Duration(hour)*time.Hour), "mateuslh")
		if !strings.HasPrefix(got, want) {
			t.Errorf("às %dh a saudação foi %q, quero prefixo %q", hour, got, want)
		}
		if !strings.HasSuffix(got, "mateuslh") {
			t.Errorf("saudação %q não inclui o usuário", got)
		}
	}
}

// stubScreen é a tela mínima que satisfaz tui.Screen. Serve só para dar à
// tool um destino dentro da TUI — o que ela desenha é irrelevante aqui.
type stubScreen struct{}

func (stubScreen) ID() tui.ScreenID                       { return "stub" }
func (stubScreen) Title() string                          { return "stub" }
func (stubScreen) Init() tea.Cmd                          { return nil }
func (s stubScreen) Update(tea.Msg) (tui.Screen, tea.Cmd) { return s, nil }
func (stubScreen) View(tui.Frame) string                  { return "" }
func (stubScreen) Hints() []tui.Hint                      { return nil }

// Uma sessão screen-v1 não passa pelo Launcher comum; a home precisa registrar
// sua abertura para alimentar recentes e sugestões.
func TestAbrirToolInterativaAlimentaRecentes(t *testing.T) {
	const id = "system-alpha"
	m := newTestModel(t)

	if len(m.highlights.Recent) != 0 {
		t.Fatalf("recentes começou com %d itens, quero 0", len(m.highlights.Recent))
	}

	// Na primeira execução tudo está em "sugeridas": nada foi usado ainda.
	var tool domain.Tool
	for _, s := range m.highlights.Suggested {
		if s.Tool.ID == id {
			tool = s.Tool
		}
	}
	if tool.ID == "" {
		t.Fatalf("tool %s não apareceu em sugeridas", id)
	}

	// Abrir navega e contabiliza; só a contabilização interessa aqui.
	m = apply(t, m, m.openTool(tool))

	// A confirmação chega com a tela da tool já no topo, então na TUI real é
	// o Refresh do Router que recarrega a home. Reproduz-se o mesmo gesto.
	m = apply(t, m, m.Refresh())

	if len(m.highlights.Recent) != 1 {
		t.Fatalf("recentes = %d itens, quero 1", len(m.highlights.Recent))
	}
	if got := m.highlights.Recent[0].Tool.ID; got != id {
		t.Errorf("recentes[0] = %s, quero %s", got, id)
	}
}

// O App descobre o Refresh por type assertion: perder o método não quebra a
// compilação, só faz a home voltar de uma tool com os dados de antes.
func TestHomeExpoeRefresh(t *testing.T) {
	var _ interface{ Refresh() tea.Cmd } = (*Model)(nil)
}

// A loja não está no catálogo: ela chega por uma factory própria, e é o
// atalho da home que a abre.
func TestAtalhoMarketplaceAbreTelaDedicada(t *testing.T) {
	m := newTestModel(t)
	m.marketplaceScreen = func() tui.Screen { return stubScreen{} }

	_, cmd := press(t, m, "m")
	if cmd == nil {
		t.Fatal("atalho m não produziu navegação")
	}
	nav, ok := cmd().(tui.NavigateMsg)
	if !ok || nav.Screen.ID() != "stub" {
		t.Fatalf("atalho m devolveu %T", nav.Screen)
	}
}

// Sem factory ligada, o atalho não pode explodir: a engine precisa abrir
// mesmo quando o marketplace não foi composto.
func TestAtalhoMarketplaceSemFactoryNaoQuebra(t *testing.T) {
	m := newTestModel(t)
	if _, cmd := press(t, m, "m"); cmd != nil {
		t.Fatalf("atalho m produziu %T sem factory", cmd())
	}
}

type requirementCheckerStub struct{ missing []domain.Requirement }

func (s requirementCheckerStub) Missing(
	context.Context,
	domain.ToolID,
) ([]domain.Requirement, error) {
	return s.missing, nil
}

func TestToolComRequisitoAusenteAbreDiagnostico(t *testing.T) {
	const id = domain.ToolID("development-alpha")
	m := newTestModel(t)
	m.prereqs = requirementCheckerStub{missing: []domain.Requirement{
		{Executable: "gh", Name: "GitHub CLI"},
	}}

	var tool domain.Tool
	for _, candidate := range m.highlights.Suggested {
		if candidate.Tool.ID == id {
			tool = candidate.Tool
			break
		}
	}
	if tool.ID == "" {
		t.Fatal("development-alpha não apareceu no catálogo")
	}

	check := m.openTool(tool)
	checkMsg := check()
	msg, ok := checkMsg.(requirementsMsg)
	if !ok {
		t.Fatalf("checagem devolveu %T", checkMsg)
	}
	_, navigate := m.Update(msg)
	if navigate == nil {
		t.Fatal("requisito ausente não abriu diagnóstico")
	}
	navigateMsg := navigate()
	nav, ok := navigateMsg.(tui.NavigateMsg)
	if !ok {
		t.Fatalf("comando devolveu %T", navigateMsg)
	}
	if !strings.Contains(string(nav.Screen.ID()), "requirements") {
		t.Fatalf("tela = %s", nav.Screen.ID())
	}
}

// fakeMarketplace alimenta a vitrine sem rede. Só Catalog importa aqui: a
// home nunca instala nem mexe em origens — ela apenas mostra e encaminha.
type fakeMarketplace struct{ catalog coremarket.Catalog }

func (f fakeMarketplace) Catalog(context.Context) (coremarket.Catalog, error) {
	return f.catalog, nil
}
func (f fakeMarketplace) List(context.Context) ([]coremarket.Listing, error) {
	return f.catalog.Tools, nil
}
func (fakeMarketplace) Install(context.Context, string) (toolinstall.Installation, error) {
	return toolinstall.Installation{}, nil
}
func (fakeMarketplace) Sources(context.Context) ([]coremarket.Origin, error) { return nil, nil }
func (fakeMarketplace) AddSource(context.Context, coremarket.Origin) error   { return nil }
func (fakeMarketplace) RemoveSource(context.Context, string) error           { return nil }
func (fakeMarketplace) SetSourceEnabled(context.Context, string, bool) error { return nil }

func marketListing(id, name string, installed string, update bool) coremarket.Listing {
	return coremarket.Listing{
		Entry: coremarket.Entry{
			ID: id, Name: name, Version: "2.0.0", Glyph: "◈",
			Summary:          "Resumo de " + name + ".",
			DistributionTier: coremarket.ChannelCommunity,
			Origin:           coremarket.Origin{Name: "meu-repo", Kind: coremarket.OriginLocal},
		},
		InstalledVersion: installed,
		UpdateAvailable:  update,
	}
}

// loadVitrine roda a carga do marketplace de forma síncrona.
func loadVitrine(t *testing.T, catalog coremarket.Catalog) *Model {
	t.Helper()
	m := newTestModel(t)
	m.marketplace = fakeMarketplace{catalog: catalog}
	next, _ := m.Update(m.loadMarketplace()())
	return next.(*Model)
}

func TestVitrineDestacaOQuePedeAcaoAntesDoRestante(t *testing.T) {
	m := loadVitrine(t, coremarket.Catalog{
		Tools: []coremarket.Listing{
			marketListing("em-dia", "Em Dia", "2.0.0", false),
			marketListing("nova", "Nova", "", false),
			marketListing("desatualizada", "Desatualizada", "1.0.0", true),
			marketListing("outra-nova", "Outra Nova", "", false),
		},
		Sources: []coremarket.SourceStatus{{Origin: coremarket.Origin{Name: "meu-repo"}, Tools: 4}},
	})

	view := m.viewMarketplace(m.deps.Theme, 70, 6)
	desatualizada := strings.Index(view, "Desatualizada")
	emDia := strings.Index(view, "Em Dia")
	switch {
	case desatualizada < 0:
		t.Fatalf("a tool com atualização não apareceu na vitrine:\n%s", view)
	case emDia >= 0 && emDia < desatualizada:
		t.Fatalf("o que já está em dia passou na frente da atualização:\n%s", view)
	}
	if !strings.Contains(view, "1 para atualizar") {
		t.Fatalf("o resumo não anunciou a atualização:\n%s", view)
	}
	if !strings.Contains(view, "outras no catálogo") {
		t.Fatalf("a vitrine escondeu o que não coube:\n%s", view)
	}
}

func TestVitrineAnunciaOrigemForaDoArSemEsconderOCatalogo(t *testing.T) {
	m := loadVitrine(t, coremarket.Catalog{
		Tools: []coremarket.Listing{marketListing("nova", "Nova", "", false)},
		Sources: []coremarket.SourceStatus{
			{Origin: coremarket.Origin{Name: "meu-repo"}, Tools: 1},
			{Origin: coremarket.Origin{Name: "offline"}, Err: context.DeadlineExceeded},
		},
	})

	view := m.viewMarketplace(m.deps.Theme, 70, 6)
	if !strings.Contains(view, "Nova") {
		t.Fatalf("a tool da origem saudável sumiu:\n%s", view)
	}
	if !strings.Contains(view, "1 origem fora do ar") {
		t.Fatalf("a origem indisponível não foi anunciada:\n%s", view)
	}
}

// A vitrine é alcançável pelo teclado e abre com Enter — sem depender do
// atalho global, que ninguém descobre sozinho.
func TestVitrineRecebeFocoEAbreComEnter(t *testing.T) {
	m := loadVitrine(t, coremarket.Catalog{
		Tools: []coremarket.Listing{marketListing("nova", "Nova", "", false)},
	})
	m.marketplaceScreen = func() tui.Screen { return stubScreen{} }
	m.width, m.height = 150, 44

	// Descer da busca leva à vitrine antes dos painéis de destaque.
	m.focus = zoneSearch
	m, _ = press(t, m, "down")
	if m.focus != zoneMarketplace {
		t.Fatalf("foco = %v, quero a vitrine", m.focus)
	}
	if view := m.viewMarketplace(m.deps.Theme, 70, marketplacePanelHeight); !strings.Contains(view, "↵ abrir loja") {
		t.Fatalf("a vitrine focada não anunciou o Enter:\n%s", view)
	}

	_, cmd := press(t, m, "enter")
	if cmd == nil {
		t.Fatal("Enter na vitrine não abriu a loja")
	}
	if nav, ok := cmd().(tui.NavigateMsg); !ok || nav.Screen.ID() != "stub" {
		t.Fatalf("Enter devolveu %T", cmd())
	}

	// E continua saindo: descer entrega o foco aos destaques.
	m, _ = press(t, m, "down")
	if m.focus == zoneMarketplace {
		t.Fatal("a vitrine prendeu o foco")
	}
}

// Quando a vitrine não está desenhada, o foco não pode ficar preso nela.
func TestFocoSaiDaVitrineQuandoElaNaoCabe(t *testing.T) {
	m := loadVitrine(t, coremarket.Catalog{
		Tools: []coremarket.Listing{marketListing("nova", "Nova", "", false)},
	})
	m.width, m.height = 150, 44
	m.focus = zoneMarketplace

	next, _ := m.Update(tea.WindowSizeMsg{Width: 150, Height: 20})
	if got := next.(*Model).focus; got == zoneMarketplace {
		t.Fatalf("foco continuou na vitrine em janela baixa: %v", got)
	}
}

// panelTitles lê da tela quais painéis de destaque foram desenhados. O teste
// confere contra o render de verdade, e não contra o cálculo do layout: era
// justamente a divergência entre os dois que produzia o bug.
func panelTitles(view string) map[zone]bool {
	drawn := map[zone]bool{}
	for title, z := range map[string]zone{
		"FAVORITAS": zoneFavorites, "RECENTES": zoneRecent, "SUGERIDAS": zoneSuggested,
	} {
		if strings.Contains(view, title) {
			drawn[z] = true
		}
	}
	return drawn
}

// Numa janela baixa o layout descarta painéis. O foco não pode continuar em
// um deles: as setas moveriam um cursor que ninguém vê.
func TestFocoNaoFicaEmPainelQueNaoCoubeNaTela(t *testing.T) {
	// Nesta altura só "sugeridas" cabe; favoritas e recentes ficam de fora.
	const width, height = 60, 14
	m := newTestModel(t)
	m.width, m.height = width, height
	m.focus = zoneRecent

	drawn := panelTitles(m.View(tui.Frame{Width: width, Height: height}))
	if drawn[zoneRecent] || len(drawn) == 0 {
		t.Fatalf("geometria inesperada: desenhados = %v", drawn)
	}

	m, _ = press(t, m, "down")
	if !drawn[m.focus] {
		t.Fatalf("foco = %v, mas os painéis desenhados são %v", m.focus, drawn)
	}
}

// O Enter também não pode abrir a tool de um painel invisível.
func TestEnterNaoAbreToolDePainelInvisivel(t *testing.T) {
	const width, height = 60, 14
	m := newTestModel(t)
	m.width, m.height = width, height
	m.focus = zoneFavorites

	drawn := panelTitles(m.View(tui.Frame{Width: width, Height: height}))
	if drawn[zoneFavorites] {
		t.Fatalf("geometria inesperada: favoritas foi desenhada")
	}

	m, _ = press(t, m, "enter")
	if m.focus == zoneFavorites {
		t.Fatal("o foco continuou no painel que não foi desenhado")
	}
}

// Em janela larga os três painéis continuam alcançáveis: a correção não pode
// encolher a navegação normal.
func TestJanelaGrandeMantemOsTresPaineisNavegaveis(t *testing.T) {
	m := newTestModel(t)
	m.width, m.height = 200, 60
	_ = m.View(tui.Frame{Width: 200, Height: 60})

	visited := map[zone]bool{}
	for range 6 {
		m.cycleZone(1)
		visited[m.focus] = true
	}
	for _, z := range panelZones {
		if !visited[z] {
			t.Fatalf("zona %v ficou fora do ciclo do Tab: %v", z, visited)
		}
	}
}

// Sumir com um painel em silêncio é o que fazia a home parecer incompleta
// sem explicação. A moldura do último painel anuncia o que ficou de fora.
func TestPaineisOmitidosSaoAnunciadosNaMoldura(t *testing.T) {
	const width, height = 60, 14
	m := newTestModel(t)
	m.width, m.height = width, height

	view := m.View(tui.Frame{Width: width, Height: height})
	if !strings.Contains(view, "painéis ocultos") {
		t.Fatalf("a home escondeu painéis sem avisar:\n%s", view)
	}

	// Na janela larga, onde tudo cabe, o aviso não aparece.
	m.width, m.height = 200, 60
	if wide := m.View(tui.Frame{Width: 200, Height: 60}); strings.Contains(wide, "oculto") {
		t.Fatalf("aviso de painel oculto apareceu com a janela cheia:\n%s", wide)
	}
}

// O atalho c abre a configuração, que também não é uma tool do catálogo.
func TestAtalhoConfiguracaoAbreTelaDedicada(t *testing.T) {
	m := newTestModel(t)
	m.settingsScreen = func() tui.Screen { return stubScreen{} }

	_, cmd := press(t, m, "c")
	if cmd == nil {
		t.Fatal("atalho c não produziu navegação")
	}
	if nav, ok := cmd().(tui.NavigateMsg); !ok || nav.Screen.ID() != "stub" {
		t.Fatalf("atalho c devolveu %T", cmd())
	}

	sem := newTestModel(t)
	if _, cmd := press(t, sem, "c"); cmd != nil {
		t.Fatalf("atalho c produziu %T sem factory", cmd())
	}
}

// A saudação e a consulta da vitrine são lidas a cada uso, para que mudá-las
// na configuração valha ao voltar para a home.
func TestConfiguracaoMudaSaudacaoEConsultaSemReiniciar(t *testing.T) {
	name := "Chefia"
	consulta := false
	m := newTestModel(t)
	m.marketplace = fakeMarketplace{}
	m.greetingName = func() string { return name }
	m.marketplaceOnHome = func() bool { return consulta }

	if !strings.Contains(m.View(tui.Frame{Width: 150, Height: 44}), "Chefia") {
		t.Fatal("a saudação não usou o nome configurado")
	}
	name = "Outro"
	if !strings.Contains(m.View(tui.Frame{Width: 150, Height: 44}), "Outro") {
		t.Fatal("a saudação não acompanhou a mudança")
	}

	if m.marketplaceEnabled() {
		t.Fatal("a vitrine consultou a rede com o interruptor desligado")
	}
	consulta = true
	if !m.marketplaceEnabled() {
		t.Fatal("a vitrine não voltou a consultar quando religada")
	}
}
