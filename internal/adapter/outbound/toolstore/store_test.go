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

// sourceWithNetwork é como source, mas declara acesso à rede — usado para
// exercitar ActivePermissions com um manifest que não tem tudo em falso.
func sourceWithNetwork(t *testing.T, version, executableBody string) string {
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
  filesystem: {read: ["~/proj"], write: []}
  network: true
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

func TestInstallVerificaChecksumETrocaVersaoAtiva(t *testing.T) {
	root := t.TempDir()
	first := source(t, "1.0.0", "primeiro")
	sum := sha256.Sum256([]byte("primeiro"))
	installed, err := store(root).Install(context.Background(), toolinstall.InstallRequest{
		Host: "lealing", SourceDir: first, ExpectedSHA256: hex.EncodeToString(sum[:]),
	})
	if err != nil {
		t.Fatal(err)
	}
	if installed.Host != "lealing" || installed.Version != "1.0.0" || installed.PreviousVersion != "" {
		t.Fatalf("instalação = %+v", installed)
	}

	second := source(t, "1.1.0", "segundo")
	updated, err := store(root).Install(context.Background(), toolinstall.InstallRequest{Host: "outro-host", SourceDir: second})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Host != "outro-host" || updated.PreviousVersion != "1.0.0" {
		t.Fatalf("atualização = %+v", updated)
	}
	active, _ := os.ReadFile(filepath.Join(root, "demo", "active"))
	if strings.TrimSpace(string(active)) != "1.1.0" {
		t.Errorf("active = %q", active)
	}
	listed, err := store(root).List(context.Background())
	if err != nil || len(listed) != 1 || listed[0].Host != "outro-host" {
		t.Fatalf("instalações = %+v, erro = %v", listed, err)
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

func TestInstallRecusaIdentidadeDiferenteDoIndice(t *testing.T) {
	root := t.TempDir()
	request := toolinstall.InstallRequest{
		SourceDir: source(t, "1.0.0", "ok"), ExpectedID: "outra-tool", ExpectedVersion: "2.0.0",
	}
	if _, err := store(root).Install(context.Background(), request); err == nil {
		t.Fatal("Install aceitou identidade diferente do índice")
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatalf("root alterado após recusa: entries=%v err=%v", entries, err)
	}
}

func TestInstallRecusaManifestQueDivergeDoAnunciadoNoMarketplace(t *testing.T) {
	root := t.TempDir()
	expected := &toolinstall.ManifestExpectation{
		ID: "demo", Version: "1.0.0", Name: "Demo",
		Summary: "Tool local de demonstração.", Detail: "Teste.", Category: "ai",
		Risk: "caution", ProtocolMin: 1, ProtocolMax: 1,
		FilesystemRead: []string{}, FilesystemWrite: []string{},
	}
	request := toolinstall.InstallRequest{
		SourceDir: source(t, "1.0.0", "ok"), ExpectedManifest: expected,
	}
	if _, err := store(root).Install(context.Background(), request); err == nil || !strings.Contains(err.Error(), "risk") {
		t.Fatalf("Install = %v", err)
	}
	if entries, err := os.ReadDir(root); err != nil || len(entries) != 0 {
		t.Fatalf("root alterado após recusa: entries=%v err=%v", entries, err)
	}

	expected.Risk = "safe"
	if _, err := store(root).Install(context.Background(), request); err != nil {
		t.Fatalf("manifest igual ao índice foi recusado: %v", err)
	}

	// Um índice que declara um nível diferente do pacote não instala: é por
	// essa ficha que o usuário decidiu confiar na tool.
	expected.WorkingDir = "write"
	if _, err := store(t.TempDir()).Install(context.Background(), request); err == nil ||
		!strings.Contains(err.Error(), "permissions.workingDir") {
		t.Fatalf("Install = %v", err)
	}
}

// Índices publicados antes do campo existir não declaram workingDir. Tratar a
// omissão como divergência quebraria toda ferramenta de publicação anterior,
// então ela precisa continuar instalando.
func TestInstallAceitaIndiceAnteriorAoCampoWorkingDir(t *testing.T) {
	root := t.TempDir()
	source := source(t, "1.0.0", "ok")
	manifest := filepath.Join(source, "manifest.yaml")
	raw, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(raw), "permissions:", "permissions:\n  workingDir: write", 1)
	if err := os.WriteFile(manifest, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}

	request := toolinstall.InstallRequest{
		SourceDir: source,
		ExpectedManifest: &toolinstall.ManifestExpectation{
			ID: "demo", Version: "1.0.0", Name: "Demo",
			Summary: "Tool local de demonstração.", Detail: "Teste.", Category: "ai",
			Risk: "safe", ProtocolMin: 1, ProtocolMax: 1,
			FilesystemRead: []string{}, FilesystemWrite: []string{},
		},
	}
	if _, err := store(root).Install(context.Background(), request); err != nil {
		t.Fatalf("índice sem o campo foi recusado: %v", err)
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
	for index, version := range []string{"1.0.0", "1.1.0"} {
		host := []string{"primeiro-host", "segundo-host"}[index]
		if _, err := s.Install(context.Background(), toolinstall.InstallRequest{Host: host, SourceDir: source(t, version, version)}); err != nil {
			t.Fatal(err)
		}
	}
	result, err := s.Rollback(context.Background(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if result.Host != "primeiro-host" || result.Version != "1.0.0" || result.PreviousVersion != "1.1.0" {
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

func TestActivePermissionsSemInstalacaoNaoEErro(t *testing.T) {
	root := t.TempDir()
	permissions, installed, err := store(root).ActivePermissions(context.Background(), "demo")
	if err != nil {
		t.Fatalf("ActivePermissions sem instalação devolveu erro: %v", err)
	}
	if installed {
		t.Fatal("installed = true para tool nunca instalada")
	}
	if !permissions.Empty() {
		t.Fatalf("permissões = %+v, queria vazio", permissions)
	}
}

func TestActivePermissionsLeADaVersaoAtiva(t *testing.T) {
	root := t.TempDir()
	s := store(root)
	if _, err := s.Install(context.Background(), toolinstall.InstallRequest{SourceDir: sourceWithNetwork(t, "1.0.0", "ok")}); err != nil {
		t.Fatal(err)
	}
	permissions, installed, err := s.ActivePermissions(context.Background(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if !installed {
		t.Fatal("installed = false após instalação")
	}
	if !permissions.Network || len(permissions.ReadPaths) != 1 || permissions.ReadPaths[0] != "~/proj" {
		t.Fatalf("permissões = %+v", permissions)
	}
}

// TestActivePermissionsAtualizaAposTrocaDeVersao garante que a leitura
// reflete a versão ativa corrente, não uma versão antiga que ficou para trás
// como "previous".
func TestActivePermissionsAtualizaAposTrocaDeVersao(t *testing.T) {
	root := t.TempDir()
	s := store(root)
	if _, err := s.Install(context.Background(), toolinstall.InstallRequest{SourceDir: source(t, "1.0.0", "primeiro")}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Install(context.Background(), toolinstall.InstallRequest{SourceDir: sourceWithNetwork(t, "1.1.0", "segundo")}); err != nil {
		t.Fatal(err)
	}
	permissions, installed, err := s.ActivePermissions(context.Background(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if !installed || !permissions.Network {
		t.Fatalf("permissões após atualização = %+v installed=%v", permissions, installed)
	}
}

func TestActivePermissionsManifestCorrompidoEErroNaoAusenciaDePermissao(t *testing.T) {
	root := t.TempDir()
	s := store(root)
	if _, err := s.Install(context.Background(), toolinstall.InstallRequest{SourceDir: sourceWithNetwork(t, "1.0.0", "ok")}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "demo", "1.0.0", "manifest.yaml"), []byte("não é yaml válido: [["), 0o600); err != nil {
		t.Fatal(err)
	}
	permissions, installed, err := s.ActivePermissions(context.Background(), "demo")
	if err == nil {
		t.Fatal("manifest corrompido deveria falhar, não devolver permissões vazias")
	}
	if installed {
		t.Fatal("installed não deveria ser true junto com um erro")
	}
	if !permissions.Empty() {
		t.Fatalf("permissões deveriam vir zeradas junto com o erro, veio %+v", permissions)
	}
}

func TestReconcileTrocaConjuntoInteiroEPodeRestaurar(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tools")
	s := store(root)
	if _, err := s.Install(context.Background(), toolinstall.InstallRequest{
		Host: "origem-antiga", SourceDir: source(t, "1.0.0", "antiga"),
	}); err != nil {
		t.Fatal(err)
	}

	result, err := s.Reconcile(context.Background(), []toolinstall.InstallRequest{{
		Host: "origem-nova", SourceDir: source(t, "2.0.0", "nova"),
		ExpectedID: "demo", ExpectedVersion: "2.0.0",
	}})
	if err != nil {
		t.Fatal(err)
	}
	installed, err := s.List(context.Background())
	if err != nil || len(installed) != 1 || installed[0].Host != "origem-nova" || installed[0].ActiveVersion != "2.0.0" {
		t.Fatalf("estado reconciliado = %+v, erro = %v", installed, err)
	}
	if result.Changed != 1 || result.RecoveryDir == "" || !strings.Contains(result.RecoveryDir, string(filepath.Separator)+".trash"+string(filepath.Separator)) {
		t.Fatalf("reconciliação = %+v", result)
	}

	if err := s.Restore(context.Background(), result); err != nil {
		t.Fatal(err)
	}
	installed, err = s.List(context.Background())
	if err != nil || len(installed) != 1 || installed[0].Host != "origem-antiga" || installed[0].ActiveVersion != "1.0.0" {
		t.Fatalf("estado restaurado = %+v, erro = %v", installed, err)
	}
}

func TestReconcileFalhaAntesDaTrocaEDeixaEstadoAtivoIntacto(t *testing.T) {
	root := filepath.Join(t.TempDir(), "tools")
	s := store(root)
	if _, err := s.Install(context.Background(), toolinstall.InstallRequest{
		Host: "lealing", SourceDir: source(t, "1.0.0", "saudavel"),
	}); err != nil {
		t.Fatal(err)
	}
	broken := source(t, "2.0.0", "quebrada")
	if err := os.WriteFile(filepath.Join(broken, "manifest.yaml"), []byte("inválido"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := s.Reconcile(context.Background(), []toolinstall.InstallRequest{{
		Host: "lealing", SourceDir: broken, ExpectedID: "demo", ExpectedVersion: "2.0.0",
	}})
	if err == nil {
		t.Fatal("reconciliação inválida terminou sem erro")
	}
	installed, listErr := s.List(context.Background())
	if listErr != nil || len(installed) != 1 || installed[0].ActiveVersion != "1.0.0" {
		t.Fatalf("estado após falha = %+v, erro = %v", installed, listErr)
	}
}
