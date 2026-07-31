package selfupdate

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	core "github.com/mateuslh/lealing/internal/core/selfupdate"
)

// --- Parsers -----------------------------------------------------------

func TestParseChecksums(t *testing.T) {
	// Formato do sha256sum, que é o que o GoReleaser publica. A terceira
	// linha vem no modo binário, com '*' antes do nome.
	in := "d0b1e5 lealing_darwin_arm64.tar.gz\n" +
		"AB12CD  lealing_linux_amd64.tar.gz\n" +
		"ff00ee *lealing_windows_amd64.zip\n" +
		"\nlinha quebrada\n"

	got := ParseChecksums(in)
	want := map[string]string{
		"lealing_darwin_arm64.tar.gz": "d0b1e5",
		"lealing_linux_amd64.tar.gz":  "ab12cd",
		"lealing_windows_amd64.zip":   "ff00ee",
	}
	if len(got) != len(want) {
		t.Fatalf("%d entradas, quero %d: %v", len(got), len(want), got)
	}
	for name, sum := range want {
		if got[name] != sum {
			t.Errorf("%s = %q, quero %q", name, got[name], sum)
		}
	}
}

func TestAssetName(t *testing.T) {
	cases := map[string]string{
		"darwin_arm64":  "lealing_darwin_arm64.tar.gz",
		"linux_amd64":   "lealing_linux_amd64.tar.gz",
		"windows_amd64": "lealing_windows_amd64.zip",
	}
	for platform, want := range cases {
		goos, goarch := split(platform)
		if got := AssetName("lealing", goos, goarch); got != want {
			t.Errorf("%s: %q, quero %q", platform, got, want)
		}
	}
}

func split(platform string) (string, string) {
	for i := range platform {
		if platform[i] == '_' {
			return platform[:i], platform[i+1:]
		}
	}
	return platform, ""
}

func TestCleanNotes(t *testing.T) {
	in := "\r\n\r\n* Uma novidade\r\n\r\n\r\n* Outra novidade\r\n" +
		"**Full Changelog**: https://github.com/mateuslh/lealing/compare/v1.0.0...v1.1.0\n"

	got := CleanNotes(in)
	want := "* Uma novidade\n\n* Outra novidade"
	if got != want {
		t.Errorf("notas = %q, quero %q", got, want)
	}
}

// --- Localização da instalação -----------------------------------------

func TestLocateReconheceOClone(t *testing.T) {
	repo := t.TempDir()
	mustWrite(t, filepath.Join(repo, "go.mod"), "module github.com/mateuslh/lealing\n\ngo 1.26\n")
	mustMkdir(t, filepath.Join(repo, ".git"))
	bin := filepath.Join(repo, "bin")
	mustMkdir(t, bin)

	in, err := (&Locator{module: "github.com/mateuslh/lealing"}).locateFrom(context.Background(),
		filepath.Join(bin, "lealing"))
	if err != nil {
		t.Fatalf("locateFrom: %v", err)
	}
	if in.Mode != core.ModeSource {
		t.Errorf("modo %v, quero ModeSource", in.Mode)
	}
	if in.RepoDir != repo {
		t.Errorf("clone %q, quero %q", in.RepoDir, repo)
	}
}

func TestLocateRecusaOutroModulo(t *testing.T) {
	// Um binário guardado dentro de outro projeto Go não pode se dizer
	// "instalação de fonte": um git pull ali atualizaria o repositório
	// errado.
	repo := t.TempDir()
	mustWrite(t, filepath.Join(repo, "go.mod"), "module github.com/outra/pessoa\n")
	mustMkdir(t, filepath.Join(repo, ".git"))

	in, err := (&Locator{module: "github.com/mateuslh/lealing"}).locateFrom(context.Background(),
		filepath.Join(repo, "lealing"))
	if err != nil {
		t.Fatalf("locateFrom: %v", err)
	}
	if in.Mode != core.ModeRelease {
		t.Errorf("modo %v, quero ModeRelease", in.Mode)
	}
}

