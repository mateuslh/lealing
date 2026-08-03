package persistence

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/mateuslh/lealing/internal/core/domain"
)

func TestUsageFileGravaERecarrega(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dados", "usage.json")
	store := NewUsageFile(path, 0) // debounce zero: grava síncrono

	want := domain.Usage{Host: "example-host", ToolID: "example-tool", Runs: 3, Favorite: true}
	if err := store.Save(context.Background(), want); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := NewUsageFile(path, 0).Load(context.Background())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if u := got["example-tool"]; u.Host != want.Host || u.Runs != want.Runs || !u.Favorite {
		t.Errorf("recarregado = %+v, quero runs=%d favorito", u, want.Runs)
	}
}

// Um arquivo ilegível é o rastro de um `sudo lealing`: ele fica com dono root
// e a execução seguinte, sem sudo, não consegue abrir. Estado de uso é cache,
// então isso não pode impedir a TUI de subir.
func TestLoadIgnoraArquivoSemPermissão(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignora as permissões do arquivo")
	}

	path := filepath.Join(t.TempDir(), "usage.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"usage":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}

	got, err := NewUsageFile(path, 0).Load(context.Background())
	if err != nil {
		t.Fatalf("Load devolveu erro em vez de degradar: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("Load = %v, quero vazio", got)
	}
}
