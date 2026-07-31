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
	present, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	presentName := filepath.Base(present)
	// O próprio binário de teste tem o formato executável correto da
	// plataforma (.exe no Windows e permissão de execução em sistemas Unix).
	t.Setenv("PATH", filepath.Dir(present))

	got := requirements.NewPathChecker().Missing(context.Background(), []domain.Requirement{
		{Executable: presentName},
		{Executable: "ausente", Name: "Ferramenta ausente"},
	})
	if len(got) != 1 || got[0].Executable != "ausente" {
		t.Fatalf("ausentes = %#v", got)
	}
}
