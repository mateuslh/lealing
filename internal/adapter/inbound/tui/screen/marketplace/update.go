package marketplace

import (
	"errors"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	confirmationscreen "github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/confirmation"
	"github.com/mateuslh/lealing/internal/core/domain"
	coremarket "github.com/mateuslh/lealing/internal/core/marketplace"
)

var errNotConfigured = errors.New("marketplace não configurado")

// sheetPage é o salto de uma rolagem da ficha. Meia tela seria mais suave,
// mas exigiria que o Update conhecesse a altura do frame.
const sheetPage = 5

// sheetBottom é um deslocamento propositalmente maior que qualquer ficha; o
// render o corta no fim real, que só ele conhece.
const sheetBottom = 1 << 20

func (m *Model) Update(message tea.Msg) (tui.Screen, tea.Cmd) {
	switch message := message.(type) {
	case catalogMsg:
		m.loading = false
		if message.err != nil {
			m.err = message.err
			// O catálogo parcial ainda vale: se três origens responderam e
			// uma caiu, esconder as três seria punir o usuário pelo servidor
			// alheio.
			m.catalog.Sources = message.catalog.Sources
			return m, nil
		}
		m.err = nil
		m.catalog = message.catalog
		m.applyFilters()
		return m, nil

	case sourcesMsg:
		if message.err != nil {
			m.err = message.err
			return m, nil
		}
		m.origins = message.origins
		m.sourceCursor = clamp(m.sourceCursor, len(m.origins))
		return m, nil

	case managedMsg:
		if message.err != nil {
			m.err = message.err
			return m, nil
		}
		m.managed = message.items
		m.manageCursor = clamp(m.manageCursor, len(m.managed))
		return m, nil

	case installedMsg:
		m.installing = ""
		if message.err != nil {
			if errors.Is(message.err, coremarket.ErrAlreadyLatest) {
				m.err, m.success = nil, false
				m.message = "a versão mais recente já está instalada"
				return m, nil
			}
			m.err, m.message = message.err, ""
			return m, nil
		}
		m.err, m.success = nil, true
		m.message = fmt.Sprintf("%s@%s instalada; catálogo atualizado",
			message.installation.ID, message.installation.Version)
		return m, m.loadCatalog()

	case sourceChangedMsg:
		if message.err != nil {
			m.err, m.message = message.err, ""
			return m, nil
		}
		m.err, m.success, m.message = nil, true, message.message
		m.loading = true
		return m, m.reload()

	case toolChangedMsg:
		if message.err != nil {
			m.err, m.message = message.err, ""
			return m, nil
		}
		m.err, m.success, m.message = nil, true, message.message
		m.loading = true
		return m, m.reload()

	case tea.KeyMsg:
		return m.handleKey(message)
	}
	return m, nil
}

func (m *Model) handleKey(key tea.KeyMsg) (tui.Screen, tea.Cmd) {
	switch {
	case m.form != nil:
		return m.handleForm(key)
	case m.pendingRemoval != "":
		return m.handleRemoval(key)
	case m.pendingUninstall != nil:
		return m.handleUninstall(key)
	case m.searching:
		return m.handleSearch(key)
	case m.installing != "":
		// Durante o download o teclado fica inerte de propósito: navegar
		// enquanto o instalador troca o diretório ativo só produziria uma
		// tela que não corresponde ao que está acontecendo.
		return m, nil
	}
	return m.handleBrowse(key)
}

