package machine_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/mateuslh/lealing/sdk/machine"
	"github.com/mateuslh/lealing/sdk/protocol"
)

func TestFileSystemEscreveAtomicamenteNoDiretorioPrivado(t *testing.T) {
	data := filepath.Join(t.TempDir(), "tool-data")
	files := machine.NewEnvironment(protocol.Initialize{DataDir: data}).Files()
	name := filepath.Join(data, "nested", "state.json")

	if err := files.WriteFileAtomic(name, []byte("primeiro"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := files.WriteFileAtomic(name, []byte("segundo"), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, err := files.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "segundo" {
		t.Fatalf("conteúdo = %q", raw)
	}
	opened, err := files.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	_ = opened.Close()
	if info, err := files.Stat(name); err != nil || info.Size() != int64(len(raw)) {
		t.Fatalf("Stat = %v, %v", info, err)
	}
	entries, err := files.ReadDir(filepath.Dir(name))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "state.json" {
		t.Fatalf("temporário vazou: %v", entries)
	}
}

func TestFileSystemRecusaCaminhoForaDaConcessao(t *testing.T) {
	files := machine.NewEnvironment(protocol.Initialize{DataDir: t.TempDir()}).Files()
	err := files.WriteFileAtomic(filepath.Join(t.TempDir(), "fora"), []byte("x"), 0o600)
	if !errors.Is(err, machine.ErrPermissionDenied) {
		t.Fatalf("erro = %v", err)
	}
}
