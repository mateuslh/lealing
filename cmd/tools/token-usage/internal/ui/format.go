// Package tokens é a tela da tool "Uso de Tokens".
package tokens

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	coretokens "github.com/mateuslh/lealing/cmd/tools/token-usage/internal/tokens"
	"github.com/mateuslh/lealing/sdk/component"
)

// formatCost apresenta valores em dólar com precisão proporcional à grandeza:
// centavos importam em $0,43 e são ruído em $1.204.
func formatCost(v float64) string {
	switch {
	case v == 0:
		return "$0"
	case v < 0.01:
		return "<$0,01"
	case v < 100:
		return fmt.Sprintf("$%.2f", v)
	case v < 10_000:
		return fmt.Sprintf("$%.0f", v)
	default:
		return fmt.Sprintf("$%.1fk", v/1000)
	}
}

// formatMoney apresenta um saldo na moeda da conta.
//
// Os créditos são cobrados na moeda do cartão, que nem sempre é dólar: o
// mesmo número em reais e em dólares são decisões diferentes, e o símbolo é
// o que separa as duas leituras.
func formatMoney(v float64, currency string) string {
	switch currency {
	case "", "USD":
		return fmt.Sprintf("$%.2f", v)
	case "BRL":
		// Vírgula decimal: um saldo em reais escrito com ponto é lido como
		// milhar por quem está olhando rápido.
		return "R$ " + strings.Replace(fmt.Sprintf("%.2f", v), ".", ",", 1)
	case "EUR":
		return fmt.Sprintf("€%.2f", v)
	default:
		return fmt.Sprintf("%.2f %s", v, currency)
	}
}

// formatTokens abrevia contagens grandes, que é o caso normal aqui —
// centenas de milhões de tokens não cabem legíveis por extenso.
func formatTokens(n int) string {
	switch v := float64(n); {
	case n < 1_000:
		return fmt.Sprintf("%d", n)
	case n < 1_000_000:
		return fmt.Sprintf("%.1fk", v/1_000)
	case n < 1_000_000_000:
		return fmt.Sprintf("%.1fM", v/1_000_000)
	default:
		return fmt.Sprintf("%.2fB", v/1_000_000_000)
	}
}

// formatPercent arredonda para inteiro, exceto abaixo de 10%.
func formatPercent(v float64) string {
	if v < 10 {
		return fmt.Sprintf("%.1f%%", v)
	}
	return fmt.Sprintf("%.0f%%", v)
}

// formatReset descreve quanto falta para uma cota reiniciar.
func formatReset(at time.Time, now time.Time) string {
	if at.IsZero() {
		return ""
	}
	if d := at.Sub(now); d <= 0 {
		return "renovada"
	}
	return "renova em " + formatDuration(at.Sub(now))
}

// formatDuration escreve um intervalo com a precisão que a grandeza pede:
// minutos até uma hora, horas e minutos até um dia, dias depois disso.
func formatDuration(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "<1min"
	case d < time.Hour:
		return fmt.Sprintf("%dmin", int(d.Minutes()))
	case d < 24*time.Hour:
		h := int(d.Hours())
		if m := int(d.Minutes()) % 60; m > 0 {
			return fmt.Sprintf("%dh%02d", h, m)
		}
		return fmt.Sprintf("%dh", h)
	default:
		days := int(d.Hours() / 24)
		if h := int(d.Hours()) % 24; h > 0 {
			return fmt.Sprintf("%dd %dh", days, h)
		}
		return fmt.Sprintf("%dd", days)
	}
}

// formatPace traduz o ritmo em uma frase: onde o consumo está em relação ao
// relógio da janela e o que isso projeta.
//
// O desvio vai em pontos percentuais, não em "%": a diferença entre 90% usado
// e 50% previsto são 40 pontos, e chamar isso de 40% confundiria com a cota.
func formatPace(p coretokens.Pace) string {
	if !p.Known() {
		return ""
	}
	// Nos primeiros minutos de uma janela não há amostra: "previsto 0%" e
	// "em dia" são verdades vazias que só ocupam a linha.
	if p.ElapsedPercent < 1 {
		return "janela recém-renovada"
	}

	var head string
	switch p.Stage {
	case coretokens.PaceDeficit:
		head = fmt.Sprintf("ritmo %.0f pts acima do previsto", p.DeltaPercent)
	case coretokens.PaceReserve:
		head = fmt.Sprintf("ritmo %.0f pts de folga", -p.DeltaPercent)
	default:
		head = "ritmo em dia"
	}

	tail := "dura até renovar"
	if !p.LastsToReset {
		tail = "esvazia em " + formatDuration(p.ETA)
		if p.ETA <= 0 {
			tail = "cota esgotada"
		}
	}
	return fmt.Sprintf("%s · previsto %s · %s", head, formatPercent(p.ExpectedPercent), tail)
}

// providerTone dá a cada CLI uma cor fixa, para que a mesma fonte seja
// reconhecível em todos os painéis da tela.
func providerTone(th *component.Theme, provider string) lipgloss.TerminalColor {
	switch provider {
	case "Claude Code":
		return th.Warning
	case "Codex":
		return th.Accent
	default:
		return th.Secondary
	}
}

// modelTone colore pela família do modelo.
func modelTone(th *component.Theme, model string) lipgloss.TerminalColor {
	switch {
	case contains(model, "opus"):
		return th.Danger
	case contains(model, "sonnet"):
		return th.Warning
	case contains(model, "haiku"):
		return th.Success
	case contains(model, "gpt"), contains(model, "o3"), contains(model, "o4"):
		return th.Accent
	default:
		return th.Muted
	}
}

// contains é um strings.Contains sem sensibilidade a caixa, restrito ao que
// esta tela precisa.
func contains(s, sub string) bool {
	if len(sub) > len(s) {
		return false
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		match := true
		for j := range sub {
			a, b := s[i+j], sub[j]
			if a >= 'A' && a <= 'Z' {
				a += 32
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// quotaTone verde/âmbar/vermelho conforme a cota consumida.
func quotaTone(th *component.Theme, usedPercent float64) lipgloss.TerminalColor {
	switch {
	case usedPercent >= 90:
		return th.Danger
	case usedPercent >= 70:
		return th.Warning
	default:
		return th.Success
	}
}

// dailyCosts extrai a série de custo diário para a sparkline.
func dailyCosts(days []coretokens.DayPoint) []float64 {
	out := make([]float64, len(days))
	for i, d := range days {
		out[i] = d.Totals.Cost
	}
	return out
}
