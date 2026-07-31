package usage

import (
	"context"
	"errors"
	"time"

	"github.com/mateuslh/lealing/cmd/tools/token-usage/internal/tokens"
)

// claudeProvider é o nome que rotula tudo que vem do Claude Code. Fica numa
// constante porque o leitor de logs e o cliente de cotas precisam concordar
// nele — é a chave que junta os dois no mesmo bloco da tela.
const claudeProvider = "Claude Code"

// ClaudeCode lê ~/.claude/projects/**/*.jsonl.
//
// Cada mensagem de assistant traz `message.usage` com a contagem do turno.
type ClaudeCode struct {
	// Root permite apontar para outro diretório em testes.
	Root string
	// Quota consulta as cotas da conta, que não existem em disco. Nil
	// desliga a consulta e a tela cai no consumo que medimos localmente.
	Quota *ClaudeQuota
}

var (
	_ tokens.Provider       = (*ClaudeCode)(nil)
	_ tokens.CreditReporter = (*ClaudeCode)(nil)
)

// NewClaudeCode monta o provedor com caminhos e credenciais explícitos.
func NewClaudeCode(root string, credentials CredentialSource) *ClaudeCode {
	return &ClaudeCode{Root: root, Quota: NewClaudeQuota(credentials)}
}

// Name implementa tokens.Provider.
func (c *ClaudeCode) Name() string { return claudeProvider }

// Collect implementa tokens.Provider.
func (c *ClaudeCode) Collect(ctx context.Context) ([]tokens.Record, error) {
	var records []tokens.Record
	// A mesma mensagem aparece em mais de um arquivo quando uma sessão é
	// retomada; sem deduplicar por uuid, o custo é contado duas vezes.
	seen := make(map[string]struct{}, 4096)

	err := scanJSONL(ctx, c.Root, nil, func(entry map[string]any) {
		if str(entry, "type") != "assistant" {
			return
		}
		message := obj(entry, "message")
		if message == nil {
			return
		}
		u := obj(message, "usage")
		if u == nil {
			return
		}

		if uuid := str(entry, "uuid"); uuid != "" {
			if _, dup := seen[uuid]; dup {
				return
			}
			seen[uuid] = struct{}{}
		}

		model := str(message, "model")
		if model == "" {
			model = "desconhecido"
		}

		input := num(u, "input_tokens")
		output := num(u, "output_tokens")
		cacheCreation := num(u, "cache_creation_input_tokens")
		cacheRead := num(u, "cache_read_input_tokens")
		ts := str(entry, "timestamp")

		records = append(records, tokens.Record{
			Provider:      c.Name(),
			Model:         model,
			Day:           dayOf(ts),
			Timestamp:     parseTime(ts),
			Project:       projectOf(str(entry, "cwd")),
			Input:         input,
			Output:        output,
			CacheCreation: cacheCreation,
			CacheRead:     cacheRead,
			Cost:          tokens.Cost(model, input, output, cacheCreation, cacheRead),
		})
	})

	return records, err
}

// RateWindows implementa tokens.Provider consultando a conta: os logs locais
// contam o que foi gasto, mas só o servidor sabe o tamanho da cota.
func (c *ClaudeCode) RateWindows(ctx context.Context) ([]tokens.RateWindow, error) {
	if c.Quota == nil {
		return nil, nil
	}
	windows, err := c.Quota.RateWindows(ctx)
	if errors.Is(err, ErrNoCredentials) {
		// Quem nunca autenticou a CLI não tem cota para mostrar. Isso é
		// ausência de dado, não falha: reportar como erro marcaria o
		// relatório inteiro como parcial em toda máquina sem Claude Code.
		return nil, nil
	}
	return windows, err
}

// Credits implementa tokens.CreditReporter.
func (c *ClaudeCode) Credits(ctx context.Context) (*tokens.Credits, error) {
	if c.Quota == nil {
		return nil, nil
	}
	return c.Quota.Credits(ctx)
}

// Codex lê ~/.codex/sessions/**/*.jsonl.
//
// O consumo vem em eventos `token_count`; modelo e projeto vêm do
// `turn_context` ou `session_meta` mais recente do arquivo, então a leitura
// precisa ser sequencial e por arquivo, não global.
type Codex struct {
	Root string
	// Quota consulta a conta. Nil deixa a tela só com o que o log diz.
	Quota *CodexQuota
}

var (
	_ tokens.Provider       = (*Codex)(nil)
	_ tokens.CreditReporter = (*Codex)(nil)
)

// codexProvider rotula tudo que vem do Codex, no log e na conta.
const codexProvider = "Codex"

// NewCodex monta o provedor com caminhos e credenciais explícitos.
func NewCodex(
	root string,
	credentials CodexCredentialSource,
) *Codex {
	return &Codex{Root: root, Quota: NewCodexQuota(credentials)}
}

// Name implementa tokens.Provider.
func (c *Codex) Name() string { return codexProvider }

