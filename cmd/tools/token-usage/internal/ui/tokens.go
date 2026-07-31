package tokens

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	coretokens "github.com/mateuslh/lealing/cmd/tools/token-usage/internal/tokens"
	"github.com/mateuslh/lealing/sdk/component"
	"github.com/mateuslh/lealing/sdk/protocol"
	"github.com/mateuslh/lealing/sdk/screen"
)

// ScreenID identifica esta tela.
const ScreenID = "tool/token-usage"

// Generator é o recorte do serviço de tokens que a tela consome.
type Generator interface {
	Generate(ctx context.Context) (coretokens.Report, error)
}

// breakdown é o recorte exibido no painel inferior direito.
type breakdown uint8

const (
	byModel breakdown = iota
	byProject
	byProvider
	breakdownCount
)

func (b breakdown) title() string {
	switch b {
	case byProject:
		return "por projeto"
	case byProvider:
		return "por provedor"
	default:
		return "por modelo"
	}
}

// Model é o estado da tela.
type Model struct {
	theme     *component.Theme
	generator Generator
	now       func() time.Time

	width, height int

	report   coretokens.Report
	loading  bool
	loadedAt time.Time
	scanTook time.Duration
	err      error

	breakdown breakdown
	// scroll rola a lista do painel de recortes, que pode ter dezenas de
	// projetos.
	scroll int
}

var _ screen.Model = (*Model)(nil)

// New monta a tela.
func New(theme *component.Theme, generator Generator, now func() time.Time) *Model {
	if now == nil {
		now = time.Now
	}
	return &Model{theme: theme, generator: generator, now: now, loading: true}
}

// ID implementa tui.Screen.
func (m *Model) ID() string { return ScreenID }

// Title implementa tui.Screen.
func (m *Model) Title() string { return "uso de tokens" }

// Init implementa tui.Screen.
func (m *Model) Init() tea.Cmd { return m.load() }

// reportMsg entrega o relatório pronto.
type reportMsg struct {
	report coretokens.Report
	took   time.Duration
	err    error
}

// load varre os logs fora da thread de render.
//
// O timeout é generoso porque a varredura percorre todo o histórico das CLIs
// no disco; em compensação, roda em goroutine e a tela permanece responsiva.
func (m *Model) load() tea.Cmd {
	generator := m.generator
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		start := time.Now()
		report, err := generator.Generate(ctx)
		return reportMsg{report: report, took: time.Since(start), err: err}
	}
}

// Update implementa tui.Screen.
func (m *Model) Update(msg tea.Msg) (screen.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case reportMsg:
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.report = msg.report
			m.loadedAt = m.now()
			m.scanTook = msg.took
			m.scroll = 0
		}
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "r", "ctrl+r":
			if m.loading {
				return m, nil
			}
			m.loading = true
			return m, m.load()

		case "tab", "right", "l":
			m.breakdown = (m.breakdown + 1) % breakdownCount
			m.scroll = 0
			return m, nil

		case "shift+tab", "left", "h":
			m.breakdown = (m.breakdown + breakdownCount - 1) % breakdownCount
			m.scroll = 0
			return m, nil

		case "down", "j":
			m.scroll = min(m.scroll+1, max(len(m.currentSlices())-1, 0))
			return m, nil

		case "up", "k":
			m.scroll = max(m.scroll-1, 0)
			return m, nil

		case "g", "home":
			m.scroll = 0
			return m, nil
		}
	}
	return m, nil
}

// currentSlices devolve o recorte ativo.
func (m *Model) currentSlices() []coretokens.Slice {
	switch m.breakdown {
	case byProject:
		return m.report.ByProject
	case byProvider:
		return m.report.ByProvider
	default:
		return m.report.ByModel
	}
}

// Hints implementa tui.Screen.
func (m *Model) Hints() []protocol.Hint {
	return []protocol.Hint{
		{Key: "↹", Label: "recorte"},
		{Key: "↑↓", Label: "rolar"},
		{Key: "r", Label: "atualizar"},
		{Key: "esc", Label: "voltar"},
	}
}

// Status alimenta a barra de status com o custo da varredura.
func (m *Model) Status() string {
	switch {
	case m.loading:
		return "varrendo os logs…"
	case m.err != nil:
		return m.err.Error()
	case m.loadedAt.IsZero():
		return ""
	}

	text := m.loadedAt.Format("15:04:05") + " · " +
		formatTokens(m.report.Overall.Messages) + " mensagens em " +
		m.scanTook.Round(time.Millisecond).String()
	if len(m.report.Errs) > 0 {
		// A mensagem em si, não um "leitura parcial" genérico: quando a cota
		// falta porque a sessão venceu, o texto do erro é a instrução do que
		// fazer para trazê-la de volta.
		return text + " · " + m.report.Errs[0].Error()
	}
	return text
}
