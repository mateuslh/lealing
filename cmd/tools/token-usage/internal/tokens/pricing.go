package tokens

import "strings"

// Rate é o preço de um modelo em dólares por milhão de tokens da tool.
type Rate struct {
	Input  float64
	Output float64
}

// Os preços da OpenAI são estimativas e podem variar por conta e por modelo.
// Os da Anthropic seguem a tabela pública.
var priceTable = map[string]Rate{
	// Anthropic
	"claude-fable-5":    {10, 50},
	"claude-mythos-5":   {10, 50},
	"claude-opus-5":     {5, 25},
	"claude-opus-4-8":   {5, 25},
	"claude-opus-4-7":   {5, 25},
	"claude-opus-4-6":   {5, 25},
	"claude-opus-4-5":   {5, 25},
	"claude-opus-4-1":   {15, 75},
	"claude-opus-4-0":   {15, 75},
	"claude-sonnet-5":   {3, 15},
	"claude-sonnet-4-6": {3, 15},
	"claude-sonnet-4-5": {3, 15},
	"claude-sonnet-4-0": {3, 15},
	"claude-haiku-4-5":  {1, 5},

	// OpenAI (estimativas)
	"gpt-5.5":      {1.25, 10},
	"gpt-5":        {1.25, 10},
	"gpt-5-codex":  {1.25, 10},
	"gpt-5-mini":   {0.25, 2},
	"gpt-5-nano":   {0.05, 0.40},
	"gpt-4.1":      {2, 8},
	"gpt-4.1-mini": {0.40, 1.60},
	"gpt-4o":       {2.5, 10},
	"o3":           {2, 8},
	"o4-mini":      {1.1, 4.4},
}

// RateFor resolve o preço de um modelo, caindo para heurísticas por família
// quando o identificador exato não está na tabela — modelos novos aparecem
// nos logs antes de a tabela ser atualizada, e estimar é melhor que zerar.
func RateFor(model string) (Rate, bool) {
	if r, ok := priceTable[model]; ok {
		return r, true
	}

	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "opus"):
		return Rate{5, 25}, true
	case strings.Contains(m, "sonnet"):
		return Rate{3, 15}, true
	case strings.Contains(m, "haiku"):
		return Rate{1, 5}, true
	case strings.Contains(m, "gpt-5"):
		return Rate{1.25, 10}, true
	case strings.Contains(m, "gpt-4"):
		return Rate{2.5, 10}, true
	case strings.HasPrefix(m, "o3"), strings.HasPrefix(m, "o4"):
		return Rate{2, 8}, true
	case strings.Contains(m, "gpt"):
		return Rate{2.5, 10}, true
	}
	return Rate{}, false
}

// Multiplicadores de cache. Escrita de cache custa mais que input normal;
// leitura custa uma fração.
const (
	cacheWriteMultiplier = 1.25
	cacheReadMultiplier  = 0.10
	perMillion           = 1_000_000
)

// Cost estima o custo em dólares de uma mensagem.
func Cost(model string, input, output, cacheCreation, cacheRead int) float64 {
	rate, ok := RateFor(model)
	if !ok {
		return 0
	}
	dollars := float64(input)*rate.Input +
		float64(output)*rate.Output +
		float64(cacheCreation)*rate.Input*cacheWriteMultiplier +
		float64(cacheRead)*rate.Input*cacheReadMultiplier
	return dollars / perMillion
}
