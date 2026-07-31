package tokens

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	coretokens "github.com/mateuslh/lealing/cmd/tools/token-usage/internal/tokens"
	"github.com/mateuslh/lealing/sdk/component"
	"github.com/mateuslh/lealing/sdk/protocol"
)

// Pontos de quebra do painel.
const (
	twoColumnMin = 96 // largura mínima para gráfico e recorte lado a lado
	kpiMinWidth  = 18 // abaixo disso um cartão de KPI vira só um número torto
)

// View implementa tui.Screen.
func (m *Model) View(f protocol.Frame) string {
	th := m.theme

	switch {
	case m.loading && !m.report.HasData():
		return component.Center(f.Width, f.Height, th.Dim.Render("varrendo ~/.claude e ~/.codex…"))
	case m.err != nil && !m.report.HasData():
		return component.Center(f.Width, f.Height,
			lipgloss.NewStyle().Foreground(th.Danger).Render("✗ "+m.err.Error()))
	case !m.report.HasData():
		return m.viewEmpty(th, f)
	}

	inner := max(f.Width-4, 24)
	blocks := []string{m.viewKPIs(th, inner)}
	used := lipgloss.Height(blocks[0])

	// A altura do painel de limites é calculada antes de renderizar: ela
	// depende da largura (a linha de ritmo some em janela estreita) e o resto
	// da tela precisa saber quanto sobrou.
	if h := m.limitsHeight(inner); h > 0 {
		blocks = append(blocks, "", m.viewLimits(th, inner, h))
		used += h + 1
	}

	panelsH := max(f.Height-used-2, 7)
	blocks = append(blocks, "", m.viewPanels(th, inner, panelsH))

	body := lipgloss.JoinVertical(lipgloss.Left, blocks...)
	return lipgloss.NewStyle().
		Padding(1, 2).
		MaxWidth(f.Width).
		MaxHeight(f.Height).
		Render(body)
}

// viewEmpty explica o que fazer quando não há log nenhum.
func (m *Model) viewEmpty(th *component.Theme, f protocol.Frame) string {
	// A explicação longa nomeia os dois caminhos; em telas estreitas, a
	// curta ainda diz o essencial.
	detail := "não há logs em ~/.claude/projects nem ~/.codex/sessions"
	if f.Width < lipgloss.Width(detail)+2 {
		detail = "nenhum log de CLI encontrado"
	}
	return component.Center(f.Width, f.Height,
		th.Ghost.Render("◔"),
		"",
		th.Title.Render("Nenhum uso encontrado"),
		th.Dim.Render(detail),
	)
}

// viewKPIs desenha a fila de cartões com as janelas de tempo.
//
// A janela é a leitura que interessa no dia a dia: o total histórico só diz
// que você usa a ferramenta há bastante tempo.
func (m *Model) viewKPIs(th *component.Theme, width int) string {
	cards := make([]kpi, 0, len(m.report.Windows)+1)
	for _, w := range m.report.Windows {
		cards = append(cards, kpi{
			label: w.Label,
			value: formatCost(w.Totals.Cost),
			sub:   formatTokens(w.Totals.TotalTokens()) + " tokens",
		})
	}
	cards = append(cards, kpi{
		label: "Total",
		value: formatCost(m.report.Overall.Cost),
		sub:   formatTokens(m.report.Overall.Messages) + " mensagens",
		tone:  th.Primary,
	})

	// Cada cartão além do primeiro come também a coluna de respiro.
	perCard := (width - (len(cards) - 1)) / max(len(cards), 1)
	if perCard < kpiMinWidth {
		// Não cabem todos: mantém os dois mais recentes e o total, que é a
		// combinação que ainda responde "estou gastando muito agora?".
		if len(cards) > 3 {
			cards = append(cards[:2:2], cards[len(cards)-1])
		}
		perCard = (width - (len(cards) - 1)) / max(len(cards), 1)
	}

	blocks := make([]string, 0, len(cards)*2)
	for i, c := range cards {
		if i > 0 {
			blocks = append(blocks, " ")
		}
		w := perCard
		if i == len(cards)-1 {
			// O último absorve a sobra da divisão inteira, para a fila fechar
			// exatamente na largura disponível.
			w = width - (perCard+1)*(len(cards)-1)
		}
		blocks = append(blocks, c.render(th, w))
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, blocks...)
}

// kpi é um cartão de número grande.
type kpi struct {
	label string
	value string
	sub   string
	tone  lipgloss.TerminalColor
}

// kpiChrome são as colunas que a moldura consome: duas de borda e duas de
// padding. lipgloss.Width dimensiona o conteúdo, não o bloco final, então
// sem descontar isso cada cartão sai quatro colunas mais largo que o
// planejado — e a fila inteira transborda.
const kpiChrome = 4

func (k kpi) render(th *component.Theme, width int) string {
	if width <= kpiChrome+4 {
		return ""
	}
	valueStyle := th.Title
	if k.tone != nil {
		valueStyle = lipgloss.NewStyle().Foreground(k.tone).Bold(true)
	}

	content := lipgloss.JoinVertical(lipgloss.Left,
		th.Ghost.Render(strings.ToUpper(k.label)),
		valueStyle.Render(k.value),
		th.Ghost.Render(k.sub),
	)
	return lipgloss.NewStyle().
		Width(width-kpiChrome).
		MaxWidth(width).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(th.Border).
		Padding(0, 1).
		Render(content)
}

