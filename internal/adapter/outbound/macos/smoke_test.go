//go:build darwin

// Estes testes leem a máquina de verdade — sysctl, pmset e os logs das CLIs
// de IA. Não afirmam valores (que variam por máquina); servem para conferir
// que os adapters conversam com o sistema e para inspecionar a saída com
// `go test -v -run Smoke`. São pulados em `-short`, que é como a CI roda.
package macos_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mateuslh/lealing/internal/adapter/outbound/macos"
	"github.com/mateuslh/lealing/internal/adapter/outbound/usage"
	"github.com/mateuslh/lealing/internal/core/tokens"
)

func TestSmokeSysInfo(t *testing.T) {
	skipShort(t)
	s, err := macos.NewSystemInspector(nil).Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, sec := range s.Sections() {
		t.Logf("[%s]", sec.Title)
		for _, f := range sec.Fields {
			t.Logf("  %-22s %s", f.Label, f.Value)
		}
	}
}

func TestSmokePower(t *testing.T) {
	skipShort(t)
	s, err := macos.NewPowerManager().Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("bateria: %+v", s.Battery)
	t.Logf("resumo:  %s", s.Battery.Summary())
	t.Logf("ac:      %+v", s.AC)
	t.Logf("sem senha: %v", macos.NewPowerManager().PasswordlessEnabled(context.Background()))
}

func TestSmokeTokens(t *testing.T) {
	skipShort(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("sem diretório do usuário: %v", err)
	}
	svc := tokens.NewService(nil,
		usage.NewClaudeCode(
			filepath.Join(home, ".claude", "projects"),
			usage.NewLocalCredentials(
				filepath.Join(home, ".claude", ".credentials.json"),
				true,
			),
		),
		usage.NewCodex(
			filepath.Join(home, ".codex", "sessions"),
			usage.NewCodexFile(filepath.Join(home, ".codex", "auth.json")),
		),
	)
	start := time.Now()
	r, err := svc.Generate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("varredura em %v | mensagens=%d custo=$%.2f tokens=%d",
		time.Since(start), r.Overall.Messages, r.Overall.Cost, r.Overall.TotalTokens())
	for _, p := range r.ByProvider {
		t.Logf("  provedor %-14s $%8.2f  %d msgs", p.Label, p.Totals.Cost, p.Totals.Messages)
	}
	for _, m := range r.ByModel {
		t.Logf("  modelo   %-24s $%8.2f", m.Label, m.Totals.Cost)
	}
	for _, w := range r.Windows {
		t.Logf("  janela   %-12s $%8.2f", w.Label, w.Totals.Cost)
	}
	for _, w := range r.RateWindows {
		t.Logf("  cota     %s %s: %.1f%% usado", w.Provider, w.Label, w.UsedPercent)
	}
	t.Logf("  dias com dado: %d | erros: %v", len(r.ByDay), r.Errs)
}

// skipShort pula testes que dependem da máquina local.
func skipShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("depende do sistema local")
	}
}
