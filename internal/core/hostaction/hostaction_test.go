package hostaction_test

import (
	"context"
	"strings"
	"testing"

	"github.com/mateuslh/lealing/internal/core/hostaction"
)

type executor struct {
	clipboard string
	browser   string
}

func (e *executor) WriteClipboard(_ context.Context, text string) error {
	e.clipboard = text
	return nil
}
func (e *executor) OpenBrowser(_ context.Context, target string) error {
	e.browser = target
	return nil
}

func TestAplicaPoliticaAntesDoHost(t *testing.T) {
	executor := &executor{}
	service := hostaction.NewService(executor)
	if err := service.WriteClipboard(context.Background(), "texto"); err != nil || executor.clipboard != "texto" {
		t.Fatalf("clipboard: %q, %v", executor.clipboard, err)
	}
	if err := service.OpenBrowser(context.Background(), "https://example.test/path"); err != nil || executor.browser == "" {
		t.Fatalf("browser: %q, %v", executor.browser, err)
	}
	for _, target := range []string{"file:///tmp/segredo", "javascript:alert(1)", "https://example.test/\nmal"} {
		if err := service.OpenBrowser(context.Background(), target); err == nil {
			t.Errorf("URL insegura aceita: %q", target)
		}
	}
	if err := service.WriteClipboard(context.Background(), strings.Repeat("x", (1<<20)+1)); err == nil {
		t.Fatal("clipboard acima do limite foi aceito")
	}
}
