package component

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/truncate"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
)

// Row é uma linha rótulo/valor. Selected e Control permitem que a mesma
// primitiva sirva tanto a uma tela somente-leitura quanto a um formulário.
type Row struct {
	Label string
	Value string
	// Control substitui o valor por um widget (interruptor, stepper).
	Control string
	// Hint é um comentário apagado à direita do valor.
	Hint string
	// Tone colore o valor; nil usa o tom padrão.
	Tone lipgloss.TerminalColor
	// LabelTone colore o rótulo. É como um formulário marca a linha editada
	// sem pendurar um símbolo extra ao lado de um controle que já é largo.
	LabelTone lipgloss.TerminalColor
	// Disabled esmaece a linha inteira: a opção existe mas não se aplica
	// nesta fonte de energia, por exemplo.
	Disabled bool
}

// FieldList desenha linhas rótulo/valor com os rótulos alinhados em coluna.
//
// O alinhamento é calculado sobre o conjunto todo, não linha a linha: é o que
// faz uma lista de propriedades parecer uma tabela em vez de texto solto.
type FieldList struct {
	Rows     []Row
	Width    int
	Selected int
	// Focusable liga o cursor de seleção.
	Focusable bool
	Focused   bool
	// LabelWidth fixa a coluna de rótulos; zero calcula pelo maior.
	LabelWidth int
}

// Render devolve as linhas prontas.
func (f FieldList) Render(th *theme.Theme) string {
	if len(f.Rows) == 0 || f.Width <= 0 {
		return ""
	}

	labelW := f.LabelWidth
	if labelW == 0 {
		for _, r := range f.Rows {
			labelW = max(labelW, lipgloss.Width(r.Label))
		}
	}
	// A coluna de rótulos nunca passa de metade da largura: além disso, o
	// valor — que é o dado — fica sem espaço.
	labelW = min(labelW, f.Width/2)

	lines := make([]string, len(f.Rows))
	for i, row := range f.Rows {
		lines[i] = f.renderRow(th, row, i == f.Selected, labelW)
	}
	return strings.Join(lines, "\n")
}

func (f FieldList) renderRow(th *theme.Theme, row Row, selected bool, labelW int) string {
	cursor := "  "
	labelStyle := th.Dim
	valueStyle := th.Body

	switch {
	case row.Disabled:
		labelStyle, valueStyle = th.Ghost, th.Ghost
	case f.Focusable && selected:
		cursor = th.Cursor.Render("▎") + " "
		labelStyle = th.Body
		if f.Focused {
			valueStyle = th.Body.Bold(true)
		}
	}

	if row.Tone != nil && !row.Disabled {
		valueStyle = lipgloss.NewStyle().Foreground(row.Tone)
	}
	if row.LabelTone != nil && !row.Disabled {
		labelStyle = lipgloss.NewStyle().Foreground(row.LabelTone)
	}

	// truncateTail só corta quando excede: chamar o truncate incondicionalmente
	// reserva espaço para as reticências mesmo em rótulos que cabiam inteiros.
	label := labelStyle.Render(padRight(truncateTail(row.Label, labelW), labelW))

	value := row.Value
	if row.Control != "" {
		value = row.Control
	}
	rendered := valueStyle.Render(value)
	if row.Control != "" && !row.Disabled {
		rendered = row.Control // controles já vêm estilizados
	}

	if row.Hint != "" {
		rendered += "  " + th.Ghost.Render(row.Hint)
	}

	prefix := cursor + label + "  "
	avail := max(f.Width-lipgloss.Width(prefix), 4)
	if lipgloss.Width(rendered) > avail {
		rendered = truncate.StringWithTail(rendered, uint(avail), "…")
	}
	return prefix + rendered
}

// PadRight completa a string com espaços até a largura visual, para alinhar
// colunas em blocos montados fora do FieldList.
func PadRight(s string, width int) string { return padRight(s, width) }

// padRight completa a string com espaços até a largura.
func padRight(s string, width int) string {
	if gap := width - lipgloss.Width(s); gap > 0 {
		return s + strings.Repeat(" ", gap)
	}
	return s
}

// Toggle desenha um interruptor no estilo dos ajustes do macOS.
//
// A cor acompanha o estado, nunca o foco: num painel em que só a linha
// selecionada mostra o verde, ler a configuração exige descer campo a campo.
func Toggle(th *theme.Theme, on bool, focused bool) string {
	if on {
		track := lipgloss.NewStyle().Foreground(th.Success)
		return arrows(th, track.Render("●━")+th.Ghost.Render(" ligado"), focused)
	}
	track := lipgloss.NewStyle().Foreground(th.Faint)
	return arrows(th, th.Ghost.Render("━")+track.Render("●")+th.Ghost.Render(" desligado"), focused)
}

// Stepper desenha um valor entre setas, indicando que é ajustável.
func Stepper(th *theme.Theme, value string, focused bool) string {
	return arrows(th, th.Body.Render(value), focused)
}

// arrows envolve um controle nas setas que anunciam quem o altera. Interruptor
// e seletor usam a mesma moldura porque respondem à mesma tecla — desenhar
// setas só em um dos dois sugeriria que o outro se muda de outro jeito.
//
// Fora do foco as setas viram espaço da mesma largura: repeti-las em dez
// linhas transforma a coluna em ruído, e apagá-las sem reservar o espaço faria
// os valores pularem de posição a cada movimento do cursor.
func arrows(th *theme.Theme, body string, focused bool) string {
	if !focused {
		return "  " + body + "  "
	}
	return th.Cursor.Render("‹ ") + body + th.Cursor.Render(" ›")
}

// TruncateTail encurta com reticências respeitando a largura visual, sem
// reservar espaço para o tail quando o texto já cabe.
func TruncateTail(s string, width int) string { return truncateTail(s, width) }

// truncateTail encurta com reticências respeitando a largura visual.
func truncateTail(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	return truncate.StringWithTail(s, uint(max(width, 0)), "…")
}

// Center empilha as linhas no meio do retângulo, truncando o que não couber.
//
// lipgloss.Place posiciona mas não recorta: uma mensagem mais larga que o
// frame sai por fora dele. Telas de carregamento, erro e estado vazio passam
// por aqui justamente porque o texto delas é o mais imprevisível.
func Center(width, height int, lines ...string) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	fitted := make([]string, len(lines))
	for i, line := range lines {
		fitted[i] = truncateTail(line, width)
	}
	block := lipgloss.JoinVertical(lipgloss.Center, fitted...)
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, block)
}
