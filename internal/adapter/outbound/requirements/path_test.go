package requirements_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mateuslh/lealing/internal/adapter/outbound/requirements"
	"github.com/mateuslh/lealing/internal/core/domain"
)

func TestPathCheckerDevolveSomenteAusentes(t *testing.T) {
	dir := t.TempDir()
	present := filepath.Join(dir, "presente")
	if err := os.WriteFile(present, []byte{}, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	got := requirements.NewPathChecker().Missing(context.Background(), []domain.Requirement{
		{Executable: "presente"},
		{Executable: "ausente", Name: "Ferramenta ausente"},
	})
	if len(got) != 1 || got[0].Executable != "ausente" {
		t.Fatalf("ausentes = %#v", got)
	}
}
