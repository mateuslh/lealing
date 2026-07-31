package tokens

import (
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/component"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	coretokens "github.com/mateuslh/lealing/internal/core/tokens"
)

// Larguras de corte do painel de limites.
const (
	// paceMinWidth é onde a linha de ritmo passa a caber sem virar reticências.
	paceMinWidth = 62
	// meterMinWidth é onde ainda sobra barra depois de rótulo e números.
	meterMinWidth = 40
	// labelColumn alinha os rótulos de janela sob o nome do provedor. Treze
	// colunas é o que "Últimas 5h" pede com a separação incluída.
	labelColumn = 13
)

// viewLimits desenha as cotas agrupadas por CLI.
//
// É o painel que responde "posso continuar trabalhando?", e por isso vem
// antes de qualquer número histórico: uma janela de 5h em 94% importa mais
// que o gasto do mês.
func (m *Model) viewLimits(th *theme.Theme, width, height int) string {
	groups := m.limitGroups()
	if len(groups) == 0 {
		return ""
	}

	inner := width - 4
	blocks := make([]string, 0, len(groups))
	for i, g := range groups {
		if i > 0 {
			blocks = append(blocks, "") // respiro entre CLIs
		}
		blocks = append(blocks, g.render(m, th, inner))
	}

	return component.Panel{
		Title:  "limites",
		Glyph:  "◷",
		Accent: th.Accent,
		Width:  width,
		Height: height,
	}.Render(th, strings.Join(blocks, "\n"))
}

// limitsHeight é quantas linhas o painel ocupa numa largura dada. A tela
// precisa saber disso antes de renderizar para dividir o espaço restante.
func (m *Model) limitsHeight(width int) int {
	groups := m.limitGroups()
	if len(groups) == 0 {
		return 0
	}
	lines := 0
	for i, g := range groups {
		if i > 0 {
			lines++ // linha de respiro entre CLIs
		}
		lines++ // cabeçalho da CLI
		for _, r := range g.rows {
			lines += r.height(width - 4)
		}
	}
	return lines + 2 // as duas bordas
}

// limitGroup é o bloco de uma CLI: o cabeçalho mais suas janelas.
type limitGroup struct {
	provider string
	// note qualifica a origem do dado no canto direito do cabeçalho.
	note string
	rows []limitRow
}

// limitRow é uma linha do bloco: uma janela de cota, o saldo de créditos ou
// uma janela estimada por nós. Estimadas trazem só o gasto — inventar
// percentual sem conhecer o limite do plano seria mentir com precisão.
type limitRow struct {
	label    string
	quota    coretokens.RateWindow
	reported bool
	credits  *coretokens.Credits
	summary  string
}

// height é quantas linhas a row ocupa: a segunda detalha ritmo ou valores, e
// só existe quando há o que detalhar e largura para isso.
func (r limitRow) height(inner int) int {
	if inner < paceMinWidth {
		return 1
	}
	if r.reported || (r.credits != nil && r.credits.Metered()) {
		return 2
	}
	return 1
}

// limitGroups monta os blocos na ordem em que devem aparecer: quem reporta
// cota primeiro, porque é o dado duro.
func (m *Model) limitGroups() []limitGroup {
	var groups []limitGroup
	index := map[string]int{}

	for _, w := range m.report.RateWindows {
		i, ok := index[w.Provider]
		if !ok {
			groups = append(groups, limitGroup{provider: w.Provider, note: w.Source})
			i = len(groups) - 1
			index[w.Provider] = i
		}
		// Uma cota lida do log é uma foto do último uso: depois de algumas
		// horas parado, o percentual descreve o passado, e dizer há quanto
		// tempo é o que impede lê-lo como estado de agora.
		if age := m.now().Sub(w.ObservedAt); !w.ObservedAt.IsZero() && age > time.Hour {
			groups[i].note = w.Source + " · visto há " + formatDuration(age)
		}
		groups[i].rows = append(groups[i].rows, limitRow{
			label:    w.Label,
			quota:    w,
			reported: true,
		})
	}

	// Créditos ficam de lado até a ordenação passar: eles não são uma janela
	// de tempo e precisam entrar como última linha do bloco, não no meio das
	// cotas ordenadas por duração.
	balances := make(map[string]coretokens.Credits, len(m.report.Credits))
	for _, c := range m.report.Credits {
		balances[c.Provider] = c
	}

	// CLIs que não publicam cota nenhuma entram com o consumo que nós mesmos
	// medimos, para que o painel não sugira que elas estão paradas.
	for _, provider := range m.report.Providers {
		if _, ok := index[provider]; ok {
			continue
		}
		rows := m.estimatedRows(provider)
		if len(rows) == 0 {
			continue
		}
		index[provider] = len(groups)
		groups = append(groups, limitGroup{
			provider: provider,
			note:     "sem cota publicada",
			rows:     rows,
		})
	}

	// A janela curta vem primeiro: é a que decide se dá para continuar agora.
	for i := range groups {
		sort.SliceStable(groups[i].rows, func(a, b int) bool {
			return groups[i].rows[a].quota.WindowMinutes < groups[i].rows[b].quota.WindowMinutes
		})
		if balance, ok := balances[groups[i].provider]; ok {
			groups[i].rows = append(groups[i].rows, limitRow{label: "Créditos", credits: &balance})
		}
	}
	return groups
}