func (m *Model) handleBrowse(key tea.KeyMsg) (tui.Screen, tea.Cmd) {
	switch key.String() {
	case "tab":
		m.tab, m.message, m.err = m.tab.move(1), "", nil
		m.sheetOpen = false
		return m, nil

	case "shift+tab":
		m.tab, m.message, m.err = m.tab.move(-1), "", nil
		m.sheetOpen = false
		return m, nil

	case "right", "l":
		// A seta entra na ficha; a lista continua atrás, e ← volta para ela.
		if m.tab == tabTools && len(m.visible) > 0 {
			m.sheetOpen, m.sheetScroll = true, 0
		}
		return m, nil

	case "left", "h":
		m.sheetOpen = false
		return m, nil

	case "pgup", "shift+up":
		m.sheetScroll = max(m.sheetScroll-sheetPage, 0)
		return m, nil

	case "pgdown", "shift+down":
		if m.tab == tabTools {
			m.sheetScroll += sheetPage
		}
		return m, nil

	case " ", "space":
		switch m.tab {
		case tabManage:
			return m.toggleCurrentTool()
		case tabSources:
			return m.toggleCurrentSource()
		}
		return m, nil

	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "g", "home":
		// Com a ficha aberta, o início e o fim são os do texto que está sendo
		// lido, não os da lista atrás dele.
		if m.sheetOpen {
			m.sheetScroll = 0
			return m, nil
		}
		m.setCursor(0)
	case "G", "end":
		if m.sheetOpen {
			m.sheetScroll = sheetBottom
			return m, nil
		}
		m.setCursor(m.count() - 1)

	case "r", "ctrl+r":
		m.loading, m.err, m.message = true, nil, ""
		return m, m.reload()

	case "/":
		if m.tab != tabTools {
			return m, nil
		}
		m.searching = true
		m.query.Focus()
		return m, textinput.Blink

	case "f":
		if m.tab != tabTools {
			return m, nil
		}
		m.filter = m.filter.next()
		m.applyFilters()
		return m, nil

	case "a":
		m.tab, m.form, m.message, m.err = tabSources, newSourceForm(), "", nil
		return m, m.form.focusOn(fieldRef)

	case "d", "delete":
		if m.tab == tabManage {
			item, ok := m.currentManaged()
			if !ok {
				return m, nil
			}
			if !item.Installed {
				return m, nil
			}
			copy := item
			m.pendingUninstall = &copy
			return m, nil
		}
		if m.tab != tabSources {
			return m, nil
		}
		origin, ok := m.currentSource()
		if !ok {
			return m, nil
		}
		if origin.Builtin {
			m.err, m.message = nil, "a origem embutida não pode ser removida; use espaço para desligá-la"
			m.success = false
			return m, nil
		}
		m.pendingRemoval = origin.Name
		return m, nil

	case "enter":
		if m.tab == tabSources {
			return m.toggleCurrentSource()
		}
		if m.tab == tabManage {
			return m.toggleCurrentTool()
		}
		return m.confirmInstall()
	}
	return m, nil
}

// confirmInstall passa pelo diálogo global antes de baixar qualquer coisa: a
// tela do marketplace é o ponto em que código de terceiros entra na máquina.
func (m *Model) confirmInstall() (tui.Screen, tea.Cmd) {
	listing, ok := m.currentTool()
	if !ok || (listing.InstalledVersion == listing.Version && !listing.UpdateAvailable) {
		return m, nil
	}
	action := "Instalar " + listing.Name
	if listing.InstalledVersion != "" {
		action = "Atualizar " + listing.Name
	}
	tool := domain.Tool{
		ID: domain.ToolID("marketplace-" + listing.ID), Name: action,
		Detail: confirmationDetail(listing), Risk: domain.RiskDestructive,
	}
	reference := listing.Ref()
	return m, tui.Navigate(confirmationscreen.New(m.deps, tool, func() tea.Cmd {
		m.installing, m.err, m.message = reference, nil, ""
		return m.install(reference)
	}))
}

func (m *Model) handleSearch(key tea.KeyMsg) (tui.Screen, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.searching = false
		m.query.SetValue("")
		m.query.Blur()
		m.applyFilters()
		return m, nil
	case "enter", "down":
		m.searching = false
		m.query.Blur()
		return m, nil
	case "up":
		m.moveCursor(-1)
		return m, nil
	}
	var command tea.Cmd
	m.query, command = m.query.Update(key)
	m.applyFilters()
	return m, command
}

