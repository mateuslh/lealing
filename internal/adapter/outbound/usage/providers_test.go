package usage_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/mateuslh/lealing/internal/adapter/outbound/usage"
)

// sessionLog escreve um rollout do Codex com uma cota registrada.
func sessionLog(t *testing.T, usedPercent float64) string {
	t.Helper()
	dir := t.TempDir()
	line := `{"type":"event_msg","timestamp":"2026-07-29T01:29:51.000Z","payload":{` +
		`"type":"token_count","rate_limits":{"primary":{"used_percent":` +
		strconv.FormatFloat(usedPercent, 'f', -1, 64) +
		`,"window_minutes":10080,"resets_at":1786066832}}}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "rollout.jsonl"), []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Sem sessão na CLI não há falha a relatar — a tela cai no que o log guardou.
func TestCodexCaiNoLogQuandoNaoHaSessao(t *testing.T) {
	c := &usage.Codex{
		Root: sessionLog(t, 1),
		Quota: &usage.CodexQuota{
			Source: fixedCodexSource{err: usage.ErrNoCodexCredentials},
			Now:    observedAt,
		},
	}

	windows, err := c.RateWindows(context.Background())
	if err != nil {
		t.Fatalf("ausência de sessão não é erro: %v", err)
	}
	if len(windows) != 1 {
		t.Fatalf("%d janelas, queria a do log", len(windows))
	}
	if windows[0].Source != "log local" {
		t.Errorf("origem = %q, queria \"log local\"", windows[0].Source)
	}
	if windows[0].UsedPercent != 1 {
		t.Errorf("uso = %v, queria o 1%% gravado no log", windows[0].UsedPercent)
	}
}

// Com a conta fora do ar, o log ainda responde — mas o erro sobe junto, para
// a barra de status poder dizer por que o número está velho.
func TestCodexCaiNoLogQuandoAContaFalhaEPreservaOErro(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer server.Close()

	c := &usage.Codex{
		Root: sessionLog(t, 1),
		Quota: &usage.CodexQuota{
			Source: fixedCodexSource{cred: usage.CodexCredential{
				AccessToken: "t", ExpiresAt: observedAt().Add(time.Hour),
			}},
			Client:   server.Client(),
			Endpoint: server.URL,
			Now:      observedAt,
		},
	}

	windows, err := c.RateWindows(context.Background())
	if err == nil {
		t.Error("a falha da conta precisa chegar à tela")
	}
	if len(windows) != 1 {
		t.Fatalf("%d janelas, queria a do log como reserva", len(windows))
	}
}

// Quando a conta responde, ela ganha: o log tem a idade do último uso, e a
// janela pode ter virado desde então.
func TestCodexPrefereAContaAoLog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(codexResponse))
	}))
	defer server.Close()

	c := &usage.Codex{
		Root: sessionLog(t, 1),
		Quota: &usage.CodexQuota{
			Source: fixedCodexSource{cred: usage.CodexCredential{
				AccessToken: "t", ExpiresAt: observedAt().Add(time.Hour),
			}},
			Client:   server.Client(),
			Endpoint: server.URL,
			Now:      observedAt,
		},
	}

	windows, err := c.RateWindows(context.Background())
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(windows) != 2 {
		t.Fatalf("%d janelas, queria as duas da conta", len(windows))
	}
	if windows[0].UsedPercent != 42.5 {
		t.Errorf("uso = %v, queria o da conta e não o do log", windows[0].UsedPercent)
	}
}
