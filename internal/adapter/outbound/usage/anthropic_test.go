package usage_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mateuslh/lealing/internal/adapter/outbound/usage"
)

// usageResponse é uma resposta real da rota de uso, com os campos que não
// consumimos preservados: eles precisam continuar sendo ignorados sem quebrar
// a leitura.
const usageResponse = `{
  "five_hour": {"utilization": 53.0, "resets_at": "2026-07-31T01:49:59.628487+00:00",
                "limit_dollars": null, "used_dollars": null},
  "seven_day": {"utilization": 31.0, "resets_at": "2026-08-01T04:59:59.628509+00:00"},
  "seven_day_opus": null,
  "seven_day_sonnet": null,
  "extra_usage": {"is_enabled": true, "monthly_limit": 27500, "used_credits": 8389.0,
                  "utilization": 30.5, "currency": "BRL", "decimal_places": 2},
  "limits": [{"kind": "session", "percent": 53, "resets_at": "2026-07-31T01:49:59.628487+00:00"}],
  "member_dashboard_available": false
}`

func observedAt() time.Time { return time.Date(2026, 7, 30, 22, 0, 0, 0, time.UTC) }

func TestParseUsageLeCotasDeSessaoESemana(t *testing.T) {
	windows, _, err := usage.ParseUsage([]byte(usageResponse), observedAt())
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(windows) != 2 {
		t.Fatalf("%d janelas, queria 2 (as de opus e sonnet vêm nulas)", len(windows))
	}

	session := windows[0]
	if session.Label != "Sessão 5h" || session.WindowMinutes != 300 {
		t.Errorf("primeira janela = %q/%dmin, queria a sessão de 5h", session.Label, session.WindowMinutes)
	}
	if session.UsedPercent != 53 {
		t.Errorf("uso da sessão = %v, queria 53", session.UsedPercent)
	}
	if got := session.RemainingPercent(); got != 47 {
		t.Errorf("folga da sessão = %v, queria 47", got)
	}
	if session.ResetsAt.IsZero() {
		t.Error("sem horário de renovação não há countdown")
	}
	if session.Source == "" {
		t.Error("sem origem a tela não sabe se o número é de agora ou do último uso")
	}

	if week := windows[1]; week.WindowMinutes != 7*24*60 || week.UsedPercent != 31 {
		t.Errorf("segunda janela = %dmin/%v%%, queria a semanal em 31%%", week.WindowMinutes, week.UsedPercent)
	}
}

// A API manda os valores na unidade menor da moeda; ler o número cru
// multiplicaria o saldo por cem na tela.
func TestParseUsageConverteCreditosParaAMoedaDaConta(t *testing.T) {
	_, credits, err := usage.ParseUsage([]byte(usageResponse), observedAt())
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if credits == nil {
		t.Fatal("saldo extra não foi lido")
	}
	if credits.Used != 83.89 {
		t.Errorf("usado = %v, queria 83.89", credits.Used)
	}
	if credits.Limit != 275 {
		t.Errorf("teto = %v, queria 275", credits.Limit)
	}
	if credits.Currency != "BRL" {
		t.Errorf("moeda = %q, queria BRL", credits.Currency)
	}
	if got := credits.Remaining(); got != 275-83.89 {
		t.Errorf("restante = %v, queria %v", got, 275-83.89)
	}
}

func TestParseUsageAceitaContaSemCreditos(t *testing.T) {
	_, credits, err := usage.ParseUsage([]byte(`{"five_hour":{"utilization":1}}`), observedAt())
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if credits != nil {
		t.Errorf("conta sem extra_usage não devia produzir saldo, veio %+v", credits)
	}
}

// --- Cliente -----------------------------------------------------------

type fixedSource struct {
	cred usage.Credential
	err  error
}

func (f fixedSource) Credential(context.Context) (usage.Credential, error) { return f.cred, f.err }

