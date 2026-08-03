// Package marketplace implementa a loja nativa da engine.
//
// A tela conversa somente com o caso de uso do core; HTTP, disco e instalação
// permanecem atrás das portas de saída. Ela tem duas faces: o catálogo de
// tools e a lista de origens — porque o marketplace do lealing não é um
// endereço, é a soma dos repositórios que o usuário resolveu confiar.
package marketplace

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/core/domain"
	coremarket "github.com/mateuslh/lealing/internal/core/marketplace"
	"github.com/mateuslh/lealing/internal/core/toolinstall"
	"github.com/mateuslh/lealing/internal/core/toolmanage"
)

// tab é a face visível da tela.
type tab int

const (
	tabTools tab = iota
	tabManage
	tabSources
)

// filter recorta o catálogo por estado de instalação. É a pergunta que o
// usuário faz com mais frequência ao abrir a loja — "o que eu já tenho?" e
// "o que mudou?" — e responder por teclado evita transformar a busca em
// linguagem de consulta.
type filter int

const (
	filterAll filter = iota
	filterUpdates
	filterInstalled
	filterAvailable
)

func (f filter) label() string {
	switch f {
	case filterUpdates:
		return "com atualização"
	case filterInstalled:
		return "instaladas"
	case filterAvailable:
		return "não instaladas"
	default:
		return "todas"
	}
}

func (f filter) next() filter {
	if f == filterAvailable {
		return filterAll
	}
	return f + 1
}

// formField numera os campos do cadastro de origem.
type formField int

const (
	fieldRef formField = iota
	fieldName
	formFields
)

// sourceForm é o cadastro de um repositório paralelo. Mora na tela porque é
// puro estado de digitação: o core recebe apenas o resultado já interpretado.
type sourceForm struct {
	inputs [formFields]textinput.Model
	focus  formField
	err    error
}

type Model struct {
	deps    tui.Deps
	manager coremarket.Manager
	tools   toolmanage.Manager

	tab     tab
	catalog coremarket.Catalog
	visible []coremarket.Listing
	origins []coremarket.Origin
	managed []toolmanage.Item

	toolCursor   int
	manageCursor int
	sourceCursor int

	searching bool
	query     textinput.Model
	filter    filter

	// sheetOpen entrega a largura inteira à ficha da tool. É o modo de
	// leitura: em janela estreita a lista e a ficha não cabem lado a lado, e
	// espremer as duas deixaria a descrição ilegível justamente na tela em
	// que ela é a informação principal.
	sheetOpen   bool
	sheetScroll int

	form *sourceForm
	// pendingRemoval guarda a origem aguardando confirmação. Remover um
	// repositório não apaga arquivo nenhum, então o diálogo global de ações
	// destrutivas seria pesado demais; ainda assim, um "d" acidental não
	// pode descartar um cadastro em silêncio.
	pendingRemoval string
	// pendingUninstall guarda uma remoção de pacote aguardando confirmação.
	// Builtins não entram neste estado: elas são desativadas, nunca fingem
	// possuir um artefato separado que poderia ser apagado.
	pendingUninstall *toolmanage.Item

	loading    bool
	installing string
	err        error
	message    string
	success    bool
}

var _ tui.Screen = (*Model)(nil)

func New(deps tui.Deps, manager coremarket.Manager, tools toolmanage.Manager) *Model {
	query := textinput.New()
	query.Prompt = ""
	query.Placeholder = "buscar por nome, publicador ou origem"
	query.CharLimit = 96

	return &Model{deps: deps, manager: manager, tools: tools, loading: true, query: query}
}

func (*Model) ID() tui.ScreenID { return "tool/marketplace" }
func (*Model) Title() string    { return "marketplace" }

func (m *Model) Init() tea.Cmd { return m.reload() }

// Capturing impede que os atalhos globais consumam letras da busca ou do
// formulário de origem.
func (m *Model) Capturing() bool { return m.searching || m.form != nil }

type catalogMsg struct {
	catalog coremarket.Catalog
	err     error
}

type sourcesMsg struct {
	origins []coremarket.Origin
	err     error
}

type installedMsg struct {
	installation toolinstall.Installation
	err          error
}

type managedMsg struct {
	items []toolmanage.Item
	err   error
}

type toolChangedMsg struct {
	message string
	err     error
}

// sourceChangedMsg fecha o ciclo de uma mutação de origem: a lista só é
// recarregada depois que a escrita terminou, para a tela nunca exibir um
// cadastro que ainda não está no disco.
type sourceChangedMsg struct {
	message string
	err     error
}

func (m *Model) reload() tea.Cmd {
	return tea.Batch(m.loadCatalog(), m.loadManaged(), m.loadSources())
}

