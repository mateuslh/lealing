package usage_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/mateuslh/lealing/cmd/tools/token-usage/internal/usage"
)

// codexResponse é uma resposta real da conta, com os campos que a tela não
// consome preservados.
const codexResponse = `{
  "plan_type": "plus",
  "rate_limit": {
    "allowed": true,
    "limit_reached": false,
    "primary_window": {"used_percent": 42.5, "limit_window_seconds": 604800,
                       "reset_after_seconds": 302400, "reset_at": 1786066832},
    "secondary_window": {"used_percent": 7, "limit_window_seconds": 18000,
                         "reset_after_seconds": 9000, "reset_at": 1785650000}
  },
  "code_review_rate_limit": null,
  "credits": {"balance": "12.50", "has_credits": true, "unlimited": false,
              "overage_limit_reached": false},
  "rate_limit_reset_credits": {"available_count": 2},
  "spend_control": {"reached": false}
}`

func TestParseCodexUsageLeAsJanelasDaConta(t *testing.T) {
	windows, _, err := usage.ParseCodexUsage([]byte(codexResponse), observedAt())
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(windows) != 2 {
		t.Fatalf("%d janelas, queria 2", len(windows))
	}

	week := windows[0]
	if week.WindowMinutes != 7*24*60 {
		t.Errorf("janela = %dmin, queria a semana", week.WindowMinutes)
	}
	if week.UsedPercent != 42.5 {
		t.Errorf("uso = %v, queria 42.5", week.UsedPercent)
	}
	if got := week.ResetsAt.Unix(); got != 1786066832 {
		t.Errorf("renovação = %d, queria o reset_at da resposta", got)
	}
	// O plano entra na origem: é ele que explica o tamanho do limite.
	if week.Source != "conta · plus" {
		t.Errorf("origem = %q, queria \"conta · plus\"", week.Source)
	}

	if session := windows[1]; session.WindowMinutes != 300 || session.UsedPercent != 7 {
		t.Errorf("segunda janela = %dmin/%v%%, queria a sessão de 5h em 7%%",
			session.WindowMinutes, session.UsedPercent)
	}
}

// Sem reset_at absoluto, o prazo relativo ainda dá o horário de renovação.
func TestParseCodexUsageAceitaPrazoRelativo(t *testing.T) {
	raw := `{"rate_limit":{"primary_window":{"used_percent":10,
	        "limit_window_seconds":18000,"reset_after_seconds":3600}}}`
	windows, _, err := usage.ParseCodexUsage([]byte(raw), observedAt())
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(windows) != 1 {
		t.Fatalf("%d janelas, queria 1", len(windows))
	}
	if want := observedAt().Add(time.Hour); !windows[0].ResetsAt.Equal(want) {
		t.Errorf("renovação = %v, queria %v", windows[0].ResetsAt, want)
	}
}

// Uma janela nula ou sem duração não vira linha: sem prazo não há ritmo nem
// countdown, e uma barra solta sugeriria um limite que não foi publicado.
func TestParseCodexUsageIgnoraJanelaSemDuracao(t *testing.T) {
	raw := `{"rate_limit":{"primary_window":{"used_percent":10,"limit_window_seconds":0},
	        "secondary_window":null}}`
	windows, _, err := usage.ParseCodexUsage([]byte(raw), observedAt())
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(windows) != 0 {
		t.Errorf("%d janelas, queria nenhuma", len(windows))
	}
}

func TestParseCodexUsageLeOSaldoComoCarteira(t *testing.T) {
	_, credits, err := usage.ParseCodexUsage([]byte(codexResponse), observedAt())
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if credits == nil {
		t.Fatal("saldo não foi lido")
	}
	if credits.Balance != 12.5 {
		t.Errorf("saldo = %v, queria 12.5", credits.Balance)
	}
	if credits.Metered() {
		t.Error("carteira sem teto não deve ser tratada como cota medida")
	}
	if got := credits.Remaining(); got != 12.5 {
		t.Errorf("restante = %v, queria o próprio saldo", got)
	}
}

// Conta sem crédito contratado não vira linha na tela: saldo zero sem
// contratação e saldo zero por consumo são coisas diferentes.
func TestParseCodexUsageOmiteCarteiraNaoContratada(t *testing.T) {
	raw := `{"credits":{"balance":"0","has_credits":false,"unlimited":false}}`
	_, credits, err := usage.ParseCodexUsage([]byte(raw), observedAt())
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if credits != nil {
		t.Errorf("saldo = %+v, queria nenhum", credits)
	}
}

