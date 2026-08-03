package toolmanifest_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/toolmanifest"
)

func validManifest(id string) []byte {
	return []byte(fmt.Sprintf(`apiVersion: lealing.dev/v1
id: %s
version: 1.2.3
name: Tool de teste
summary: Faz uma leitura segura.
detail: Detalhe.
category: ai
risk: safe
runtime:
  kind: process
  protocol:
    min: 1
    max: 1
  executable: lealing-tool-%s
ui:
  mode: screen-v1
  capabilities:
    - navigation.back
platforms:
  - darwin-arm64
  - windows-amd64
requirements: []
permissions:
  filesystem:
    read:
      - ~/.codex/sessions
  network: false
  subprocess: false
`, id, id))
}

func opts() toolmanifest.ValidationOptions {
	return toolmanifest.ValidationOptions{
		Categories: map[domain.CategoryID]bool{"ai": true},
		Target:     toolmanifest.Target{OS: "darwin", Arch: "arm64"},
	}
}

func TestManifestValidoViraToolExterna(t *testing.T) {
	manifest, err := toolmanifest.ParseAndValidate(validManifest("example-tool"), opts())
	if err != nil {
		t.Fatal(err)
	}
	tool, err := manifest.Tool("/instalacao", "/instalacao/lealing-tool-example")
	if err != nil {
		t.Fatal(err)
	}
	if tool.ID != "example-tool" || !tool.Interactive() || tool.Kind != domain.KindProcess {
		t.Fatalf("tool = %+v", tool)
	}
	if tool.Runtime.ProtocolMin != 1 || len(tool.Runtime.Permissions.ReadPaths) != 1 {
		t.Errorf("runtime = %+v", tool.Runtime)
	}
	if tool.WantsMouse() {
		t.Error("wantsMouse omitido deveria ficar false, deixando o mouse livre para o terminal")
	}
}

func TestManifestPropagaWantsMouse(t *testing.T) {
	raw := strings.Replace(string(validManifest("mouse-tool")), "capabilities:\n    - navigation.back",
		"capabilities:\n    - navigation.back\n  wantsMouse: true", 1)
	manifest, err := toolmanifest.ParseAndValidate([]byte(raw), opts())
	if err != nil {
		t.Fatal(err)
	}
	tool, err := manifest.Tool("/instalacao", "/instalacao/lealing-tool-mouse")
	if err != nil {
		t.Fatal(err)
	}
	if !tool.WantsMouse() {
		t.Error("ui.wantsMouse: true deveria propagar para tool.WantsMouse()")
	}
}

func TestManifestRecusaCamposInvalidos(t *testing.T) {
	tests := map[string]func(string) string{
		"apiVersion": func(s string) string { return strings.Replace(s, "lealing.dev/v1", "lealing.dev/v9", 1) },
		"id":         func(s string) string { return strings.Replace(s, "id: example-tool", "id: ../example", 1) },
		"version":    func(s string) string { return strings.Replace(s, "version: 1.2.3", "version: latest", 1) },
		"summary multilinha": func(s string) string {
			return strings.Replace(s, "summary: Faz uma leitura segura.", "summary: |\n  linha um\n  linha dois", 1)
		},
		"summary sem ponto": func(s string) string {
			return strings.Replace(s, "Faz uma leitura segura.", "Faz uma leitura segura", 1)
		},
		"categoria":  func(s string) string { return strings.Replace(s, "category: ai", "category: fantasma", 1) },
		"risk":       func(s string) string { return strings.Replace(s, "risk: safe", "risk: nuclear", 1) },
		"protocolo":  func(s string) string { return strings.Replace(s, "min: 1", "min: 0", 1) },
		"plataforma": func(s string) string { return strings.Replace(s, "darwin-arm64", "plan9-mips", 1) },
		"permissão":  func(s string) string { return strings.Replace(s, "~/.codex/sessions", "../segredo", 1) },
		"escape no nome": func(s string) string {
			return strings.Replace(s, "name: Tool de teste", "name: \"Tool \\x1b]0;roubada\\x07\"", 1)
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			raw := mutate(string(validManifest("example-tool")))
			if _, err := toolmanifest.ParseAndValidate([]byte(raw), opts()); err == nil {
				t.Fatal("manifest inválido foi aceito")
			}
		})
	}
}

func TestExecutableNaoAceitaCaminhoArgumentoOuEscape(t *testing.T) {
	for _, executable := range []string{"/bin/tool", "../tool", `subdir\tool.exe`, "tool --flag", ""} {
		t.Run(executable, func(t *testing.T) {
			raw := strings.Replace(string(validManifest("demo")), "lealing-tool-demo", executable, 1)
			if _, err := toolmanifest.ParseAndValidate([]byte(raw), opts()); err == nil {
				t.Fatalf("executable %q foi aceito", executable)
			}
		})
	}
}

func TestVersionSegueSemver(t *testing.T) {
	for _, version := range []string{"01.2.3", "1.2", "1.2.3-..", "1.2.3-01", "1.2.3+"} {
		t.Run(version, func(t *testing.T) {
			raw := strings.Replace(string(validManifest("demo")), "version: 1.2.3", "version: "+version, 1)
			if _, err := toolmanifest.ParseAndValidate([]byte(raw), opts()); err == nil {
				t.Fatalf("versão %q foi aceita", version)
			}
		})
	}
}

func TestPlataformaIncompativelEFiltravel(t *testing.T) {
	manifest, err := toolmanifest.ParseAndValidate(validManifest("demo"), opts())
	if err != nil {
		t.Fatal(err)
	}
	if !manifest.Supports(toolmanifest.Target{OS: "darwin", Arch: "arm64"}) {
		t.Error("darwin-arm64 deveria ser aceito")
	}
	if manifest.Supports(toolmanifest.Target{OS: "windows", Arch: "arm64"}) {
		t.Error("windows-arm64 não foi declarado")
	}
	if got := manifest.ExecutableName(toolmanifest.Target{OS: "windows", Arch: "amd64"}); got != "lealing-tool-demo.exe" {
		t.Errorf("executável Windows = %s", got)
	}
}

func benchmarkManifests(b *testing.B, count int) {
	raws := make([][]byte, count)
	for i := range raws {
		raws[i] = validManifest(fmt.Sprintf("tool-%d", i))
	}
	b.ReportAllocs()
	for b.Loop() {
		for _, raw := range raws {
			if _, err := toolmanifest.ParseAndValidate(raw, opts()); err != nil {
				b.Fatal(err)
			}
		}
	}
}

func BenchmarkParse100Manifests(b *testing.B)   { benchmarkManifests(b, 100) }
func BenchmarkParse1000Manifests(b *testing.B)  { benchmarkManifests(b, 1_000) }
func BenchmarkParse10000Manifests(b *testing.B) { benchmarkManifests(b, 10_000) }
