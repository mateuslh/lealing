//go:build darwin

package usage_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mateuslh/lealing/cmd/tools/token-usage/internal/tokens"
	"github.com/mateuslh/lealing/cmd/tools/token-usage/internal/usage"
)

// TestSmokeTokens pertence à tool: a engine não conhece mais o domínio nem
// os adapters que leem os logs das CLIs.
func TestSmokeTokens(t *testing.T) {
	if testing.Short() {
		t.Skip("depende dos arquivos da máquina local")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("sem diretório do usuário: %v", err)
	}
	service := tokens.NewService(nil,
		usage.NewClaudeCode(
			filepath.Join(home, ".claude", "projects"),
			usage.NewLocalCredentials(filepath.Join(home, ".claude", ".credentials.json"), true),
		),
		usage.NewCodex(
			filepath.Join(home, ".codex", "sessions"),
			usage.NewCodexFile(filepath.Join(home, ".codex", "auth.json")),
		),
	)
	start := time.Now()
	report, err := service.Generate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("varredura em %v | mensagens=%d custo=$%.2f tokens=%d",
		time.Since(start), report.Overall.Messages, report.Overall.Cost, report.Overall.TotalTokens())
}