func TestParseCodexUsageRotulaACotaDeCodeReview(t *testing.T) {
	raw := `{"code_review_rate_limit":{"primary_window":
	        {"used_percent":5,"limit_window_seconds":604800,"reset_at":1786066832}}}`
	windows, _, err := usage.ParseCodexUsage([]byte(raw), observedAt())
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(windows) != 1 {
		t.Fatalf("%d janelas, queria 1", len(windows))
	}
	if windows[0].Label != "Semana · review" {
		t.Errorf("rótulo = %q, queria distinguir a cota de review", windows[0].Label)
	}
}

// --- Credencial e cliente ----------------------------------------------

// jwt monta um token com o `exp` pedido. A assinatura é irrelevante: quem a
// valida é o servidor; o campo serve só para evitar uma chamada perdida.
func jwt(exp time.Time) string {
	body, _ := json.Marshal(map[string]int64{"exp": exp.Unix()})
	return "cabecalho." + base64.RawURLEncoding.EncodeToString(body) + ".assinatura"
}

func codexAuthFile(exp time.Time) []byte {
	raw, _ := json.Marshal(map[string]any{
		"auth_mode": "chatgpt",
		"tokens": map[string]string{
			"access_token": jwt(exp),
			"account_id":   "conta-123",
		},
	})
	return raw
}

func TestParseCodexCredentialLeOPrazoDeDentroDoToken(t *testing.T) {
	cred, err := usage.ParseCodexCredential(codexAuthFile(observedAt().Add(time.Hour)))
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if cred.AccountID != "conta-123" {
		t.Errorf("conta = %q", cred.AccountID)
	}
	if cred.Expired(observedAt()) {
		t.Error("credencial válida marcada como vencida")
	}
	if !cred.Expired(observedAt().Add(2 * time.Hour)) {
		t.Error("credencial vencida não foi detectada")
	}
}

// Token que não é JWT não impede a consulta: sem prazo conhecido, a chamada
// acontece e o 401 decide.
func TestParseCodexCredentialSemPrazoAindaVale(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{
		"tokens": map[string]string{"access_token": "opaco"},
	})
	cred, err := usage.ParseCodexCredential(raw)
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if cred.Expired(observedAt()) {
		t.Error("sem exp legível a credencial não pode ser dada como vencida")
	}
}

func TestParseCodexCredentialRecusaArquivoSemToken(t *testing.T) {
	for _, raw := range []string{`{}`, `{"tokens":{}}`, `nada disso`} {
		if _, err := usage.ParseCodexCredential([]byte(raw)); err != usage.ErrNoCodexCredentials {
			t.Errorf("%q → %v, queria ErrNoCodexCredentials", raw, err)
		}
	}
}

type fixedCodexSource struct {
	cred usage.CodexCredential
	err  error
}

func (f fixedCodexSource) CodexCredential(context.Context) (usage.CodexCredential, error) {
	return f.cred, f.err
}

// A borda da OpenAI responde com uma página de bloqueio quando não há
// User-Agent, então mandá-lo não é enfeite.
func TestCodexQuotaSeIdentificaNaChamada(t *testing.T) {
	var gotAgent, gotAccount string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAgent, gotAccount = r.Header.Get("User-Agent"), r.Header.Get("chatgpt-account-id")
		w.Write([]byte(codexResponse))
	}))
	defer server.Close()

	q := &usage.CodexQuota{
		Source: fixedCodexSource{cred: usage.CodexCredential{
			AccessToken: "t", AccountID: "conta-123", ExpiresAt: observedAt().Add(time.Hour),
		}},
		Client:   server.Client(),
		Endpoint: server.URL,
		Now:      observedAt,
	}

	windows, err := q.RateWindows(context.Background())
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(windows) != 2 {
		t.Errorf("%d janelas, queria 2", len(windows))
	}
	if gotAgent == "" {
		t.Error("sem User-Agent a borda devolve página de bloqueio em HTML")
	}
	if gotAccount != "conta-123" {
		t.Errorf("conta = %q; sem ela a resposta é a da conta padrão", gotAccount)
	}
}

func TestCodexQuotaReportaSessaoExpirada(t *testing.T) {
	q := &usage.CodexQuota{
		Source: fixedCodexSource{cred: usage.CodexCredential{
			AccessToken: "t", ExpiresAt: observedAt().Add(-time.Minute),
		}},
		Now: observedAt,
	}
	if _, err := q.RateWindows(context.Background()); err != usage.ErrCodexSessionExpired {
		t.Errorf("erro = %v, queria ErrCodexSessionExpired", err)
	}
}