// Collect implementa tokens.Provider.
func (c *Codex) Collect(ctx context.Context) ([]tokens.Record, error) {
	var records []tokens.Record

	// Modelo e projeto valem só dentro do arquivo em que foram declarados,
	// então são reiniciados a cada sessão. Sem isso, um arquivo sem
	// `session_meta` herdaria o modelo do arquivo lido antes dele.
	const (
		unknownModel   = "gpt (desconhecido)"
		unknownProject = "—"
	)
	model, project := unknownModel, unknownProject

	reset := func() { model, project = unknownModel, unknownProject }

	err := scanJSONL(ctx, c.Root, reset, func(entry map[string]any) {
		payload := obj(entry, "payload")
		if payload == nil {
			return
		}

		switch str(entry, "type") {
		case "session_meta", "turn_context":
			if m := str(payload, "model"); m != "" {
				model = m
			}
			if cwd := str(payload, "cwd"); cwd != "" {
				project = projectOf(cwd)
			}
			return

		case "event_msg":
			if str(payload, "type") != "token_count" {
				return
			}
			info := obj(payload, "info")
			if info == nil {
				return
			}
			last := obj(info, "last_token_usage")
			if last == nil {
				return
			}

			// input_tokens já inclui o que foi servido do cache; o custo
			// cheio se aplica só à diferença.
			totalInput := num(last, "input_tokens")
			cached := num(last, "cached_input_tokens")
			input := max(totalInput-cached, 0)
			output := num(last, "output_tokens")
			ts := str(entry, "timestamp")

			records = append(records, tokens.Record{
				Provider:  c.Name(),
				Model:     model,
				Day:       dayOf(ts),
				Timestamp: parseTime(ts),
				Project:   project,
				Input:     input,
				Output:    output,
				CacheRead: cached,
				Cost:      tokens.Cost(model, input, output, 0, cached),
			})
		}
	})

	return records, err
}

// RateWindows implementa tokens.Provider preferindo a conta e caindo no log.
//
// O log é um retrato do último uso: se a janela virou desde então, ele
// descreve um ciclo que já acabou. Mas é o que resta quando não há sessão ou
// rede, e uma cota velha rotulada como velha vale mais que nenhuma.
func (c *Codex) RateWindows(ctx context.Context) ([]tokens.RateWindow, error) {
	if c.Quota == nil {
		return c.rateWindowsFromLogs(ctx)
	}

	windows, err := c.Quota.RateWindows(ctx)
	if err == nil && len(windows) > 0 {
		return windows, nil
	}

	fallback, logErr := c.rateWindowsFromLogs(ctx)
	if logErr != nil {
		return nil, logErr
	}
	if errors.Is(err, ErrNoCodexCredentials) {
		// Sem sessão não há falha a relatar: quem não autenticou a CLI
		// simplesmente não tem cota na conta para consultar.
		return fallback, nil
	}
	return fallback, err
}

// Credits implementa tokens.CreditReporter.
func (c *Codex) Credits(ctx context.Context) (*tokens.Credits, error) {
	if c.Quota == nil {
		return nil, nil
	}
	return c.Quota.Credits(ctx)
}

// rateWindowsFromLogs extrai as cotas do evento `token_count` mais recente
// que as reportou.
func (c *Codex) rateWindowsFromLogs(ctx context.Context) ([]tokens.RateWindow, error) {
	var (
		latestAt     time.Time
		latestLimits map[string]any
	)

	err := scanJSONL(ctx, c.Root, nil, func(entry map[string]any) {
		if str(entry, "type") != "event_msg" {
			return
		}
		payload := obj(entry, "payload")
		if payload == nil || str(payload, "type") != "token_count" {
			return
		}
		limits := obj(payload, "rate_limits")
		if limits == nil {
			return
		}
		at := parseTime(str(entry, "timestamp"))
		if at.IsZero() || !at.After(latestAt) {
			return
		}
		latestAt, latestLimits = at, limits
	})
	if err != nil || latestLimits == nil {
		return nil, err
	}

	var out []tokens.RateWindow
	for _, key := range []string{"primary", "secondary"} {
		window := obj(latestLimits, key)
		if window == nil {
			continue
		}
		used, ok := float(window, "used_percent")
		if !ok {
			continue
		}
		minutes := num(window, "window_minutes")
		rate := tokens.RateWindow{
			Provider:      c.Name(),
			Label:         windowLabel(minutes),
			UsedPercent:   used,
			WindowMinutes: minutes,
			ObservedAt:    latestAt,
			Source:        "log local",
		}
		if secs, ok := float(window, "resets_at"); ok {
			rate.ResetsAt = time.Unix(int64(secs), 0)
		}
		out = append(out, rate)
	}
	return out, nil
}

// windowLabel dá nome à janela a partir da duração em minutos.
//
// O nome sai da duração, não da posição na resposta: o Codex já publicou
// versões em que a janela `primary` é a semanal, e chamar de "sessão" o que
// dura sete dias é pior que não nomear.
//
// Forma curta de propósito: o rótulo divide uma coluna estreita com a barra
// de preenchimento, e "5 horas" empurraria a barra para fora da tela.
func windowLabel(minutes int) string {
	switch {
	case minutes <= 0:
		return "Janela"
	case minutes < 1440:
		return "Sessão " + itoa(minutes/60) + "h"
	case minutes < 10080:
		return itoa(minutes/1440) + " dias"
	default:
		return "Semana"
	}
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
