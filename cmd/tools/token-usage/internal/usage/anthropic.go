package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/mateuslh/lealing/cmd/tools/token-usage/internal/tokens"
)

// UsageEndpoint é a rota que a própria CLI consulta para desenhar seu `/usage`.
const UsageEndpoint = "https://api.anthropic.com/api/oauth/usage"

// oauthBeta é o cabeçalho que libera a credencial de OAuth nesta rota.
const oauthBeta = "oauth-2025-04-20"

// quotaCacheTTL deduplica as duas perguntas que um mesmo relatório faz —
// cotas e créditos vêm na mesma resposta, e buscá-la duas vezes por refresh
// seria uma ida à rede jogada fora.
const quotaCacheTTL = 5 * time.Second

// ClaudeQuota lê as cotas da conta do Claude Code.
//
// Os logs em disco não trazem cota nenhuma: o percentual das janelas de 5h e
// de 7 dias existe só do lado do servidor. A sessão usada é a que a própria
// CLI gravou no chaveiro, e a leitura é a mesma que ela faz — nenhum segredo
// novo é criado nem gravado.
type ClaudeQuota struct {
	Source   CredentialSource
	Client   *http.Client
	Endpoint string
	Now      func() time.Time

	mu     sync.Mutex
	cached *quotaSnapshot
}

// quotaSnapshot é a última resposta aproveitável.
type quotaSnapshot struct {
	at      time.Time
	windows []tokens.RateWindow
	credits *tokens.Credits
	err     error
}

// NewClaudeQuota monta o cliente com a fonte de credenciais escolhida pelo
// composition root.
func NewClaudeQuota(source CredentialSource) *ClaudeQuota {
	return &ClaudeQuota{
		Source:   source,
		Client:   &http.Client{Timeout: 8 * time.Second},
		Endpoint: UsageEndpoint,
		Now:      time.Now,
	}
}

// fetch devolve cotas e saldo, reaproveitando a resposta recente.
func (q *ClaudeQuota) fetch(ctx context.Context) ([]tokens.RateWindow, *tokens.Credits, error) {
	now := q.now()

	q.mu.Lock()
	defer q.mu.Unlock()
	if q.cached != nil && now.Sub(q.cached.at) < quotaCacheTTL {
		return q.cached.windows, q.cached.credits, q.cached.err
	}

	windows, credits, err := q.load(ctx, now)
	q.cached = &quotaSnapshot{at: now, windows: windows, credits: credits, err: err}
	return windows, credits, err
}

func (q *ClaudeQuota) load(ctx context.Context, now time.Time) ([]tokens.RateWindow, *tokens.Credits, error) {
	if q.Source == nil {
		return nil, nil, ErrNoCredentials
	}
	cred, err := q.Source.Credential(ctx)
	if err != nil {
		return nil, nil, err
	}
	if cred.Expired(now) {
		return nil, nil, ErrSessionExpired
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, q.endpoint(), nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cred.AccessToken)
	req.Header.Set("anthropic-beta", oauthBeta)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "lealing")

	resp, err := q.client().Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("cota do Claude Code: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, nil, ErrSessionExpired
	default:
		// O corpo do erro pode ecoar dados da conta; o status já basta para
		// o usuário saber que a consulta falhou.
		return nil, nil, fmt.Errorf("cota do Claude Code: HTTP %d", resp.StatusCode)
	}

	// Teto de leitura: a resposta normal tem poucos kilobytes, e um corpo
	// gigante travaria a varredura inteira.
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("cota do Claude Code: %w", err)
	}
	windows, credits, err := ParseUsage(raw, now)
	// O plano vem na credencial, não na resposta: dizer "conta · pro" no
	// cabeçalho é o que explica por que os limites são os que são.
	if cred.Plan != "" {
		for i := range windows {
			windows[i].Source += " · " + cred.Plan
		}
	}
	return windows, credits, err
}