// estimatedRows resume o consumo próprio de uma CLI nas janelas móveis.
//
// Só as móveis: elas são o paralelo da sessão e da semana que as outras CLIs
// publicam. "Hoje" e "este mês" são recortes de calendário e já aparecem nos
// cartões do topo.
func (m *Model) estimatedRows(provider string) []limitRow {
	var rows []limitRow
	for _, w := range m.report.Windows {
		if w.Minutes == 0 {
			continue
		}
		for _, s := range w.ByProvider {
			if s.Label != provider || s.Totals.Messages == 0 {
				continue
			}
			rows = append(rows, limitRow{
				label:   w.Label,
				summary: formatCost(s.Totals.Cost) + " · " + formatTokens(s.Totals.TotalTokens()) + " tokens",
			})
		}
	}
	return rows
}

// render desenha o bloco de uma CLI.
func (g limitGroup) render(m *Model, th *theme.Theme, inner int) string {
	tone := providerTone(th, g.provider)
	head := " " + component.Spread(
		lipgloss.NewStyle().Foreground(tone).Bold(true).Render(g.provider),
		th.Ghost.Render(g.note),
		inner-1,
	)

	lines := []string{head}
	for _, row := range g.rows {
		lines = append(lines, row.render(m, th, inner)...)
	}
	return strings.Join(lines, "\n")
}

// render desenha uma linha: barra, folga e prazo na primeira; ritmo ou
// valores absolutos na segunda, quando há espaço.
func (r limitRow) render(m *Model, th *theme.Theme, inner int) []string {
	// Trunca em labelColumn-3 para que sobre sempre uma coluna de separação
	// entre o rótulo mais longo e a barra.
	label := th.Dim.Render(component.PadRight("  "+component.TruncateTail(r.label, labelColumn-3), labelColumn))
	avail := max(inner-labelColumn, 4)
	detailed := r.height(inner) == 2

	if r.credits != nil {
		return r.renderCredits(th, label, avail, detailed)
	}
	if !r.reported {
		return []string{label + th.Body.Render(component.TruncateTail(r.summary, avail))}
	}

	now := m.now()
	w := r.quota
	tone := quotaTone(th, w.UsedPercent)

	right := lipgloss.NewStyle().Foreground(tone).Render(formatPercent(w.RemainingPercent()) + " livre")
	if reset := formatReset(w.ResetsAt, now); reset != "" {
		right += th.Ghost.Render("  " + reset)
	}
	if w.Expired(now) {
		right = th.Ghost.Render("renovada · sem leitura nova")
	}

	lines := []string{barLine(th, label, right, w.UsedPercent, tone, avail)}
	if detailed {
		pace := w.Pace(now)
		text := formatPace(pace)
		if text == "" {
			return append(lines, "")
		}
		// Só o déficit ganha cor: é a única leitura que pede uma decisão
		// agora. Pintar as três faria a tela alertar o tempo todo.
		style := th.Ghost
		if pace.Stage == coretokens.PaceDeficit {
			style = lipgloss.NewStyle().Foreground(th.Warning)
			if !pace.LastsToReset {
				style = lipgloss.NewStyle().Foreground(th.Danger)
			}
		}
		lines = append(lines,
			component.PadRight("", labelColumn)+style.Render(component.TruncateTail(text, avail)))
	}
	return lines
}

// renderCredits desenha o saldo extra: quanto sobra em dinheiro, e a conta
// completa embaixo. O saldo não tem prazo de renovação, então o lugar do
// countdown fica com o valor restante, que é a pergunta equivalente.
func (r limitRow) renderCredits(th *theme.Theme, label string, avail int, detailed bool) []string {
	c := *r.credits

	// Sem teto não há barra: uma carteira de saldo livre desenharia uma barra
	// sempre cheia ou sempre vazia, as duas mentindo.
	if !c.Metered() {
		text := formatMoney(c.Balance, c.Currency) + " de saldo"
		if c.Unlimited {
			text = "sem teto"
		}
		return []string{label + th.Body.Render(component.TruncateTail(text, avail))}
	}

	used := c.UsedPercent()
	tone := quotaTone(th, used)

	right := lipgloss.NewStyle().Foreground(tone).
		Render(formatMoney(c.Remaining(), c.Currency) + " livres")

	lines := []string{barLine(th, label, right, used, tone, avail)}
	if detailed {
		detail := formatMoney(c.Used, c.Currency) + " de " + formatMoney(c.Limit, c.Currency) +
			" · " + formatPercent(used) + " usado"
		if !c.Enabled {
			detail += " · desativado"
		}
		lines = append(lines,
			component.PadRight("", labelColumn)+th.Ghost.Render(component.TruncateTail(detail, avail)))
	}
	return lines
}

// barLine monta "rótulo · barra · leitura à direita", degradando para só a
// leitura quando a barra não caberia num tamanho honesto.
func barLine(th *theme.Theme, label, right string, percent float64, tone lipgloss.TerminalColor, avail int) string {
	barW := avail - lipgloss.Width(right) - 2
	if avail < meterMinWidth || barW < 6 {
		return label + component.Spread("", right, avail)
	}
	return label + component.Meter{Percent: percent, Width: barW, Tone: tone}.Render(th) + " " + right
}
