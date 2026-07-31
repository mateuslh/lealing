package theme

import "github.com/charmbracelet/lipgloss"

// Theme é a paleta já materializada em estilos do lipgloss.
//
// Os estilos são calculados uma única vez, na construção: View() roda a cada
// frame e a cada tecla, então montar estilo dentro do render é justamente o
// tipo de custo que faz uma TUI parecer travada em terminais lentos.
type Theme struct {
	Palette

	// Estrutura da janela.
	App     lipgloss.Style
	Topbar  lipgloss.Style
	Brand   lipgloss.Style
	Meta    lipgloss.Style
	Status  lipgloss.Style
	Divider lipgloss.Style

	// Painéis.
	Panel      lipgloss.Style
	PanelFocus lipgloss.Style
	PanelTitle lipgloss.Style

	// Tipografia.
	Title    lipgloss.Style
	Subtitle lipgloss.Style
	Body     lipgloss.Style
	Dim      lipgloss.Style
	Ghost    lipgloss.Style
	Strong   lipgloss.Style

	// Listas.
	Item         lipgloss.Style
	ItemSelected lipgloss.Style
	ItemGlyph    lipgloss.Style
	ItemDesc     lipgloss.Style
	MatchHint    lipgloss.Style

	// Elementos.
	Badge     lipgloss.Style
	Pill      lipgloss.Style
	KeyCap    lipgloss.Style
	KeyLabel  lipgloss.Style
	Counter   lipgloss.Style
	Scrollbar lipgloss.Style
	Cursor    lipgloss.Style
}

// New materializa um tema a partir de uma paleta.
func New(p Palette) *Theme {
	t := &Theme{Palette: p}

	t.App = lipgloss.NewStyle().Foreground(p.Text)
	t.Topbar = lipgloss.NewStyle().Foreground(p.Muted).Padding(0, 2)
	t.Brand = lipgloss.NewStyle().Bold(true)
	t.Meta = lipgloss.NewStyle().Foreground(p.Faint)
	t.Status = lipgloss.NewStyle().Foreground(p.Muted).Padding(0, 2)
	t.Divider = lipgloss.NewStyle().Foreground(p.Border)

	t.Panel = lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(p.Border).
		Padding(0, 1)
	t.PanelFocus = t.Panel.BorderForeground(p.BorderFocus)
	t.PanelTitle = lipgloss.NewStyle().Foreground(p.Muted).Bold(true)

	t.Title = lipgloss.NewStyle().Foreground(p.Text).Bold(true)
	t.Subtitle = lipgloss.NewStyle().Foreground(p.Muted)
	t.Body = lipgloss.NewStyle().Foreground(p.Text)
	t.Dim = lipgloss.NewStyle().Foreground(p.Muted)
	t.Ghost = lipgloss.NewStyle().Foreground(p.Faint)
	t.Strong = lipgloss.NewStyle().Foreground(p.Primary).Bold(true)

	t.Item = lipgloss.NewStyle().Foreground(p.Text)
	t.ItemSelected = lipgloss.NewStyle().Foreground(p.Text).Bold(true)
	t.ItemGlyph = lipgloss.NewStyle().Foreground(p.Primary)
	t.ItemDesc = lipgloss.NewStyle().Foreground(p.Faint)
	t.MatchHint = lipgloss.NewStyle().Foreground(p.Accent).Bold(true)

	t.Badge = lipgloss.NewStyle().Padding(0, 1).Foreground(p.OnPrimary).Background(p.Primary)
	t.Pill = lipgloss.NewStyle().Padding(0, 1).Foreground(p.Muted).Background(p.Overlay)
	t.KeyCap = lipgloss.NewStyle().Foreground(p.Text).Background(p.Overlay).Padding(0, 1)
	t.KeyLabel = lipgloss.NewStyle().Foreground(p.Faint)
	t.Counter = lipgloss.NewStyle().Foreground(p.Faint)
	t.Scrollbar = lipgloss.NewStyle().Foreground(p.Border)
	t.Cursor = lipgloss.NewStyle().Foreground(p.Primary).Bold(true)

	return t
}

// Default devolve o tema padrão do lealing.
func Default() *Theme { return New(Midnight()) }

// SpectrumStyle devolve um estilo de texto na cor do spectrum indicada.
//
// Não se chama Accent porque Palette já embute um token com esse nome, e um
// método sombreando o campo promovido tornaria t.Accent ambíguo na leitura.
func (t *Theme) SpectrumStyle(i int) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(t.SpectrumAt(i))
}

// Key renderiza um atalho no formato "tecla ação", usado na barra de status.
func (t *Theme) Key(cap, label string) string {
	return t.KeyCap.Render(cap) + " " + t.KeyLabel.Render(label)
}

// Logo devolve o wordmark do lealing em gradiente, na versão compacta.
func (t *Theme) Logo() string {
	return t.Brand.Render(Text("lealing", t.Gradient))
}