// RateWindows implementa a porta de cotas do provedor.
func (q *ClaudeQuota) RateWindows(ctx context.Context) ([]tokens.RateWindow, error) {
	windows, _, err := q.fetch(ctx)
	return windows, err
}

// Credits implementa tokens.CreditReporter.
func (q *ClaudeQuota) Credits(ctx context.Context) (*tokens.Credits, error) {
	_, credits, err := q.fetch(ctx)
	if err != nil {
		// O erro já viaja pelas cotas; repeti-lo encheria a tela com a mesma
		// frase duas vezes.
		return nil, nil
	}
	return credits, nil
}

func (q *ClaudeQuota) now() time.Time {
	if q.Now == nil {
		return time.Now()
	}
	return q.Now()
}

func (q *ClaudeQuota) client() *http.Client {
	if q.Client == nil {
		return http.DefaultClient
	}
	return q.Client
}

func (q *ClaudeQuota) endpoint() string {
	if q.Endpoint == "" {
		return UsageEndpoint
	}
	return q.Endpoint
}

// usageWindow é uma janela na resposta da API.
type usageWindow struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    string  `json:"resets_at"`
}

// usagePayload é o recorte da resposta que a tela consome. Os campos que a
// API traz e não usamos ficam de fora de propósito: declará-los criaria a
// impressão de que a tela os mostra.
type usagePayload struct {
	FiveHour       *usageWindow `json:"five_hour"`
	SevenDay       *usageWindow `json:"seven_day"`
	SevenDayOpus   *usageWindow `json:"seven_day_opus"`
	SevenDaySonnet *usageWindow `json:"seven_day_sonnet"`
	ExtraUsage     *struct {
		IsEnabled    bool    `json:"is_enabled"`
		MonthlyLimit float64 `json:"monthly_limit"`
		UsedCredits  float64 `json:"used_credits"`
		Currency     string  `json:"currency"`
		// DecimalPlaces diz em que unidade os dois valores acima vêm: com 2,
		// 27500 são R$ 275,00. Tomar o número cru multiplicaria a conta por
		// cem na tela.
		DecimalPlaces int `json:"decimal_places"`
	} `json:"extra_usage"`
}

// ParseUsage traduz a resposta da API em cotas e saldo.
func ParseUsage(raw []byte, observedAt time.Time) ([]tokens.RateWindow, *tokens.Credits, error) {
	var payload usagePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, nil, fmt.Errorf("cota do Claude Code: resposta ilegível: %w", err)
	}

	defs := []struct {
		window  *usageWindow
		label   string
		minutes int
	}{
		{payload.FiveHour, "Sessão 5h", 5 * 60},
		{payload.SevenDay, "Semana", 7 * 24 * 60},
		{payload.SevenDayOpus, "Opus 7d", 7 * 24 * 60},
		{payload.SevenDaySonnet, "Sonnet 7d", 7 * 24 * 60},
	}

	var windows []tokens.RateWindow
	for _, def := range defs {
		if def.window == nil {
			continue
		}
		windows = append(windows, tokens.RateWindow{
			Provider:      claudeProvider,
			Label:         def.label,
			UsedPercent:   def.window.Utilization,
			WindowMinutes: def.minutes,
			ResetsAt:      parseTime(def.window.ResetsAt),
			ObservedAt:    observedAt,
			Source:        "conta",
		})
	}

	var credits *tokens.Credits
	if e := payload.ExtraUsage; e != nil && (e.IsEnabled || e.UsedCredits > 0) {
		scale := 1.0
		for range e.DecimalPlaces {
			scale *= 10
		}
		credits = &tokens.Credits{
			Provider: claudeProvider,
			Used:     e.UsedCredits / scale,
			Limit:    e.MonthlyLimit / scale,
			Currency: e.Currency,
			Enabled:  e.IsEnabled,
		}
	}
	return windows, credits, nil
}
