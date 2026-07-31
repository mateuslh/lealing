// Package sysinfo é a tela da tool "Informações do Sistema".
package sysinfo

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/component"
	"github.com/mateuslh/lealing/internal/core/sysinfo"
)

// ScreenID identifica esta tela.
const ScreenID tui.ScreenID = "tool/system-info"

// Model é o estado da tela.
type Model struct {
	deps      tui.Deps
	inspector sysinfo.Inspector
	now       func() time.Time

	width, height int

	snapshot sysinfo.Snapshot
	loadedAt time.Time
	loading  bool
	err      error
}

var _ tui.Screen = (*Model)(nil)

// New monta a tela com o relógio explícito.
func New(deps tui.Deps, inspector sysinfo.Inspector, now func() time.Time) *Model {
	if now == nil {
		now = time.Now
	}
	return &Model{deps: deps, inspector: inspector, now: now, loading: true}
}

// ID implementa tui.Screen.
func (m *Model) ID() tui.ScreenID { return ScreenID }

// Title implementa tui.Screen.
func (m *Model) Title() string { return "informações do sistema" }

// Init implementa tui.Screen.
func (m *Model) Init() tea.Cmd { return m.load() }

// snapshotMsg entrega a leitura concluída.
type snapshotMsg struct {
	snapshot sysinfo.Snapshot
	err      error
}

// load lê o estado da máquina fora da thread de render.
func (m *Model) load() tea.Cmd {
	inspector := m.inspector
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		snap, err := inspector.Inspect(ctx)
		return snapshotMsg{snapshot: snap, err: err}
	}
}

// Update implementa tui.Screen.
func (m *Model) Update(msg tea.Msg) (tui.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case snapshotMsg:
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.snapshot = msg.snapshot
			m.loadedAt = m.now()
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "r", "ctrl+r":
			m.loading = true
			return m, m.load()
		}
	}
	return m, nil
}

// View implementa tui.Screen.
func (m *Model) View(f tui.Frame) string {
	th := m.deps.Theme

	if m.err != nil {
		return component.Center(f.Width, f.Height,
			lipgloss.NewStyle().Foreground(th.Danger).Render("✗ "+m.err.Error()))
	}
	if m.loading && m.snapshot.OSVersion == "" {
		return component.Center(f.Width, f.Height, th.Dim.Render("lendo o sistema…"))
	}

	inner := max(f.Width-4, 20)
	// Duas colunas quando há largura; a leitura fica mais densa e evita uma
	// coluna de valores perdida à direita de uma tela larga.
	sections := m.snapshot.Sections()

	var blocks []string
	if inner >= 96 {
		blocks = m.twoColumns(inner, sections)
	} else {
		blocks = m.oneColumn(inner, sections)
	}

	body := lipgloss.JoinVertical(lipgloss.Left, blocks...)
	paddingY := 1
	if lipgloss.Height(body)+2 > f.Height {
		// Em janelas baixas, respiro é menos importante que fechar todas as
		// molduras. Sem esta compactação o painel de hardware terminava sem
		// borda em 60×20, embora nenhuma linha excedesse o frame.
		blocks = m.compactColumns(inner, sections)
		body = lipgloss.JoinVertical(lipgloss.Left, blocks...)
		paddingY = 0
	}
	// Os limites são aplicados depois do padding: recortar só o miolo
	// deixaria as duas linhas de respiro estourando o frame.
	return lipgloss.NewStyle().
		Padding(paddingY, 2).
		MaxWidth(f.Width).
		MaxHeight(f.Height).
		Render(body)
}

