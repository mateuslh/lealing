package marketplace

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	"github.com/mateuslh/lealing/internal/core/domain"
	coremarket "github.com/mateuslh/lealing/internal/core/marketplace"
	"github.com/mateuslh/lealing/internal/core/toolinstall"
	"github.com/mateuslh/lealing/internal/core/toolmanage"
)

type fakeManager struct {
	catalog      coremarket.Catalog
	catalogErr   error
	origins      []coremarket.Origin
	installation toolinstall.Installation
	installErr   error

	catalogs    int
	installedID string
	added       coremarket.Origin
	removed     string
	toggled     string
	toggledTo   bool
	mutationErr error
}

type fakeToolManagement struct {
	items     []toolmanage.Item
	toggled   domain.ToolID
	toggledTo bool
	removed   domain.ToolID
}

func (f *fakeToolManagement) List(context.Context) ([]toolmanage.Item, error) { return f.items, nil }
func (f *fakeToolManagement) SetEnabled(_ context.Context, id domain.ToolID, enabled bool) error {
	f.toggled, f.toggledTo = id, enabled
	return nil
}
func (f *fakeToolManagement) Remove(_ context.Context, id domain.ToolID) (toolinstall.Removal, error) {
	f.removed = id
	return toolinstall.Removal{ID: string(id), RecoveryDir: "/tools/.trash/" + string(id)}, nil
}

func (f *fakeManager) Catalog(context.Context) (coremarket.Catalog, error) {
	f.catalogs++
	return f.catalog, f.catalogErr
}

func (f *fakeManager) List(ctx context.Context) ([]coremarket.Listing, error) {
	catalog, err := f.Catalog(ctx)
	return catalog.Tools, err
}

func (f *fakeManager) Install(_ context.Context, ref string) (toolinstall.Installation, error) {
	f.installedID = ref
	return f.installation, f.installErr
}

func (f *fakeManager) Sources(context.Context) ([]coremarket.Origin, error) {
	return f.origins, nil
}

func (f *fakeManager) AddSource(_ context.Context, origin coremarket.Origin) error {
	f.added = origin
	return f.mutationErr
}

func (f *fakeManager) RemoveSource(_ context.Context, name string) error {
	f.removed = name
	return f.mutationErr
}

func (f *fakeManager) SetSourceEnabled(_ context.Context, name string, enabled bool) error {
	f.toggled, f.toggledTo = name, enabled
	return f.mutationErr
}

func fixture() coremarket.Listing {
	return coremarket.Listing{Entry: coremarket.Entry{
		ID: "example-tool", Version: "1.0.0", Name: "Example Tool",
		Summary: "Demonstra uma extensão externa.", Publisher: "example",
		DistributionTier: coremarket.ChannelOfficial, Risk: "safe", Glyph: "✧",
		Protocol: coremarket.VersionRange{Min: 1, Max: 1},
		Origin:   officialOrigin(),
	}}
}

func officialOrigin() coremarket.Origin {
	return coremarket.Origin{
		Name: "lealing", Label: "índice padrão", Kind: coremarket.OriginRemote,
		Ref: "https://example.test/index.json", Trusted: true, Builtin: true, Enabled: true,
	}
}

func customOrigin() coremarket.Origin {
	return coremarket.Origin{
		Name: "meu-repo", Kind: coremarket.OriginLocal,
		Ref: "/Users/alguem/dev/tools", Enabled: true, Priority: 1,
	}
}

func testDeps() tui.Deps { return tui.Deps{Theme: theme.Default()} }

// loaded devolve a tela já com catálogo e origens carregados, que é o estado
// em que quase todo caso interessante começa.
func loaded(t *testing.T, manager *fakeManager) *Model {
	t.Helper()
	model := New(testDeps(), manager, &fakeToolManagement{})
	for _, command := range []tea.Cmd{model.loadCatalog(), model.loadManaged(), model.loadSources()} {
		model, _ = updateAsModel(t, model, command())
	}
	return model
}

func TestEstadosLoadingRunningEError(t *testing.T) {
	manager := &fakeManager{catalog: coremarket.Catalog{
		Tools:   []coremarket.Listing{fixture()},
		Sources: []coremarket.SourceStatus{{Origin: officialOrigin(), Tools: 1}},
	}}
	model := New(testDeps(), manager, &fakeToolManagement{})
	if !strings.Contains(model.View(tui.Frame{Width: 80, Height: 20}), "consultando") {
		t.Fatal("loading não foi renderizado")
	}

	model = loaded(t, manager)
	if manager.catalogs != 1 {
		t.Fatalf("Catalog = %d", manager.catalogs)
	}
	if view := model.View(tui.Frame{Width: 80, Height: 20}); !strings.Contains(view, "Example Tool") {
		t.Fatalf("catálogo não foi renderizado: %q", view)
	}

	broken := loaded(t, &fakeManager{catalogErr: errors.New("sem rede")})
	if !strings.Contains(broken.View(tui.Frame{Width: 60, Height: 12}), "sem rede") {
		t.Fatal("erro de carga não foi renderizado")
	}
}

