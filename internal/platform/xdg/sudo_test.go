package xdg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMissingAncestorsListaSóOQueFalta(t *testing.T) {
	root := t.TempDir()
	existente := filepath.Join(root, "share")
	if err := os.Mkdir(existente, 0o755); err != nil {
		t.Fatal(err)
	}

	alvo := filepath.Join(existente, "lealing", "sub")
	got := missingAncestors(alvo)

	want := []string{alvo, filepath.Join(existente, "lealing")}
	if len(got) != len(want) {
		t.Fatalf("faltantes = %v, quero %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("faltantes[%d] = %q, quero %q", i, got[i], want[i])
		}
	}
}

func TestMkdirAllCriaAÁrvore(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b", "c")
	if err := MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		t.Fatalf("Stat(%q) = %v, %v", dir, info, err)
	}
	// Idempotente: o segundo passe não tem nada a adotar e não pode falhar.
	if err := MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll repetido: %v", err)
	}
}

func TestAdoptÉNoOpForaDoSudo(t *testing.T) {
	// Sem euid 0 não há adoção possível, e o caminho nem chega a ser tocado —
	// é o caminho de todo dia, em que um erro de chown seria ruído puro.
	t.Setenv("SUDO_UID", "501")
	t.Setenv("SUDO_GID", "20")

	if os.Geteuid() == 0 {
		t.Skip("teste descreve o processo sem privilégio")
	}
	if _, _, ok := realUser(); ok {
		t.Fatal("realUser devolveu ok sem euid 0")
	}
	if err := Adopt(filepath.Join(t.TempDir(), "inexistente")); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
}