func (m *Model) handleForm(key tea.KeyMsg) (tui.Screen, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.form = nil
		return m, nil

	case "tab", "down":
		return m, m.form.focusOn((m.form.focus + 1) % formFields)

	case "shift+tab", "up":
		return m, m.form.focusOn((m.form.focus + formFields - 1) % formFields)

	case "enter":
		// Enter no primeiro campo avança em vez de submeter: quem cola a URL
		// e tecla Enter por reflexo espera revisar o nome derivado antes de
		// cadastrar.
		if m.form.focus == fieldRef && strings.TrimSpace(m.form.inputs[fieldName].Value()) == "" &&
			strings.TrimSpace(m.form.inputs[fieldRef].Value()) != "" {
			return m, m.form.focusOn(fieldName)
		}
		origin, err := coremarket.NewOrigin(
			m.form.inputs[fieldName].Value(), "", m.form.inputs[fieldRef].Value())
		if err != nil {
			m.form.err = err
			return m, nil
		}
		m.form = nil
		return m, m.addSource(origin)
	}

	var command tea.Cmd
	m.form.inputs[m.form.focus], command = m.form.inputs[m.form.focus].Update(key)
	m.form.err = nil
	return m, command
}

func (m *Model) handleRemoval(key tea.KeyMsg) (tui.Screen, tea.Cmd) {
	switch strings.ToLower(key.String()) {
	case "y", "s", "enter":
		name := m.pendingRemoval
		m.pendingRemoval = ""
		return m, m.removeSource(name)
	case "n", "esc":
		m.pendingRemoval = ""
	}
	return m, nil
}

func (m *Model) handleUninstall(key tea.KeyMsg) (tui.Screen, tea.Cmd) {
	switch strings.ToLower(key.String()) {
	case "y", "s", "enter":
		item := m.pendingUninstall
		m.pendingUninstall = nil
		return m, m.uninstallTool(item.Tool.ID)
	case "n", "esc":
		m.pendingUninstall = nil
	}
	return m, nil
}

func (m *Model) count() int {
	if m.tab == tabSources {
		return len(m.origins)
	}
	if m.tab == tabManage {
		return len(m.managed)
	}
	return len(m.visible)
}

func (m *Model) cursor() int {
	if m.tab == tabSources {
		return m.sourceCursor
	}
	if m.tab == tabManage {
		return m.manageCursor
	}
	return m.toolCursor
}

func (m *Model) setCursor(value int) {
	value = clamp(value, m.count())
	if m.tab == tabSources {
		m.sourceCursor = value
		return
	}
	if m.tab == tabManage {
		m.manageCursor = value
		return
	}
	if m.toolCursor != value {
		// Trocar de tool começa a leitura do início; herdar o scroll da
		// anterior abriria a ficha no meio de uma frase.
		m.sheetScroll = 0
	}
	m.toolCursor = value
}

// toggleCurrentSource liga ou desliga a origem sob o cursor.
func (m *Model) toggleCurrentSource() (tui.Screen, tea.Cmd) {
	origin, ok := m.currentSource()
	if !ok {
		return m, nil
	}
	return m, m.toggleSource(origin.Name, !origin.Enabled)
}

func (m *Model) toggleCurrentTool() (tui.Screen, tea.Cmd) {
	item, ok := m.currentManaged()
	if !ok {
		return m, nil
	}
	return m, m.toggleTool(item.Tool.ID, !item.Enabled)
}

func (m *Model) moveCursor(delta int) { m.setCursor(m.cursor() + delta) }

func clamp(value, length int) int {
	if value < 0 || length == 0 {
		return 0
	}
	return min(value, length-1)
}

func (t tab) move(delta int) tab {
	count := int(tabSources) + 1
	return tab((int(t) + delta + count) % count)
}

func confirmationDetail(listing coremarket.Listing) string {
	origin := listing.Origin.Title()
	if !listing.Origin.Trusted {
		origin += " (origem não verificada pela engine)"
	}
	return fmt.Sprintf(
		"Origem: %s · publicador: %s · canal: %s · versão: %s. Risco %s; arquivos: %s; rede: %s; subprocesso: %s. O artefato será validado por checksum e pelo manifest antes de ser ativado.",
		origin, listing.Publisher, listing.DistributionTier, listing.Version, listing.Risk,
		permissionLabel(len(listing.Permissions.Filesystem.Read), len(listing.Permissions.Filesystem.Write)),
		yesNo(listing.Permissions.Network), yesNo(listing.Permissions.Subprocess),
	)
}