func TestEnterAbreConfirmacaoGlobalSemInstalarDentroDeUpdate(t *testing.T) {
	manager := &fakeManager{catalog: coremarket.Catalog{Tools: []coremarket.Listing{fixture()}}}
	model := loaded(t, manager)

	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("enter não abriu confirmação")
	}
	if message := command(); !isNavigate(message) {
		t.Fatalf("mensagem = %T, quero NavigateMsg", message)
	}
	if manager.installedID != "" {
		t.Fatal("Update executou I/O de instalação")
	}
}

func TestInstalacaoUsaReferenciaQualificadaERelataSucesso(t *testing.T) {
	manager := &fakeManager{
		catalog:      coremarket.Catalog{Tools: []coremarket.Listing{fixture()}},
		installation: toolinstall.Installation{ID: "example-tool", Version: "1.0.0"},
	}
	model := loaded(t, manager)

	message := model.install(model.visible[0].Ref())()
	if manager.installedID != "lealing/example-tool" {
		t.Fatalf("referência instalada = %q", manager.installedID)
	}
	next, command := model.Update(message)
	view := next.View(tui.Frame{Width: 100, Height: 24})
	if command == nil || !strings.Contains(view, "example-tool@1.0.0 instalada") {
		t.Fatalf("sucesso não atualizou o estado nem pediu recarga: command=%v view=%q", command != nil, view)
	}
}

func TestBuscaEFiltroRecortamOCatalogo(t *testing.T) {
	other := fixture()
	other.ID, other.Name, other.Summary = "another-tool", "Another Tool", "Resume dados locais."
	other.InstalledVersion = "1.0.0"

	manager := &fakeManager{catalog: coremarket.Catalog{Tools: []coremarket.Listing{fixture(), other}}}
	model := loaded(t, manager)

	model, _ = updateAsModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	for _, symbol := range "another" {
		model, _ = updateAsModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{symbol}})
	}
	if len(model.visible) != 1 || model.visible[0].ID != "another-tool" {
		t.Fatalf("busca = %+v", model.visible)
	}

	model, _ = updateAsModel(t, model, tea.KeyMsg{Type: tea.KeyEsc})
	model, _ = updateAsModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	if model.filter != filterUpdates || len(model.visible) != 0 {
		t.Fatalf("filtro = %v, visíveis = %d", model.filter, len(model.visible))
	}
}

func TestAbaDeOrigensLigaDesligaERemove(t *testing.T) {
	manager := &fakeManager{origins: []coremarket.Origin{officialOrigin(), customOrigin()}}
	model := loaded(t, manager)

	model, _ = updateAsModel(t, model, tea.KeyMsg{Type: tea.KeyTab})
	model, _ = updateAsModel(t, model, tea.KeyMsg{Type: tea.KeyTab})
	if model.tab != tabSources {
		t.Fatal("tab não alternou para origens")
	}
	model, _ = updateAsModel(t, model, tea.KeyMsg{Type: tea.KeyDown})

	_, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if command == nil {
		t.Fatal("espaço não alternou a origem")
	}
	command()
	if manager.toggled != "meu-repo" || manager.toggledTo {
		t.Fatalf("toggle = %q → %v", manager.toggled, manager.toggledTo)
	}

	model, _ = updateAsModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if model.pendingRemoval != "meu-repo" {
		t.Fatalf("remoção pendente = %q", model.pendingRemoval)
	}
	if !strings.Contains(model.View(tui.Frame{Width: 100, Height: 28}), "Remover a origem") {
		t.Fatal("confirmação de remoção não apareceu")
	}
	_, command = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if command == nil {
		t.Fatal("confirmação não disparou a remoção")
	}
	command()
	if manager.removed != "meu-repo" {
		t.Fatalf("origem removida = %q", manager.removed)
	}
}

func TestOrigemEmbutidaNaoPodeSerRemovida(t *testing.T) {
	manager := &fakeManager{origins: []coremarket.Origin{officialOrigin()}}
	model := loaded(t, manager)
	model, _ = updateAsModel(t, model, tea.KeyMsg{Type: tea.KeyTab})
	model, _ = updateAsModel(t, model, tea.KeyMsg{Type: tea.KeyTab})
	model, _ = updateAsModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})

	if model.pendingRemoval != "" || manager.removed != "" {
		t.Fatal("a origem embutida entrou em remoção")
	}
	if !strings.Contains(model.message, "não pode ser removida") {
		t.Fatalf("mensagem = %q", model.message)
	}
}