// viewPanels põe a série diária e o recorte lado a lado quando cabe.
func (m *Model) viewPanels(th *component.Theme, width, height int) string {
	// Com poucos dias registrados, esticar os painéis até a base deixa dois
	// retângulos vazios maiores que os dados dentro deles.
	// 5 linhas de moldura e cabeçalho na série, 4 no recorte.
	fit := max(len(m.report.ByDay)+5, len(m.currentSlices())+4)
	height = max(min(height, fit), 7)

	if width < twoColumnMin {
		half := max(height/2, 5)
		return lipgloss.JoinVertical(lipgloss.Left,
			m.viewDaily(th, width, half),
			"",
			m.viewBreakdown(th, width, height-half-1),
		)
	}

	left := (width - 1) * 55 / 100
	return lipgloss.JoinHorizontal(lipgloss.Top,
		m.viewDaily(th, left, height),
		" ",
		m.viewBreakdown(th, width-left-1, height),
	)
}

// viewDaily desenha o custo por dia como barras horizontais mais a série.
func (m *Model) viewDaily(th *component.Theme, width, height int) string {
	days := m.report.ByDay
	if len(days) == 0 {
		return component.Panel{
			Title: "por dia", Glyph: "▤", Accent: th.Primary,
			Width: width, Height: height,
		}.Render(th, th.Ghost.Render("sem dias registrados"))
	}

	inner := width - 4
	// A sparkline resume a série inteira; as barras detalham os dias que
	// cabem. Juntas, dão tendência e magnitude sem exigir rolagem.
	spark := component.Sparkline{
		Values:    dailyCosts(days),
		Width:     inner,
		Tone:      th.Primary,
		Highlight: true,
	}.Render(th)

	maxCost := 0.0
	for _, d := range days {
		maxCost = max(maxCost, d.Totals.Cost)
	}

	// Reserva: 2 linhas de sparkline + 1 de legenda.
	rowsAvail := max(height-2-3, 1)
	visible := days
	if len(visible) > rowsAvail {
		visible = visible[len(visible)-rowsAvail:] // os dias mais recentes
	}

	rows := make([]component.BarRow, len(visible))
	for i, d := range visible {
		fraction := 0.0
		if maxCost > 0 {
			fraction = d.Totals.Cost / maxCost
		}
		rows[i] = component.BarRow{
			Label:    d.Day[5:], // "MM-DD" basta: o ano é sempre o corrente
			Value:    formatCost(d.Totals.Cost),
			Fraction: fraction,
			Tone:     th.Primary,
		}
	}

	chart := component.BarChart{Rows: rows, Width: inner, LabelWidth: 5}.Render(th)
	legend := th.Ghost.Render(days[0].Day + " → " + days[len(days)-1].Day)

	content := lipgloss.JoinVertical(lipgloss.Left, spark, legend, "", chart)

	return component.Panel{
		Title:  "custo por dia",
		Glyph:  "▤",
		Accent: th.Primary,
		Footer: formatCost(m.report.Overall.Cost) + " no total",
		Width:  width,
		Height: height,
	}.Render(th, content)
}

// viewBreakdown desenha o recorte ativo (modelo, projeto ou provedor).
func (m *Model) viewBreakdown(th *component.Theme, width, height int) string {
	slices := m.currentSlices()
	inner := width - 4

	tabs := m.viewTabs(th, inner)

	if len(slices) == 0 {
		return component.Panel{
			Title: m.breakdown.title(), Glyph: "◫", Accent: th.Secondary,
			Width: width, Height: height,
		}.Render(th, tabs+"\n\n"+th.Ghost.Render("sem dados"))
	}

	total := 0.0
	for _, s := range slices {
		total += s.Totals.Cost
	}

	rowsAvail := max(height-2-2, 1)
	start := min(m.scroll, max(len(slices)-rowsAvail, 0))
	end := min(start+rowsAvail, len(slices))

	rows := make([]component.BarRow, 0, end-start)
	for _, s := range slices[start:end] {
		fraction := 0.0
		if total > 0 {
			fraction = s.Totals.Cost / total
		}
		rows = append(rows, component.BarRow{
			Label:    s.Label,
			Value:    formatCost(s.Totals.Cost),
			Fraction: fraction,
			Tone:     m.toneFor(th, s),
		})
	}

	chart := component.BarChart{Rows: rows, Width: inner}.Render(th)
	content := lipgloss.JoinVertical(lipgloss.Left, tabs, "", chart)

	footer := ""
	if len(slices) > rowsAvail {
		footer = itoa(end) + "/" + itoa(len(slices))
	}

	return component.Panel{
		Title:  m.breakdown.title(),
		Glyph:  "◫",
		Accent: th.Secondary,
		Footer: footer,
		Width:  width,
		Height: height,
	}.Render(th, content)
}

// toneFor colore a barra conforme o tipo do recorte.
func (m *Model) toneFor(th *component.Theme, s coretokens.Slice) lipgloss.TerminalColor {
	switch m.breakdown {
	case byProvider:
		return providerTone(th, s.Label)
	case byModel:
		return modelTone(th, s.Label)
	default:
		return th.Secondary
	}
}

// viewTabs desenha o seletor de recorte.
func (m *Model) viewTabs(th *component.Theme, width int) string {
	names := []string{"modelo", "projeto", "provedor"}
	parts := make([]string, len(names))
	for i, name := range names {
		if breakdown(i) == m.breakdown {
			parts[i] = lipgloss.NewStyle().Foreground(th.Secondary).Bold(true).Render("▸ " + name)
			continue
		}
		parts[i] = th.Ghost.Render("  " + name)
	}
	line := strings.Join(parts, " ")
	if lipgloss.Width(line) > width {
		// Sem espaço para as três abas, mostra só a ativa mais a dica.
		return lipgloss.NewStyle().Foreground(th.Secondary).Bold(true).Render(m.breakdown.title()) +
			th.Ghost.Render("  ↹")
	}
	return line
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