// compactColumns reúne os campos numa moldura só. Três pares de borda
// consumiriam seis linhas — quase metade do corpo de uma janela 60×20.
func (m *Model) compactColumns(width int, sections []sysinfo.Section) []string {
	var rows []component.Row
	for _, section := range sections {
		for _, field := range section.Fields {
			row := component.Row{Label: field.Label, Value: field.Value}
			if section.Title == "Bateria" && field.Label == "Carga" {
				if pct, ok := percentOf(field.Value); ok && pct <= 20 {
					row.Tone = m.deps.Theme.Danger
				}
			}
			rows = append(rows, row)
		}
	}
	content := component.FieldList{Rows: rows, Width: width - 4}.Render(m.deps.Theme)
	panel := component.Panel{
		Title:  "Resumo",
		Glyph:  "◎",
		Accent: m.deps.Theme.Primary,
		Width:  width,
		Height: len(rows) + 2,
	}.Render(m.deps.Theme, content)
	return []string{panel}
}

// oneColumn empilha as seções.
func (m *Model) oneColumn(width int, sections []sysinfo.Section) []string {
	blocks := make([]string, 0, len(sections)*2)
	for i, sec := range sections {
		if i > 0 {
			blocks = append(blocks, "")
		}
		blocks = append(blocks, m.panel(sec, width))
	}
	return blocks
}

// twoColumns põe Sistema e Hardware lado a lado, com Bateria embaixo.
func (m *Model) twoColumns(width int, sections []sysinfo.Section) []string {
	if len(sections) < 3 {
		return m.oneColumn(width, sections)
	}
	colW := (width - 1) / 2
	// As duas colunas recebem a mesma altura: molduras de tamanhos
	// diferentes lado a lado deixam um degrau que parece defeito.
	rows := max(len(sections[0].Fields), len(sections[1].Fields))
	top := lipgloss.JoinHorizontal(lipgloss.Top,
		m.panelH(sections[0], colW, rows+2),
		" ",
		m.panelH(sections[1], width-colW-1, rows+2),
	)
	return []string{top, "", m.panel(sections[2], width)}
}

// panel envolve uma seção na moldura, dimensionada pelo próprio conteúdo.
func (m *Model) panel(sec sysinfo.Section, width int) string {
	return m.panelH(sec, width, len(sec.Fields)+2)
}

// panelH envolve uma seção com altura explícita.
func (m *Model) panelH(sec sysinfo.Section, width, height int) string {
	th := m.deps.Theme

	rows := make([]component.Row, len(sec.Fields))
	for i, f := range sec.Fields {
		rows[i] = component.Row{Label: f.Label, Value: f.Value}
	}
	// Bateria fraca merece destaque: é a informação acionável desta tela.
	if sec.Title == "Bateria" && m.snapshot.HasBattery {
		if pct, ok := percentOf(m.snapshot.BatteryLevel); ok && pct <= 20 {
			rows[0].Tone = th.Danger
		}
	}

	content := component.FieldList{Rows: rows, Width: width - 4}.Render(th)

	glyph := map[string]string{"Sistema": "⌬", "Hardware": "⚙", "Bateria": "⏻"}[sec.Title]
	accent := map[string]lipgloss.TerminalColor{
		"Sistema":  th.Primary,
		"Hardware": th.Accent,
		"Bateria":  th.Success,
	}[sec.Title]

	return component.Panel{
		Title:  sec.Title,
		Glyph:  glyph,
		Accent: accent,
		Width:  width,
		Height: height,
	}.Render(th, content)
}

// percentOf extrai o número de uma string como "51%".
func percentOf(s string) (int, bool) {
	n := 0
	found := false
	for _, r := range s {
		if r < '0' || r > '9' {
			break
		}
		n = n*10 + int(r-'0')
		found = true
	}
	return n, found
}

// Hints implementa tui.Screen.
func (m *Model) Hints() []tui.Hint {
	return []tui.Hint{
		{Key: "r", Label: "atualizar"},
		{Key: "esc", Label: "voltar"},
	}
}

// Status alimenta a barra de status com o horário da leitura.
func (m *Model) Status() (string, lipgloss.TerminalColor) {
	th := m.deps.Theme
	if m.loading {
		return "atualizando…", th.Accent
	}
	if m.loadedAt.IsZero() {
		return "", nil
	}
	return "lido às " + m.loadedAt.Format("15:04:05"), th.Faint
}
