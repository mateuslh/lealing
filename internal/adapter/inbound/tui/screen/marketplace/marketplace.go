// Package marketplace implementa a loja nativa da engine. Ela conversa
// somente com o caso de uso do core; HTTP, arquivos e instalação permanecem
// atrás das portas de saída.
package marketplace

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/component"
	confirmationscreen "github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/confirmation"
	"github.com/mateuslh/lealing/internal/core/domain"
	coremarket "github.com/mateuslh/lealing/internal/core/marketplace"
	"github.com/mateuslh/lealing/internal/core/toolinstall"
)

type Model struct {
	deps    tui.Deps
	manager coremarket.Manager

	tools      []coremarket.Listing
	selected   int
	loading    bool
	installing bool
	err        error
	message    string
}

var _ tui.Screen = (*Model)(nil)

func New(deps tui.Deps, manager coremarket.Manager) *Model {
	return &Model{deps: deps, manager: manager, loading: true}
}

func (*Model) ID() tui.ScreenID { return "tool/marketplace" }
func (*Model) Title() string    { return "marketplace" }
func (m *Model) Init() tea.Cmd  { return m.load() }

type loadedMsg struct {
	tools []coremarket.Listing
	err   error
}

type installedMsg struct {
	installation toolinstall.Installation
	err          error
}

func (m *Model) Update(message tea.Msg) (tui.Screen, tea.Cmd) {
	switch message := message.(type) {
	case loadedMsg:
		m.loading = false
		m.err = message.err
		if message.err == nil {
			m.tools = message.tools
			m.selected = min(m.selected, max(len(m.tools)-1, 0))
		}
		return m, nil

	case installedMsg:
		m.installing = false
		if message.err != nil {
			if errors.Is(message.err, coremarket.ErrAlreadyLatest) {
				m.message = "a versão mais recente já está instalada"
				m.err = nil
			} else {
				m.err = message.err
			}
			return m, nil
		}
		m.err = nil
		m.message = fmt.Sprintf("%s@%s instalada; catálogo atualizado", message.installation.ID, message.installation.Version)
		return m, m.load()

	case tea.KeyMsg:
		if m.installing {
			return m, nil
		}
		switch message.String() {
		case "up", "k":
			m.selected = max(m.selected-1, 0)
		case "down", "j":
			m.selected = min(m.selected+1, max(len(m.tools)-1, 0))
		case "g", "home":
			m.selected = 0
		case "G", "end":
			m.selected = max(len(m.tools)-1, 0)
		case "r", "ctrl+r":
			m.loading, m.err, m.message = true, nil, ""
			return m, m.load()
		case "enter":
			listing, ok := m.current()
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
			return m, tui.Navigate(confirmationscreen.New(m.deps, tool, func() tea.Cmd {
				m.installing, m.err, m.message = true, nil, ""
				return m.install(listing.ID)
			}))
		}
	}
	return m, nil
}

func confirmationDetail(listing coremarket.Listing) string {
	return fmt.Sprintf(
		"Publicador: %s · canal: %s · versão: %s. Risco %s; arquivos: %s; rede: %s; subprocesso: %s. O artefato será validado por checksum e pelo manifest antes de ser ativado.",
		listing.Publisher, listing.DistributionTier, listing.Version, listing.Risk,
		permissionLabel(len(listing.Permissions.Filesystem.Read), len(listing.Permissions.Filesystem.Write)),
		yesNo(listing.Permissions.Network), yesNo(listing.Permissions.Subprocess),
	)
}

func (m *Model) load() tea.Cmd {
	manager := m.manager
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if manager == nil {
			return loadedMsg{err: errors.New("marketplace não configurado")}
		}
		tools, err := manager.List(ctx)
		return loadedMsg{tools: tools, err: err}
	}
}

func (m *Model) install(id string) tea.Cmd {
	manager := m.manager
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		installation, err := manager.Install(ctx, id)
		return installedMsg{installation: installation, err: err}
	}
}

func (m *Model) current() (coremarket.Listing, bool) {
	if m.selected < 0 || m.selected >= len(m.tools) {
		return coremarket.Listing{}, false
	}
	return m.tools[m.selected], true
}

