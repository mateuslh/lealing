package settingsstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestArquivoAusenteValeComoNadaAlterado(t *testing.T) {
	values, err := New(filepath.Join(t.TempDir(), "settings.json")).Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 0 {
		t.Fatalf("valores = %+v", values)
	}
}

func TestGravaELeDeVolta(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "settings.json")
	store := New(path)

	want := map[string]string{"home.greeting_name": "Chefia", "marketplace.check_on_home": "false"}
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got["home.greeting_name"] != "Chefia" {
		t.Fatalf("valores = %+v", got)
	}

	// O arquivo é configuração de gente: precisa ser legível e editável.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "\n  ") {
		t.Fatalf("o arquivo saiu sem indentação: %s", raw)
	}
}

func TestFormatoMaisNovoERecusado(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"version":999,"values":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(path).Load(); err == nil || !strings.Contains(err.Error(), "versão mais nova") {
		t.Fatalf("Load = %v", err)
	}
}

// JSON quebrado devolve erro e um mapa utilizável: a engine sobe nos padrões
// e a tela mostra a falha.
func TestJSONInvalidoDevolveErroEMapaVazio(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{{{`), 0o600); err != nil {
		t.Fatal(err)
	}
	values, err := New(path).Load()
	if err == nil {
		t.Fatal("JSON inválido foi aceito")
	}
	if values == nil {
		t.Fatal("Load devolveu mapa nil; quem chamou teria de checar antes de usar")
	}
}
