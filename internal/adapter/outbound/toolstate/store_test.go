package toolstate_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mateuslh/lealing/internal/adapter/outbound/toolstate"
	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/toolmanage"
)

func state(refs ...domain.ToolRef) toolmanage.State {
	return toolmanage.State{Disabled: refs}
}

func TestStoreAusenteEVaiEVoltaComFormatoIntegro(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "tool-state.json")
	store := toolstate.New(path)
	if got, err := store.Load(t.Context()); err != nil || len(got.Disabled) != 0 {
		t.Fatalf("Load ausente = %+v, %v", got, err)
	}
	want := state(
		domain.ToolRef{ID: "example-tool", Host: "provider-b"},
		domain.ToolRef{ID: "example-tool", Host: "provider-a"},
		domain.ToolRef{ID: "another-tool", Host: "provider-a"},
	)
	if err := store.Save(t.Context(), want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ordered := []domain.ToolRef{
		{ID: "another-tool", Host: "provider-a"},
		{ID: "example-tool", Host: "provider-a"},
		{ID: "example-tool", Host: "provider-b"},
	}
	if len(got.Disabled) != len(ordered) {
		t.Fatalf("Load = %+v", got)
	}
	for index := range ordered {
		if got.Disabled[index] != ordered[index] {
			t.Fatalf("Load[%d] = %+v, quero %+v", index, got.Disabled[index], ordered[index])
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		`"version": 3`, `"generation": 1`, `"id": "example-tool"`,
		`"host": "provider-a"`, `"checksum": "sha256:`,
	} {
		if !strings.Contains(string(raw), fragment) {
			t.Fatalf("arquivo não contém %q:\n%s", fragment, raw)
		}
	}
	for _, candidate := range []string{path, path + ".bak"} {
		info, err := os.Stat(candidate)
		if err != nil || info.Mode().Perm()&0o077 != 0 {
			t.Fatalf("permissões de %s = %v, err=%v", candidate, info.Mode(), err)
		}
	}
}

func TestChecksumDetectaAlteracaoSemBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool-state.json")
	store := toolstate.New(path)
	if err := store.Save(t.Context(), state(domain.ToolRef{ID: "demo", Host: "provider"})); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(string(raw), `"id": "demo"`, `"id": "outra"`, 1)
	if err := os.WriteFile(path, []byte(tampered), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path + ".bak"); err != nil {
		t.Fatal(err)
	}
	if _, err := toolstate.New(path).Load(t.Context()); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("Load adulterado = %v", err)
	}
}

func TestBackupRecuperaEstadoQuandoPrincipalCorrompe(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool-state.json")
	store := toolstate.New(path)
	first := state(domain.ToolRef{ID: "primeira", Host: "provider"})
	if err := store.Save(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	if err := store.Save(t.Context(), state(domain.ToolRef{ID: "segunda", Host: "provider"})); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"version":3`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := toolstate.New(path).Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Disabled) != 1 || got.Disabled[0].ID != "segunda" {
		t.Fatalf("backup = %+v", got)
	}
}

func TestPrincipalEBackupInvalidosExplicamAsDuasFalhas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool-state.json")
	for _, candidate := range []string{path, path + ".bak"} {
		if err := os.WriteFile(candidate, []byte(`{{{`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := toolstate.New(path).Load(t.Context()); err == nil ||
		!strings.Contains(err.Error(), "principal") || !strings.Contains(err.Error(), "backup") {
		t.Fatalf("Load = %v", err)
	}
}

func TestJSONHostilERecusado(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "identidade duplicada", raw: `{"version":3,"generation":1,"disabled":[{"id":"demo","host":"a"},{"id":"demo","host":"a"}],"checksum":"sha256:x"}`},
		{name: "host vazio", raw: `{"version":3,"generation":1,"disabled":[{"id":"demo","host":""}],"checksum":"sha256:x"}`},
		{name: "campo desconhecido", raw: `{"version":3,"generation":1,"checksum":"sha256:x","extra":true}`},
		{name: "chave superior duplicada", raw: `{"version":3,"version":3,"generation":1,"checksum":"sha256:x"}`},
		{name: "chave interna duplicada", raw: `{"version":3,"generation":1,"disabled":[{"id":"demo","id":"outra","host":"a"}],"checksum":"sha256:x"}`},
		{name: "dados adicionais", raw: `{"version":3,"generation":1,"checksum":"sha256:x"} {}`},
		{name: "versão antiga", raw: `{"version":2,"disabled":[]}`},
		{name: "versão futura", raw: `{"version":999,"disabled":[]}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "tool-state.json")
			if err := os.WriteFile(path, []byte(test.raw), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := toolstate.New(path).Load(t.Context()); err == nil {
				t.Fatal("conteúdo hostil foi aceito")
			}
		})
	}
}

func TestSaveRecusaEstadoForaDosLimites(t *testing.T) {
	longID := strings.Repeat("a", 257)
	tests := []struct {
		name  string
		state toolmanage.State
	}{
		{name: "duplicado", state: state(
			domain.ToolRef{ID: "demo", Host: "provider"},
			domain.ToolRef{ID: "demo", Host: "provider"},
		)},
		{name: "espaço", state: state(domain.ToolRef{ID: " demo", Host: "provider"})},
		{name: "controle", state: state(domain.ToolRef{ID: "demo", Host: "provider\tforjado"})},
		{name: "longo", state: state(domain.ToolRef{ID: domain.ToolID(longID), Host: "provider"})},
	}
	tooMany := make([]domain.ToolRef, 4097)
	for index := range tooMany {
		tooMany[index] = domain.ToolRef{ID: domain.ToolID(fmt.Sprintf("tool-%d", index)), Host: "provider"}
	}
	tests = append(tests, struct {
		name  string
		state toolmanage.State
	}{name: "quantidade", state: state(tooMany...)})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "tool-state.json")
			if err := toolstate.New(path).Save(t.Context(), test.state); err == nil {
				t.Fatal("estado inválido foi gravado")
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("arquivo apareceu após recusa: %v", err)
			}
		})
	}
}