func (m *Model) loadCatalog() tea.Cmd {
	manager := m.manager
	return func() tea.Msg {
		if manager == nil {
			return catalogMsg{err: errNotConfigured}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		catalog, err := manager.Catalog(ctx)
		return catalogMsg{catalog: catalog, err: err}
	}
}

// loadSources é separado do catálogo porque as origens desabilitadas não
// aparecem na consulta: elas não são visitadas, mas continuam cadastradas e
// precisam ser listadas para que dê para religá-las.
func (m *Model) loadSources() tea.Cmd {
	manager := m.manager
	return func() tea.Msg {
		if manager == nil {
			return sourcesMsg{err: errNotConfigured}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		origins, err := manager.Sources(ctx)
		return sourcesMsg{origins: origins, err: err}
	}
}

func (m *Model) loadManaged() tea.Cmd {
	manager := m.tools
	return func() tea.Msg {
		if manager == nil {
			return managedMsg{err: errors.New("gerenciamento de tools não configurado")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		items, err := manager.List(ctx)
		return managedMsg{items: items, err: err}
	}
}

func (m *Model) install(ref string) tea.Cmd {
	manager := m.manager
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		installation, err := manager.Install(ctx, ref)
		return installedMsg{installation: installation, err: err}
	}
}

func (m *Model) addSource(origin coremarket.Origin) tea.Cmd {
	manager := m.manager
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := manager.AddSource(ctx, origin); err != nil {
			return sourceChangedMsg{err: err}
		}
		return sourceChangedMsg{message: "origem " + origin.Name + " cadastrada"}
	}
}

func (m *Model) removeSource(name string) tea.Cmd {
	manager := m.manager
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := manager.RemoveSource(ctx, name); err != nil {
			return sourceChangedMsg{err: err}
		}
		return sourceChangedMsg{message: "origem " + name + " removida"}
	}
}

func (m *Model) toggleSource(name string, enabled bool) tea.Cmd {
	manager := m.manager
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := manager.SetSourceEnabled(ctx, name, enabled); err != nil {
			return sourceChangedMsg{err: err}
		}
		state := "habilitada"
		if !enabled {
			state = "desabilitada"
		}
		return sourceChangedMsg{message: "origem " + name + " " + state}
	}
}

func (m *Model) toggleTool(id domain.ToolID, enabled bool) tea.Cmd {
	manager := m.tools
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := manager.SetEnabled(ctx, id, enabled); err != nil {
			return toolChangedMsg{err: err}
		}
		state := "ativada"
		if !enabled {
			state = "desativada"
		}
		return toolChangedMsg{message: string(id) + " " + state}
	}
}

func (m *Model) uninstallTool(id domain.ToolID) tea.Cmd {
	manager := m.tools
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		removed, err := manager.Remove(ctx, id)
		if err != nil {
			return toolChangedMsg{err: err}
		}
		return toolChangedMsg{message: removed.ID + " desinstalada; recuperação em " + removed.RecoveryDir}
	}
}

// applyFilters recalcula a lista visível e mantém o cursor sobre a mesma tool
// sempre que ela sobrevive ao novo recorte.
func (m *Model) applyFilters() {
	selected := ""
	if current, ok := m.currentTool(); ok {
		selected = current.Ref()
	}

	terms := strings.Fields(strings.ToLower(m.query.Value()))
	visible := make([]coremarket.Listing, 0, len(m.catalog.Tools))
	for _, listing := range m.catalog.Tools {
		if !matchesFilter(listing, m.filter) || !matchesTerms(listing, terms) {
			continue
		}
		visible = append(visible, listing)
	}
	m.visible = visible

	m.toolCursor = 0
	for index, listing := range visible {
		if listing.Ref() == selected {
			m.toolCursor = index
			break
		}
	}
}

func matchesFilter(listing coremarket.Listing, active filter) bool {
	switch active {
	case filterUpdates:
		return listing.UpdateAvailable
	case filterInstalled:
		return listing.InstalledVersion != ""
	case filterAvailable:
		return listing.InstalledVersion == ""
	default:
		return true
	}
}

// matchesTerms exige todos os termos, cada um em qualquer campo: é o que faz
// "example lealing" encontrar a extensão publicada pela origem padrão.
func matchesTerms(listing coremarket.Listing, terms []string) bool {
	if len(terms) == 0 {
		return true
	}
	haystack := strings.ToLower(strings.Join([]string{
		listing.Name, listing.ID, listing.Summary, listing.Publisher,
		listing.Category, listing.Origin.Name, listing.Origin.Label,
	}, " "))
	for _, term := range terms {
		if !strings.Contains(haystack, term) {
			return false
		}
	}
	return true
}

func (m *Model) currentTool() (coremarket.Listing, bool) {
	if m.toolCursor < 0 || m.toolCursor >= len(m.visible) {
		return coremarket.Listing{}, false
	}
	return m.visible[m.toolCursor], true
}

func (m *Model) currentSource() (coremarket.Origin, bool) {
	if m.sourceCursor < 0 || m.sourceCursor >= len(m.origins) {
		return coremarket.Origin{}, false
	}
	return m.origins[m.sourceCursor], true
}

func (m *Model) currentManaged() (toolmanage.Item, bool) {
	if m.manageCursor < 0 || m.manageCursor >= len(m.managed) {
		return toolmanage.Item{}, false
	}
	return m.managed[m.manageCursor], true
}

// sourceStatus liga uma origem ao resultado da última consulta. Origens
// desabilitadas não têm status porque não foram visitadas.
func (m *Model) sourceStatus(name string) (coremarket.SourceStatus, bool) {
	for _, status := range m.catalog.Sources {
		if status.Name == name {
			return status, true
		}
	}
	return coremarket.SourceStatus{}, false
}

func newSourceForm() *sourceForm {
	form := &sourceForm{}
	reference := textinput.New()
	reference.Prompt = ""
	reference.Placeholder = "https://exemplo.dev/tools/index.json ou /Users/voce/tools"
	reference.CharLimit = 512
	reference.Focus()

	name := textinput.New()
	name.Prompt = ""
	name.Placeholder = "opcional — derivado do endereço"
	name.CharLimit = 64

	form.inputs[fieldRef] = reference
	form.inputs[fieldName] = name
	return form
}

func (f *sourceForm) focusOn(field formField) tea.Cmd {
	f.focus = field
	for index := range f.inputs {
		if formField(index) == field {
			f.inputs[index].Focus()
			continue
		}
		f.inputs[index].Blur()
	}
	return textinput.Blink
}