func (m *Model) View(frame tui.Frame) string {
	th := m.deps.Theme
	if m.loading && len(m.tools) == 0 {
		return component.Center(frame.Width, frame.Height, th.Dim.Render("consultando o índice público…"))
	}
	if m.err != nil && len(m.tools) == 0 {
		return component.Center(frame.Width, frame.Height, lipgloss.NewStyle().Foreground(th.Danger).Render(component.TruncateTail("marketplace indisponível: "+m.err.Error(), max(frame.Width-8, 1))))
	}
	if len(m.tools) == 0 {
		return component.Center(frame.Width, frame.Height, th.Ghost.Render("nenhuma tool compatível para esta plataforma"))
	}

	innerWidth := max(frame.Width-4, 1)
	innerHeight := max(frame.Height-2, 1)
	listWidth := innerWidth
	showDetail := innerWidth >= 76 && innerHeight >= 8
	if showDetail {
		listWidth = max(innerWidth*5/9, 30)
	}
	list := m.renderList(listWidth, innerHeight)
	content := list
	if showDetail {
		detailWidth := max(innerWidth-listWidth-3, 20)
		detail := m.renderDetail(detailWidth, innerHeight)
		content = lipgloss.JoinHorizontal(lipgloss.Top, list, strings.Repeat(" ", 3), detail)
	}
	return lipgloss.NewStyle().
		Padding(1, 2).
		MaxWidth(frame.Width).
		MaxHeight(frame.Height).
		Render(content)
}

func (m *Model) renderList(width, height int) string {
	th := m.deps.Theme
	header := component.Spread(th.Strong.Render("TOOLS DISPONÍVEIS"), th.Ghost.Render(fmt.Sprintf("%d", len(m.tools))), width)
	lines := []string{header, ""}
	visible := max(height-len(lines)-1, 1)
	start := 0
	if m.selected >= visible {
		start = m.selected - visible + 1
	}
	end := min(start+visible, len(m.tools))
	for index := start; index < end; index++ {
		tool := m.tools[index]
		caret := "  "
		name := th.Item.Render(tool.Name)
		if index == m.selected {
			caret = lipgloss.NewStyle().Foreground(th.Primary).Render("▎") + " "
			name = th.ItemSelected.Render(tool.Name)
		}
		state := tool.Version
		switch {
		case tool.UpdateAvailable:
			state = lipgloss.NewStyle().Foreground(th.Warning).Render("↑ " + tool.Version)
		case tool.InstalledVersion != "":
			state = lipgloss.NewStyle().Foreground(th.Success).Render("✓ " + tool.Version)
		}
		line := component.Spread(caret+toolGlyph(tool.Glyph)+" "+name, state, width)
		lines = append(lines, component.TruncateTail(line, width))
	}
	return strings.Join(lines, "\n")
}

func (m *Model) renderDetail(width, height int) string {
	tool, ok := m.current()
	if !ok {
		return ""
	}
	th := m.deps.Theme
	state := "não instalada"
	if tool.UpdateAvailable {
		state = "atualização de " + tool.InstalledVersion
	} else if tool.InstalledVersion != "" {
		state = "instalada em " + tool.InstalledVersion
	}
	lines := []string{
		th.Strong.Render(component.TruncateTail(tool.Name, width)),
		th.Ghost.Render(component.TruncateTail(tool.ID+" · "+tool.Version, width)),
		"",
		th.ItemDesc.Render(component.TruncateTail(tool.Summary, width)),
		"",
		component.TruncateTail("publicador  "+tool.Publisher, width),
		component.TruncateTail("canal       "+string(tool.DistributionTier), width),
		component.TruncateTail("risco       "+tool.Risk, width),
		component.TruncateTail("estado      "+state, width),
		component.TruncateTail("arquivos    "+permissionLabel(len(tool.Permissions.Filesystem.Read), len(tool.Permissions.Filesystem.Write)), width),
		component.TruncateTail("rede        "+yesNo(tool.Permissions.Network), width),
		component.TruncateTail("subprocesso "+yesNo(tool.Permissions.Subprocess), width),
	}
	if m.installing {
		lines = append(lines, "", lipgloss.NewStyle().Foreground(th.Warning).Render("instalando e validando…"))
	} else if m.message != "" {
		lines = append(lines, "", lipgloss.NewStyle().Foreground(th.Success).Render(component.TruncateTail(m.message, width)))
	} else if m.err != nil {
		lines = append(lines, "", lipgloss.NewStyle().Foreground(th.Danger).Render(component.TruncateTail(m.err.Error(), width)))
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
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

func (*Model) Hints() []tui.Hint {
	return []tui.Hint{{Key: "↑↓", Label: "selecionar"}, {Key: "↵", Label: "instalar/atualizar"}, {Key: "r", Label: "recarregar"}, {Key: "esc", Label: "voltar"}}
}

func (m *Model) Meta() []string {
	if tool, ok := m.current(); ok {
		return []string{string(tool.DistributionTier), tool.Version}
	}
	return nil
}

func (m *Model) Status() (string, lipgloss.TerminalColor) {
	switch {
	case m.installing:
		return "instalando", m.deps.Theme.Warning
	case m.err != nil:
		return "falha no marketplace", m.deps.Theme.Danger
	case m.loading:
		return "atualizando índice", m.deps.Theme.Secondary
	default:
		return fmt.Sprintf("%d tools compatíveis", len(m.tools)), m.deps.Theme.Success
	}
}