func quota(t *testing.T, handler http.HandlerFunc, cred usage.Credential) *usage.ClaudeQuota {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return &usage.ClaudeQuota{
		Source:   fixedSource{cred: cred},
		Client:   server.Client(),
		Endpoint: server.URL,
		Now:      observedAt,
	}
}

func validCredential() usage.Credential {
	return usage.Credential{AccessToken: "token-de-teste", ExpiresAt: observedAt().Add(time.Hour)}
}

func TestClaudeQuotaEnviaACredencialELeAResposta(t *testing.T) {
	var gotAuth, gotBeta string
	q := quota(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotBeta = r.Header.Get("Authorization"), r.Header.Get("anthropic-beta")
		w.Write([]byte(usageResponse))
	}, validCredential())

	windows, err := q.RateWindows(context.Background())
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(windows) != 2 {
		t.Fatalf("%d janelas, queria 2", len(windows))
	}
	if gotAuth != "Bearer token-de-teste" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotBeta == "" {
		t.Error("sem o cabeçalho de beta a rota recusa a credencial de OAuth")
	}
}

// Cotas e saldo vêm da mesma resposta: pedir duas vezes por relatório seria
// uma ida à rede jogada fora.
func TestClaudeQuotaReaproveitaARespostaEntreCotasESaldo(t *testing.T) {
	var calls int
	q := quota(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Write([]byte(usageResponse))
	}, validCredential())

	ctx := context.Background()
	if _, err := q.RateWindows(ctx); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if _, err := q.Credits(ctx); err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if calls != 1 {
		t.Errorf("%d chamadas à API, queria 1", calls)
	}
}

func TestClaudeQuotaReportaSessaoExpirada(t *testing.T) {
	expired := usage.Credential{AccessToken: "velho", ExpiresAt: observedAt().Add(-time.Minute)}
	q := quota(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("credencial vencida não devia chegar à rede")
	}, expired)

	if _, err := q.RateWindows(context.Background()); err != usage.ErrSessionExpired {
		t.Errorf("erro = %v, queria ErrSessionExpired", err)
	}
}

func TestClaudeQuotaTrataRecusaDaAPIComoSessaoExpirada(t *testing.T) {
	q := quota(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
	}, validCredential())

	if _, err := q.RateWindows(context.Background()); err != usage.ErrSessionExpired {
		t.Errorf("erro = %v, queria ErrSessionExpired", err)
	}
}

// O saldo não repete o erro que as cotas já reportaram: a tela mostraria a
// mesma frase duas vezes na barra de status.
func TestClaudeQuotaNaoDuplicaOErroNoSaldo(t *testing.T) {
	q := quota(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}, validCredential())

	credits, err := q.Credits(context.Background())
	if err != nil || credits != nil {
		t.Errorf("Credits = %v, %v; queria nil, nil", credits, err)
	}
}

func TestParseCredentialLeOFormatoDoChaveiro(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"claudeAiOauth": map[string]any{
			"accessToken":      "abc",
			"expiresAt":        observedAt().Add(time.Hour).UnixMilli(),
			"subscriptionType": "pro",
		},
	})

	cred, err := usage.ParseCredential(raw)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if cred.AccessToken != "abc" || cred.Plan != "pro" {
		t.Errorf("credencial = %+v", cred)
	}
	if cred.Expired(observedAt()) {
		t.Error("credencial válida marcada como vencida")
	}
	if !cred.Expired(observedAt().Add(2 * time.Hour)) {
		t.Error("credencial vencida não foi detectada")
	}
}

func TestParseCredentialRecusaPayloadSemToken(t *testing.T) {
	for _, raw := range []string{`{}`, `{"claudeAiOauth":{}}`, `não é json`} {
		if _, err := usage.ParseCredential([]byte(raw)); err != usage.ErrNoCredentials {
			t.Errorf("%q → %v, queria ErrNoCredentials", raw, err)
		}
	}
}