func TestAbaGerenciarAtivaDesativaEDesinstalaExterna(t *testing.T) {
	tools := &fakeToolManagement{items: []toolmanage.Item{{
		Tool:    domain.Tool{ID: "example-tool", Name: "Example Tool", Kind: domain.KindProcess},
		Enabled: true, Installed: true, ActiveVersion: "1.0.0",
	}}}
	model := New(testDeps(), &fakeManager{}, tools)
	for _, command := range []tea.Cmd{model.loadCatalog(), model.loadManaged(), model.loadSources()} {
		model, _ = updateAsModel(t, model, command())
	}

	model, _ = updateAsModel(t, model, tea.KeyMsg{Type: tea.KeyTab})
	if model.tab != tabManage || !strings.Contains(model.View(tui.Frame{Width: 100, Height: 26}), "GERENCIAR") {
		t.Fatal("aba de gerenciamento não abriu")
	}
	_, command := model.Update(tea.KeyMsg{Type: tea.KeySpace})
	if command == nil {
		t.Fatal("espaço não gerou a desativação")
	}
	command()
	if tools.toggled != "example-tool" || tools.toggledTo {
		t.Fatalf("toggle = %s → %v", tools.toggled, tools.toggledTo)
	}

	model, _ = updateAsModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if model.pendingUninstall == nil || !strings.Contains(model.View(tui.Frame{Width: 100, Height: 26}), "Desinstalar") {
		t.Fatal("desinstalação não pediu confirmação")
	}
	_, command = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	if command == nil {
		t.Fatal("confirmação não gerou comando")
	}
	command()
	if tools.removed != "example-tool" {
		t.Fatalf("removida = %s", tools.removed)
	}
}

func TestFormularioCadastraRepositorioProprio(t *testing.T) {
	manager := &fakeManager{origins: []coremarket.Origin{officialOrigin()}}
	model := loaded(t, manager)

	model, _ = updateAsModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if model.form == nil || model.tab != tabSources {
		t.Fatal("a tecla a não abriu o cadastro de origem")
	}
	if !strings.Contains(model.View(tui.Frame{Width: 110, Height: 30}), "ADICIONAR ORIGEM") {
		t.Fatal("formulário não foi renderizado")
	}

	for _, symbol := range "https://exemplo.test/tools/index.json" {
		model, _ = updateAsModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{symbol}})
	}
	// O primeiro Enter avança para o nome em vez de cadastrar.
	model, _ = updateAsModel(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if model.form == nil || model.form.focus != fieldName {
		t.Fatal("Enter no endereço não avançou para o nome")
	}

	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("cadastro não gerou comando")
	}
	command()
	if manager.added.Name != "exemplo-test" || manager.added.Kind != coremarket.OriginRemote {
		t.Fatalf("origem cadastrada = %+v", manager.added)
	}
}

