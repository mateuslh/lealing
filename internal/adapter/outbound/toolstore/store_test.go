package toolstore_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mateuslh/lealing/internal/adapter/outbound/toolstore"
	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/toolinstall"
	"github.com/mateuslh/lealing/internal/toolmanifest"
)

func source(t *testing.T, version, executableBody string) string {
	t.Helper()
	dir := t.TempDir()
	manifest := strings.ReplaceAll(`apiVersion: lealing.dev/v1
id: demo
version: VERSION
name: Demo
summary: Tool local de demonstração.
detail: Teste.
category: ai
risk: safe
runtime:
  kind: process
  protocol: {min: 1, max: 1}
  executable: demo-tool
ui: {mode: screen-v1}
platforms: [darwin-arm64]
requirements: []
permissions:
  filesystem: {read: [], write: []}
  network: false
  subprocess: false
`, "VERSION", version)
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(manifest), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "demo-tool"), []byte(executableBody), 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func store(root string) *toolstore.Store {
	return toolstore.New(root, []domain.Category{{ID: "ai", Name: "IA"}}, toolmanifest.Target{OS: "darwin", Arch: "arm64"}, nil)
}

func TestInstallVerificaChecksumETrocaVersaoAtiva(t *testing.T) {
	root := t.TempDir()
	first := source(t, "1.0.0", "primeiro")
	sum := sha256.Sum256([]byte("primeiro"))
	installed, err := store(root).Install(context.Background(), toolinstall.InstallRequest{
		SourceDir: first, ExpectedSHA256: hex.EncodeToString(sum[:]),
	})
	if err != nil {
		t.Fatal(err)
	}
	if installed.Version != "1.0.0" || installed.PreviousVersion != "" {
		t.Fatalf("instalação = %+v", installed)
	}

	second := source(t, "1.1.0", "segundo")
	updated, err := store(root).Install(context.Background(), toolinstall.InstallRequest{SourceDir: second})
	if err != nil {
		t.Fatal(err)
	}
	if updated.PreviousVersion != "1.0.0" {
		t.Fatalf("atualização = %+v", updated)
	}
	active, _ := os.ReadFile(filepath.Join(root, "demo", "active"))
	if strings.TrimSpace(string(active)) != "1.1.0" {
		t.Errorf("active = %q", active)
	}
}

func TestChecksumErradoNaoSubstituiInstalacaoSaudavel(t *testing.T) {
	root := t.TempDir()
	if _, err := store(root).Install(context.Background(), toolinstall.InstallRequest{SourceDir: source(t, "1.0.0", "ok")}); err != nil {
		t.Fatal(err)
	}
	_, err := store(root).Install(context.Background(), toolinstall.InstallRequest{
		SourceDir: source(t, "1.1.0", "novo"), ExpectedSHA256: strings.Repeat("0", 64),
	})
	if err == nil {
		t.Fatal("checksum errado foi aceito")
	}
	active, _ := os.ReadFile(filepath.Join(root, "demo", "active"))
	if strings.TrimSpace(string(active)) != "1.0.0" {
		t.Errorf("instalação saudável foi alterada: %q", active)
	}
}

func TestManifestNovoInvalidoNaoSubstituiInstalacaoSaudavel(t *testing.T) {
	root := t.TempDir()
	s := store(root)
	if _, err := s.Install(context.Background(), toolinstall.InstallRequest{SourceDir: source(t, "1.0.0", "ok")}); err != nil {
		t.Fatal(err)
	}
	broken := source(t, "1.1.0", "novo")
	if err := os.WriteFile(filepath.Join(broken, "manifest.yaml"), []byte("manifest quebrado"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Install(context.Background(), toolinstall.InstallRequest{SourceDir: broken}); err == nil {
		t.Fatal("manifest inválido foi instalado")
	}
	active, _ := os.ReadFile(filepath.Join(root, "demo", "active"))
	if strings.TrimSpace(string(active)) != "1.0.0" {
		t.Errorf("instalação saudável foi alterada: %q", active)
	}
}

func TestRollbackTrocaAtivaEAnterior(t *testing.T) {
	root := t.TempDir()
	s := store(root)
	for _, version := range []string{"1.0.0", "1.1.0"} {
		if _, err := s.Install(context.Background(), toolinstall.InstallRequest{SourceDir: source(t, version, version)}); err != nil {
			t.Fatal(err)
		}
	}
	result, err := s.Rollback(context.Background(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if result.Version != "1.0.0" || result.PreviousVersion != "1.1.0" {
		t.Fatalf("rollback = %+v", result)
	}
}

func TestRemoveMoveParaDiretorioRecuperavel(t *testing.T) {
	root := t.TempDir()
	s := store(root)
	if _, err := s.Install(context.Background(), toolinstall.InstallRequest{SourceDir: source(t, "1.0.0", "ok")}); err != nil {
		t.Fatal(err)
	}
	removed, err := s.Remove(context.Background(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(removed.RecoveryDir); err != nil {
		t.Fatalf("remoção não é recuperável: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "demo")); !os.IsNotExist(err) {
		t.Fatal("tool continuou ativa")
	}
}