func TestLocateMarcaDiretorioSemEscrita(t *testing.T) {
	// No Windows o os.Chmod só mexe no atributo somente-leitura de arquivos e
	// não faz nada em diretório: a permissão de verdade está na ACL, que este
	// teste não tem como montar. O código sob teste é o mesmo nos dois — ele
	// tenta criar um arquivo e observa o resultado.
	if runtime.GOOS == "windows" {
		t.Skip("os.Chmod não tira a escrita de um diretório no Windows")
	}

	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Skipf("sem como tirar a escrita do diretório: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	in, err := (&Locator{}).locateFrom(context.Background(), filepath.Join(dir, "lealing"))
	if err != nil {
		t.Fatalf("locateFrom: %v", err)
	}
	if in.Writable {
		t.Error("diretório sem escrita foi reportado como gravável — o download só falharia no fim")
	}
}

// --- Troca do binário --------------------------------------------------

// TestApplyTrocaOBinario exercita o caminho inteiro: baixa o tarball do
// servidor de teste, confere o checksum, extrai e substitui o executável.
func TestApplyTrocaOBinario(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("o artefato do Windows é .zip; este teste monta um tar.gz")
	}

	const novo = "#!/bin/sh\necho versão nova\n"
	archive := tarGz(t, "lealing", novo)
	srv := releaseServer(t, map[string][]byte{
		AssetName("lealing", runtime.GOOS, runtime.GOARCH): archive,
		"checksums.txt": []byte(fmt.Sprintf("%s  %s\n",
			sha256hex(archive), AssetName("lealing", runtime.GOOS, runtime.GOARCH))),
	})

	dir := t.TempDir()
	target := filepath.Join(dir, "lealing")
	mustWrite(t, target, "#!/bin/sh\necho versão velha\n")
	if err := os.Chmod(target, 0o755); err != nil {
		t.Fatal(err)
	}

	applier := testApplier(srv.URL)
	out, err := applier.Apply(context.Background(),
		core.Install{Mode: core.ModeRelease, BinaryPath: target, Writable: true},
		core.Release{Tag: "v1.4.0"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if out.To != "v1.4.0" || !out.Restart {
		t.Errorf("outcome = %+v, quero v1.4.0 com Restart", out)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != novo {
		t.Errorf("conteúdo = %q, quero %q", got, novo)
	}

	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Error("binário instalado sem permissão de execução")
	}
	// Nada de sobras: o tarball e o arquivo intermediário saem do diretório.
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("sobraram arquivos no diretório: %v", names(entries))
	}
}

func TestApplyRecusaChecksumErrado(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("o artefato do Windows é .zip; este teste monta um tar.gz")
	}

	asset := AssetName("lealing", runtime.GOOS, runtime.GOARCH)
	srv := releaseServer(t, map[string][]byte{
		asset:           tarGz(t, "lealing", "conteúdo adulterado"),
		"checksums.txt": []byte("0000000000000000000000000000000000000000000000000000000000000000  " + asset + "\n"),
	})

	dir := t.TempDir()
	target := filepath.Join(dir, "lealing")
	const original = "#!/bin/sh\necho versão velha\n"
	mustWrite(t, target, original)

	_, err := testApplier(srv.URL).Apply(context.Background(),
		core.Install{Mode: core.ModeRelease, BinaryPath: target, Writable: true},
		core.Release{Tag: "v1.4.0"})
	if !errors.Is(err, core.ErrChecksum) {
		t.Fatalf("erro %v, quero ErrChecksum", err)
	}

	got, _ := os.ReadFile(target)
	if string(got) != original {
		t.Error("o binário foi trocado apesar do checksum não conferir")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("o download recusado ficou em disco: %v", names(entries))
	}
}

func TestApplyRecusaDiretorioSemEscrita(t *testing.T) {
	// Sem nem tocar na rede: descobrir isso depois do download seria
	// descobrir tarde demais.
	_, err := testApplier("http://127.0.0.1:1").Apply(context.Background(),
		core.Install{Mode: core.ModeRelease, BinaryPath: "/usr/bin/lealing"},
		core.Release{Tag: "v1.4.0"})
	if err == nil {
		t.Fatal("aceitou atualizar um binário em diretório sem escrita")
	}
}

// --- Auxiliares --------------------------------------------------------

func testApplier(host string) *Applier {
	a := NewApplier(Repo{Owner: "mateuslh", Name: "lealing"}, "lealing", "./cmd/lealing")
	a.host = host
	return a
}

// releaseServer serve os artefatos no mesmo caminho que o GitHub usa.
func releaseServer(t *testing.T, files map[string][]byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := files[filepath.Base(r.URL.Path)]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func tarGz(t *testing.T, name, content string) []byte {
	t.Helper()
	var buf writeBuffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	hdr := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(content)), Typeflag: tar.TypeReg}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.data
}

// writeBuffer é um bytes.Buffer mínimo — o suficiente para o gzip escrever.
type writeBuffer struct{ data []byte }

func (b *writeBuffer) Write(p []byte) (int, error) {
	b.data = append(b.data, p...)
	return len(p), nil
}

func sha256hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func names(entries []os.DirEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name()
	}
	return out
}