func TestFormularioRecusaEnderecoInvalidoSemFechar(t *testing.T) {
	model := loaded(t, &fakeManager{})
	model, _ = updateAsModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	for _, symbol := range "ftp://exemplo.test" {
		model, _ = updateAsModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{symbol}})
	}
	model, _ = updateAsModel(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	model, _ = updateAsModel(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	if model.form == nil || model.form.err == nil {
		t.Fatal("endereço inválido não foi reportado no formulário")
	}
}

func TestOrigemForaDoArApareceSemEsconderAsDemais(t *testing.T) {
	manager := &fakeManager{
		catalog: coremarket.Catalog{
			Tools: []coremarket.Listing{fixture()},
			Sources: []coremarket.SourceStatus{
				{Origin: officialOrigin(), Tools: 1},
				{Origin: customOrigin(), Err: errors.New("índice ilegível")},
			},
		},
		origins: []coremarket.Origin{officialOrigin(), customOrigin()},
	}
	model := loaded(t, manager)

	view := model.View(tui.Frame{Width: 120, Height: 30})
	if !strings.Contains(view, "Example Tool") {
		t.Fatal("a tool da origem saudável sumiu por causa da que falhou")
	}
	if !strings.Contains(view, "indisponível") {
		t.Fatalf("a falha não foi anunciada: %q", view)
	}
	if status, _ := model.Status(); !strings.Contains(status, "fora do ar") {
		t.Fatalf("status = %q", status)
	}
}

func TestHintsIncluemEsc(t *testing.T) {
	model := New(testDeps(), &fakeManager{}, &fakeToolManagement{})
	for _, hint := range model.Hints() {
		if strings.Contains(hint.Key, "esc") {
			return
		}
	}
	t.Fatal("hint esc ausente")
}

func isNavigate(message tea.Msg) bool {
	_, ok := message.(tui.NavigateMsg)
	return ok
}

func updateAsModel(t *testing.T, model *Model, message tea.Msg) (*Model, tea.Cmd) {
	t.Helper()
	next, command := model.Update(message)
	updated, ok := next.(*Model)
	if !ok {
		t.Fatalf("model = %T", next)
	}
	return updated, command
}

func detailed() coremarket.Listing {
	listing := fixture()
	listing.Detail = "Lê dados locais de exemplo, normaliza os registros e apresenta um resumo para demonstrar a ficha."
	listing.MinimumEngine = "0.3.0"
	listing.Permissions = coremarket.Permissions{
		Filesystem: coremarket.FilesystemPermissions{Read: []string{"~/.example/data", "~/.example/cache"}},
		Network:    true,
	}
	listing.Artifacts = []coremarket.Artifact{
		{Platform: "darwin-arm64"}, {Platform: "windows-amd64"},
	}
	return listing
}

func TestFichaMostraDescricaoProcedenciaERequisitos(t *testing.T) {
	model := loaded(t, &fakeManager{catalog: coremarket.Catalog{Tools: []coremarket.Listing{detailed()}}})
	sheet := strings.Join(model.toolSheet(model.deps.Theme, detailed(), 70), "\n")

	for _, want := range []string{
		"dados locais de exemplo", // descrição longa
		"example",                 // publicador
		"official",                // canal
		"≥ 0.3.0",                 // versão mínima da engine
		"darwin-arm64",            // plataformas com artefato
		"~/.example/data",         // caminho concedido, não a contagem
		"~/.example/cache",        // cada caminho em sua linha
		"—",                       // ausência de escrita
	} {
		if !strings.Contains(sheet, want) {
			t.Errorf("a ficha não mostrou %q:\n%s", want, sheet)
		}
	}
}

func TestFichaSemDescricaoLongaExplicaAAusencia(t *testing.T) {
	model := loaded(t, &fakeManager{catalog: coremarket.Catalog{Tools: []coremarket.Listing{fixture()}}})
	sheet := strings.Join(model.toolSheet(model.deps.Theme, fixture(), 70), "\n")
	if !strings.Contains(sheet, "não publicou uma descrição longa") {
		t.Fatalf("a ficha ficou muda sobre a descrição ausente:\n%s", sheet)
	}
}

func TestSetaAbreFichaEmLarguraCheiaERolaSemPerderALista(t *testing.T) {
	model := loaded(t, &fakeManager{catalog: coremarket.Catalog{Tools: []coremarket.Listing{detailed()}}})

	model, _ = updateAsModel(t, model, tea.KeyMsg{Type: tea.KeyRight})
	if !model.sheetOpen {
		t.Fatal("→ não abriu a ficha")
	}
	view := model.View(tui.Frame{Width: 100, Height: 26})
	if strings.Contains(view, "CATÁLOGO") {
		t.Fatalf("a lista continuou dividindo a largura com a ficha:\n%s", view)
	}
	if !strings.Contains(view, "PERMISSÕES") && !strings.Contains(view, "SOBRE") {
		t.Fatalf("a ficha expandida não trouxe as seções:\n%s", view)
	}

	model, _ = updateAsModel(t, model, tea.KeyMsg{Type: tea.KeyPgDown})
	if model.sheetScroll == 0 {
		t.Fatal("⇟ não rolou a ficha")
	}
	model, _ = updateAsModel(t, model, tea.KeyMsg{Type: tea.KeyLeft})
	if model.sheetOpen {
		t.Fatal("← não devolveu o foco à lista")
	}
	if !strings.Contains(model.View(tui.Frame{Width: 100, Height: 26}), "CATÁLOGO") {
		t.Fatal("a lista não voltou")
	}
}

func TestTrocarDeToolRecomecaALeituraDaFicha(t *testing.T) {
	other := detailed()
	other.ID, other.Name = "outra", "Outra"
	model := loaded(t, &fakeManager{catalog: coremarket.Catalog{
		Tools: []coremarket.Listing{detailed(), other},
	}})

	model, _ = updateAsModel(t, model, tea.KeyMsg{Type: tea.KeyRight})
	model, _ = updateAsModel(t, model, tea.KeyMsg{Type: tea.KeyPgDown})
	model, _ = updateAsModel(t, model, tea.KeyMsg{Type: tea.KeyDown})
	if model.sheetScroll != 0 {
		t.Fatalf("scroll = %d ao trocar de tool", model.sheetScroll)
	}
}
