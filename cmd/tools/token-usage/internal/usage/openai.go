package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/mateuslh/lealing/cmd/tools/token-usage/internal/tokens"
)

// CodexUsageEndpoint é a rota que a conta do ChatGPT publica para o Codex.
const CodexUsageEndpoint = "https://chatgpt.com/backend-api/codex/usage"

// userAgent identifica o lealing nas chamadas.
//
// Não é enfeite: a borda da OpenAI recusa requisição sem User-Agent próprio,
// e responde com uma página de bloqueio em HTML. Vai o nosso nome, não o da
// CLI — quem atende do outro lado tem direito de saber quem está chamando.
const userAgent = "lealing"

// CodexQuota lê as cotas da conta do Codex.
//
// O log em disco também traz cota, mas com a idade do último uso: se a janela
// virou desde então, o percentual de lá descreve um ciclo encerrado. Esta
// consulta é a mesma que a CLI faz, com a sessão que ela já gravou.
type CodexQuota struct {
	Source   CodexCredentialSource
	Client   *http.Client
	Endpoint string
	Now      func() time.Time

	mu     sync.Mutex
	cached *quotaSnapshot
}

// NewCodexQuota monta o cliente com a fonte de credenciais escolhida pelo
// composition root.
func NewCodexQuota(source CodexCredentialSource) *CodexQuota {
	return &CodexQuota{
		Source:   source,
		Client:   &http.Client{Timeout: 8 * time.Second},
		Endpoint: CodexUsageEndpoint,
		Now:      time.Now,
	}
}

func (q *CodexQuota) fetch(ctx context.Context) ([]tokens.RateWindow, *tokens.Credits, error) {
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

func (q *CodexQuota) load(ctx context.Context, now time.Time) ([]tokens.RateWindow, *tokens.Credits, error) {
	if q.Source == nil {
		return nil, nil, ErrNoCodexCredentials
	}
	cred, err := q.Source.CodexCredential(ctx)
	if err != nil {
		return nil, nil, err
	}
	if cred.Expired(now) {
		return nil, nil, ErrCodexSessionExpired
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, q.endpoint(), nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+cred.AccessToken)
	if cred.AccountID != "" {
		// Sem isso a resposta é a da conta padrão, que não é a certa para
		// quem alterna entre pessoal e organização.
		req.Header.Set("chatgpt-account-id", cred.AccountID)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := q.client().Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("cota do Codex: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized:
		return nil, nil, ErrCodexSessionExpired
	default:
		return nil, nil, fmt.Errorf("cota do Codex: HTTP %d", resp.StatusCode)
	}

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, nil, fmt.Errorf("cota do Codex: %w", err)
	}
	return ParseCodexUsage(raw, now)
}

// RateWindows devolve as cotas da conta.
func (q *CodexQuota) RateWindows(ctx context.Context) ([]tokens.RateWindow, error) {
	windows, _, err := q.fetch(ctx)
	return windows, err
}

// Credits implementa tokens.CreditReporter.
func (q *CodexQuota) Credits(ctx context.Context) (*tokens.Credits, error) {
	_, credits, err := q.fetch(ctx)
	if err != nil {
		return nil, nil // o erro já viaja pelas cotas
	}
	return credits, nil
}

func (q *CodexQuota) now() time.Time {
	if q.Now == nil {
		return time.Now()
	}
	return q.Now()
}

func (q *CodexQuota) client() *http.Client {
	if q.Client == nil {
		return http.DefaultClient
	}
	return q.Client
}

func (q *CodexQuota) endpoint() string {
	if q.Endpoint == "" {
		return CodexUsageEndpoint
	}
	return q.Endpoint
}

// codexWindow é uma janela na resposta da conta.
type codexWindow struct {
	UsedPercent float64 `json:"used_percent"`
	// LimitWindowSeconds é a duração; zero significa janela não publicada.
	LimitWindowSeconds int   `json:"limit_window_seconds"`
	ResetAt            int64 `json:"reset_at"`
	ResetAfterSeconds  int64 `json:"reset_after_seconds"`
}

type codexRateLimit struct {
	Primary   *codexWindow `json:"primary_window"`
	Secondary *codexWindow `json:"secondary_window"`
}

type codexUsagePayload struct {
	PlanType        string          `json:"plan_type"`
	RateLimit       *codexRateLimit `json:"rate_limit"`
	CodeReviewLimit *codexRateLimit `json:"code_review_rate_limit"`
	Credits         *struct {
		Balance      string `json:"balance"`
		HasCredits   bool   `json:"has_credits"`
		Unlimited    bool   `json:"unlimited"`
		OverageLimit bool   `json:"overage_limit_reached"`
	} `json:"credits"`
}

// ParseCodexUsage traduz a resposta da conta em cotas e saldo.
func ParseCodexUsage(raw []byte, observedAt time.Time) ([]tokens.RateWindow, *tokens.Credits, error) {
	var payload codexUsagePayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, nil, fmt.Errorf("cota do Codex: resposta ilegível: %w", err)
	}

	source := "conta"
	if payload.PlanType != "" {
		source += " · " + payload.PlanType
	}

	var windows []tokens.RateWindow
	add := func(w *codexWindow, suffix string) {
		// Uma janela sem duração não permite calcular ritmo nem prazo, e
		// vinha nula nas contas que não têm aquele limite.
		if w == nil || w.LimitWindowSeconds <= 0 {
			return
		}
		minutes := w.LimitWindowSeconds / 60
		rate := tokens.RateWindow{
			Provider:      codexProvider,
			Label:         windowLabel(minutes) + suffix,
			UsedPercent:   w.UsedPercent,
			WindowMinutes: minutes,
			ObservedAt:    observedAt,
			Source:        source,
		}
		switch {
		case w.ResetAt > 0:
			rate.ResetsAt = time.Unix(w.ResetAt, 0)
		case w.ResetAfterSeconds > 0:
			rate.ResetsAt = observedAt.Add(time.Duration(w.ResetAfterSeconds) * time.Second)
		}
		windows = append(windows, rate)
	}

	if rl := payload.RateLimit; rl != nil {
		add(rl.Primary, "")
		add(rl.Secondary, "")
	}
	if rl := payload.CodeReviewLimit; rl != nil {
		add(rl.Primary, " · review")
		add(rl.Secondary, " · review")
	}

	var credits *tokens.Credits
	if c := payload.Credits; c != nil && (c.HasCredits || c.Unlimited) {
		// O saldo vem como texto para não perder precisão no trânsito; um
		// valor que não converte vira zero, e a conta ainda aparece como
		// contratada em vez de sumir da tela.
		balance, _ := strconv.ParseFloat(c.Balance, 64)
		credits = &tokens.Credits{
			Provider:  codexProvider,
			Balance:   balance,
			Enabled:   c.HasCredits,
			Unlimited: c.Unlimited,
		}
	}
	return windows, credits, nil
}
