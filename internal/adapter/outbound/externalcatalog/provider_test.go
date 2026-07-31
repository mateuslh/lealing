package externalcatalog_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mateuslh/lealing/internal/adapter/outbound/externalcatalog"
	"github.com/mateuslh/lealing/internal/adapter/outbound/registry"
	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/port/outbound"
	"github.com/mateuslh/lealing/internal/toolmanifest"
)

const manifestTemplate = `apiVersion: lealing.dev/v1
id: ID
version: VERSION
name: Tool externa
summary: Desenha uma tela externa.
detail: Teste.
category: ai
risk: safe
runtime:
  kind: process
  protocol: {min: 1, max: 1}
  executable: tool-bin
ui:
  mode: screen-v1
platforms: [darwin-arm64]
requirements: []
permissions:
  filesystem: {read: [], write: []}
  network: false
  subprocess: false
`

func installManifest(t testing.TB, root, id, version, body string) string {
	t.Helper()
	dir := filepath.Join(root, id, version)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body = strings.ReplaceAll(body, "ID", id)
	body = strings.ReplaceAll(body, "VERSION", version)
	if err := os.WriteFile(filepath.Join(dir, "manifest.yaml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, id, "active"), []byte(version+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func provider(root string, strict bool, reserved ...domain.ToolID) *externalcatalog.Provider {
	return externalcatalog.New(externalcatalog.Options{
		Root: root, Categories: []domain.Category{{ID: "ai", Name: "IA"}},
		Reserved: reserved, Target: toolmanifest.Target{OS: "darwin", Arch: "arm64"},
		Strict: strict,
	})
}

func TestDescobertaLeManifestSemExigirNemExecutarBinario(t *testing.T) {
	root := t.TempDir()
	dir := installManifest(t, root, "token-usage", "1.0.0", manifestTemplate)
	// A ausência proposital do executável demonstra que descoberta não faz
	// stat nem spawn; isso só será erro quando a tela for aberta.
	if _, err := os.Stat(filepath.Join(dir, "tool-bin")); !os.IsNotExist(err) {
		t.Fatal("fixture criou executável sem querer")
	}

	tools, _, err := provider(root, true).Provide(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].ID != "token-usage" || tools[0].Runtime == nil {
		t.Fatalf("tools = %+v", tools)
	}
}

func TestManifestCorrompidoEIgnoradoForaDoStrict(t *testing.T) {
	root := t.TempDir()
	installManifest(t, root, "quebrada", "1.0.0", "isto não é um manifest")
	installManifest(t, root, "saudavel", "1.0.0", manifestTemplate)

	tools, _, err := provider(root, false).Provide(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 1 || tools[0].ID != "saudavel" {
		t.Fatalf("tools = %+v", tools)
	}
	if _, _, err := provider(root, true).Provide(context.Background()); err == nil {
		t.Fatal("strict aceitou instalação corrompida")
	}
}

func TestToolDePlataformaIncompativelSome(t *testing.T) {
	root := t.TempDir()
	body := strings.Replace(manifestTemplate, "darwin-arm64", "windows-amd64", 1)
	installManifest(t, root, "windows-only", "1.0.0", body)
	tools, _, err := provider(root, true).Provide(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 0 {
		t.Fatalf("tool incompatível apareceu: %+v", tools)
	}
}

func TestToolExternaNaoSobrescreveBuiltinReservada(t *testing.T) {
	root := t.TempDir()
	installManifest(t, root, "system-info", "1.0.0", manifestTemplate)
	tools, _, err := provider(root, false, "system-info").Provide(context.Background())
	if err != nil || len(tools) != 0 {
		t.Fatalf("tools=%+v err=%v", tools, err)
	}
	if _, _, err := provider(root, true, "system-info").Provide(context.Background()); err == nil {
		t.Fatal("strict não reportou tentativa de sobrescrever builtin")
	}
}

func TestProviderExternoIntegraAoRegistryEDetectaIDDuplicado(t *testing.T) {
	rootA, rootB := t.TempDir(), t.TempDir()
	installManifest(t, rootA, "duplicada", "1.0.0", manifestTemplate)
	installManifest(t, rootB, "duplicada", "1.0.0", manifestTemplate)
	providers := []outbound.ToolProvider{
		&registry.Static{Label: "categorias", Categories: []domain.Category{{ID: "ai", Name: "IA"}}},
		provider(rootA, true), provider(rootB, true),
	}
	if _, err := registry.New(providers, registry.WithStrict(true)).All(context.Background()); err == nil {
		t.Fatal("registry aceitou ID externo duplicado")
	}
}

func TestReloadDoRegistryTornaInstalacaoVisivelNaMesmaEngine(t *testing.T) {
	root := t.TempDir()
	external := provider(root, true)
	repository := registry.New([]outbound.ToolProvider{
		&registry.Static{Label: "categorias", Categories: []domain.Category{{ID: "ai", Name: "IA"}}},
		external,
	}, registry.WithStrict(true))

	tools, err := repository.All(context.Background())
	if err != nil || len(tools) != 0 {
		t.Fatalf("catálogo inicial = %+v, %v", tools, err)
	}
	installManifest(t, root, "nova-tool", "1.0.0", manifestTemplate)
	if _, err := repository.ByID(context.Background(), "nova-tool"); err == nil {
		t.Fatal("cache mudou sem uma recarga explícita")
	}
	if err := repository.Reload(context.Background()); err != nil {
		t.Fatal(err)
	}
	tool, err := repository.ByID(context.Background(), "nova-tool")
	if err != nil || tool.ID != "nova-tool" {
		t.Fatalf("tool após reload = %+v, %v", tool, err)
	}
}

func BenchmarkRegistrySemSpawn(b *testing.B) {
	root := b.TempDir()
	for i := 0; i < 100; i++ {
		id := "tool-" + strconv.Itoa(i)
		installManifest(b, root, id, "1.0.0", manifestTemplate)
	}
	b.ResetTimer()
	for b.Loop() {
		p := provider(root, true)
		if _, _, err := p.Provide(context.Background()); err != nil {
			b.Fatal(err)
		}
	}
}