func TestArquivoGrandeOuInseguroERecusado(t *testing.T) {
	t.Run("limite", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "tool-state.json")
		if err := os.WriteFile(path, make([]byte, (1<<20)+1), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := toolstate.New(path).Load(t.Context()); err == nil || !strings.Contains(err.Error(), "excede") {
			t.Fatalf("Load grande = %v", err)
		}
	})

	t.Run("symlink", func(t *testing.T) {
		directory := t.TempDir()
		target := filepath.Join(directory, "target.json")
		path := filepath.Join(directory, "tool-state.json")
		if err := os.WriteFile(target, []byte(`{"version":3}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, path); err != nil {
			t.Skipf("symlink indisponível: %v", err)
		}
		if _, err := toolstate.New(path).Load(t.Context()); err == nil || !strings.Contains(err.Error(), "regular") {
			t.Fatalf("Load symlink = %v", err)
		}
	})
}

func TestEscritaConcorrenteDetectaEstadoObsoleto(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool-state.json")
	first, second := toolstate.New(path), toolstate.New(path)
	if _, err := first.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := first.Save(t.Context(), state(domain.ToolRef{ID: "primeira", Host: "provider"})); err != nil {
		t.Fatal(err)
	}
	if err := second.Save(t.Context(), state(domain.ToolRef{ID: "segunda", Host: "provider"})); !errors.Is(err, toolstate.ErrConflict) {
		t.Fatalf("Save obsoleto = %v", err)
	}
}

func TestLockRespeitaCancelamentoESeRecuperaQuandoObsoleto(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool-state.json")
	lock := path + ".lock"
	if err := os.WriteFile(lock, []byte("ocupado\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if err := toolstate.New(path).Save(ctx, state(domain.ToolRef{ID: "demo", Host: "provider"})); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Save bloqueado = %v", err)
	}

	old := time.Now().Add(-2 * time.Minute)
	if err := os.Chtimes(lock, old, old); err != nil {
		t.Fatal(err)
	}
	if err := toolstate.New(path).Save(t.Context(), state(domain.ToolRef{ID: "demo", Host: "provider"})); err != nil {
		t.Fatalf("lock obsoleto não foi recuperado: %v", err)
	}
}

func TestContextoCanceladoNaoTocaODisco(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool-state.json")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := toolstate.New(path).Save(ctx, state(domain.ToolRef{ID: "demo", Host: "provider"})); !errors.Is(err, context.Canceled) {
		t.Fatalf("Save cancelado = %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("arquivo apareceu após cancelamento: %v", err)
	}
}

func TestMesmaInstanciaSerializaEscritas(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tool-state.json")
	store := toolstate.New(path)
	var wait sync.WaitGroup
	errorsFound := make(chan error, 8)
	for index := 0; index < 8; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			errorsFound <- store.Save(t.Context(), state(domain.ToolRef{ID: "demo", Host: "provider"}))
		}()
	}
	wait.Wait()
	close(errorsFound)
	for err := range errorsFound {
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
}
