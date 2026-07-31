package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	core "github.com/mateuslh/lealing/internal/core/selfupdate"
)

// maxAsset limita o que aceitamos baixar. O binário tem alguns megabytes; uma
// resposta muito maior que isso é erro ou hostilidade, não uma release.
const maxAsset = 64 << 20

// Applier implementa core.Applier: troca o binário de release ou recompila o
// clone, conforme o modo detectado.
type Applier struct {
	repo   Repo
	host   string
	binary string
	pkg    string
	client *http.Client
}

var _ core.Applier = (*Applier)(nil)

// NewApplier monta o aplicador. binary é o nome do executável dentro do
// arquivo compactado, pkg é o pacote main usado ao recompilar do fonte.
func NewApplier(repo Repo, binary, pkg string) *Applier {
	return &Applier{
		repo:   repo,
		host:   defaultDownloadHost,
		binary: binary,
		pkg:    pkg,
		client: &http.Client{Timeout: 5 * time.Minute},
	}
}

// Apply implementa core.Applier.
func (a *Applier) Apply(ctx context.Context, in core.Install, rel core.Release) (core.Outcome, error) {
	switch in.Mode {
	case core.ModeSource:
		return a.fromSource(ctx, in)
	case core.ModeRelease:
		return a.fromRelease(ctx, in, rel)
	default:
		return core.Outcome{}, core.ErrNotApplicable
	}
}

// --- Caminho do binário de release -------------------------------------

// fromRelease baixa o artefato da plataforma, confere o checksum publicado e
// substitui o executável em disco.
func (a *Applier) fromRelease(ctx context.Context, in core.Install, rel core.Release) (core.Outcome, error) {
	if !in.Writable {
		return core.Outcome{}, fmt.Errorf(
			"sem permissão de escrita em %s — reinstale com o install.sh ou rode com privilégio",
			filepath.Dir(in.BinaryPath))
	}

	asset := AssetName(a.binary, runtime.GOOS, runtime.GOARCH)

	sums, err := a.fetchChecksums(ctx, rel.Tag)
	if err != nil {
		return core.Outcome{}, err
	}
	want, ok := sums[asset]
	if !ok {
		return core.Outcome{}, fmt.Errorf("%w: %s não está em checksums.txt", core.ErrNoAsset, asset)
	}

	// O arquivo vai para o diretório do binário, não para o temporário do
	// sistema: a troca final é um rename, e rename entre volumes diferentes
	// falha. Baixar já no destino garante o mesmo volume.
	dir := filepath.Dir(in.BinaryPath)
	archive, err := a.download(ctx, a.repo.downloadURL(a.host, rel.Tag, asset), dir, want)
	if err != nil {
		return core.Outcome{}, err
	}
	defer os.Remove(archive)

	extracted, err := extractBinary(archive, a.binary, dir)
	if err != nil {
		return core.Outcome{}, err
	}
	defer os.Remove(extracted)

	if err := replace(extracted, in.BinaryPath); err != nil {
		return core.Outcome{}, err
	}

	return core.Outcome{
		To:      rel.Tag,
		Detail:  "binário substituído em " + in.BinaryPath,
		Restart: true,
	}, nil
}

// AssetName é o nome do arquivo compactado de uma plataforma. Precisa casar
// com o name_template do .goreleaser.yaml — se um dos dois mudar sozinho, a
// tool de atualização para de achar o arquivo.
func AssetName(binary, goos, goarch string) string {
	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("%s_%s_%s.%s", binary, goos, goarch, ext)
}

// fetchChecksums baixa e interpreta o checksums.txt da release.
func (a *Applier) fetchChecksums(ctx context.Context, tag string) (map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.repo.downloadURL(a.host, tag, "checksums.txt"), nil)
	if err != nil {
		return nil, err
	}
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("checksums.txt de %s: %s", tag, resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	return ParseChecksums(string(body)), nil
}

// ParseChecksums lê o formato do sha256sum, que é o que o GoReleaser publica:
// "<hex>  <nome do arquivo>", um por linha.
func ParseChecksums(s string) map[string]string {
	out := make(map[string]string)
	for line := range strings.Lines(s) {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		sum := strings.ToLower(fields[0])
		// Linha que não começa com um hash hexadecimal não é um checksum:
		// é lixo, ou um cabeçalho, e aceitá-la calada colocaria um "hash"
		// inventado no mapa que decide se o download é confiável.
		if !isHex(sum) {
			continue
		}
		// O sha256sum prefixa o nome com '*' no modo binário.
		out[strings.TrimPrefix(fields[1], "*")] = sum
	}
	return out
}

// isHex informa se a string é hexadecimal não vazia.
func isHex(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// download grava a URL num arquivo temporário dentro de dir, conferindo o
// sha256 enquanto escreve.
//
// O hash é calculado no mesmo passo da escrita, e não numa releitura: é uma
// passada a menos sobre o arquivo e, principalmente, garante que o que foi
// verificado é exatamente o que foi gravado.
func (a *Applier) download(ctx context.Context, url, dir, wantSum string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "lealing-selfupdate")

	resp, err := a.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("%w: %s respondeu %s", core.ErrNoAsset, path.Base(url), resp.Status)
	}

	f, err := os.CreateTemp(dir, ".lealing-download-*")
	if err != nil {
		return "", err
	}
	name := f.Name()

	sum := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(f, sum), io.LimitReader(resp.Body, maxAsset))
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil {
		os.Remove(name)
		return "", firstErr(copyErr, closeErr)
	}

	if got := hex.EncodeToString(sum.Sum(nil)); got != wantSum {
		os.Remove(name)
		return "", fmt.Errorf("%w: %s esperava %s, veio %s", core.ErrChecksum, path.Base(url), wantSum, got)
	}
	return name, nil
}

// extractBinary tira o executável do arquivo compactado, devolvendo o caminho
// do arquivo extraído dentro de dir.
func extractBinary(archive, binary, dir string) (string, error) {
	if strings.HasSuffix(archive, ".zip") || runtime.GOOS == "windows" {
		if out, err := extractFromZip(archive, binary, dir); err == nil {
			return out, nil
		}
	}
	return extractFromTarGz(archive, binary, dir)
}

func extractFromTarGz(archive, binary, dir string) (string, error) {
	f, err := os.Open(archive)
	if err != nil {
		return "", err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return "", fmt.Errorf("%s não está no arquivo baixado", binary)
		}
		if err != nil {
			return "", err
		}
		if hdr.Typeflag != tar.TypeReg || !isBinary(hdr.Name, binary) {
			continue
		}
		return writeTemp(tr, dir)
	}
}

func extractFromZip(archive, binary, dir string) (string, error) {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return "", err
	}
	defer zr.Close()

	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !isBinary(f.Name, binary) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", err
		}
		defer rc.Close()
		return writeTemp(rc, dir)
	}
	return "", fmt.Errorf("%s não está no arquivo baixado", binary)
}

// isBinary casa o executável ignorando o diretório interno do arquivo e a
// extensão do Windows.
func isBinary(entry, binary string) bool {
	base := path.Base(filepath.ToSlash(entry))
	return base == binary || base == binary+".exe"
}

// writeTemp grava o conteúdo extraído já com permissão de execução.
func writeTemp(r io.Reader, dir string) (string, error) {
	f, err := os.CreateTemp(dir, ".lealing-new-*")
	if err != nil {
		return "", err
	}
	name := f.Name()

	_, copyErr := io.Copy(f, io.LimitReader(r, maxAsset))
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil {
		os.Remove(name)
		return "", firstErr(copyErr, closeErr)
	}
	if err := os.Chmod(name, 0o755); err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}

// replace põe o arquivo novo no lugar do antigo.
//
// O rename direto resolve no Unix, inclusive com o binário em execução. No
// Windows ele falha sobre um executável aberto — mas *renomear* o que está
// rodando é permitido, então tiramos o antigo do caminho primeiro e o
// apagamos depois, quando o processo já tiver saído.
func replace(newFile, target string) error {
	if err := os.Rename(newFile, target); err == nil {
		return nil
	}

	backup := target + ".old"
	_ = os.Remove(backup)
	if err := os.Rename(target, backup); err != nil {
		return fmt.Errorf("não foi possível liberar %s: %w", target, err)
	}
	if err := os.Rename(newFile, target); err != nil {
		// Desfaz: melhor voltar ao binário antigo que deixar o usuário sem
		// nenhum.
		_ = os.Rename(backup, target)
		return err
	}
	_ = os.Remove(backup)
	return nil
}

// --- Caminho do clone --------------------------------------------------

// fromSource traz os commits novos e recompila.
func (a *Applier) fromSource(ctx context.Context, in core.Install) (core.Outcome, error) {
	git, err := exec.LookPath("git")
	if err != nil {
		return core.Outcome{}, fmt.Errorf("git não encontrado no PATH: %w", err)
	}
	goBin, err := exec.LookPath("go")
	if err != nil {
		return core.Outcome{}, fmt.Errorf("go não encontrado no PATH: %w", err)
	}

	// --ff-only: um merge automático num clone com trabalho local é
	// exatamente o tipo de surpresa que uma tool de atualização não pode
	// causar. Se não avançar em linha reta, o usuário resolve à mão.
	if out, err := runIn(ctx, in.RepoDir, git, "pull", "--ff-only"); err != nil {
		return core.Outcome{}, fmt.Errorf("git pull falhou: %s", firstLine(out))
	}

	version := describe(ctx, git, in.RepoDir)
	target := in.BinaryPath
	if target == "" {
		target = filepath.Join(in.RepoDir, "bin", a.binary)
	}

	// Compila para um arquivo ao lado e só então substitui: um build que
	// falha no meio não pode deixar um binário truncado no lugar do que
	// funcionava.
	staged := target + ".new"
	out, err := runIn(ctx, in.RepoDir, goBin, "build", "-trimpath",
		"-ldflags", "-s -w -X main.version="+version,
		"-o", staged, a.pkg)
	if err != nil {
		os.Remove(staged)
		return core.Outcome{}, fmt.Errorf("go build falhou: %s", firstLine(out))
	}
	if err := replace(staged, target); err != nil {
		os.Remove(staged)
		return core.Outcome{}, err
	}

	return core.Outcome{
		To:      version,
		Detail:  "clone atualizado e recompilado em " + in.RepoDir,
		Restart: true,
	}, nil
}

// describe devolve a versão do clone depois do pull.
func describe(ctx context.Context, git, repo string) string {
	out, err := runIn(ctx, repo, git, "describe", "--tags", "--always", "--dirty")
	if err != nil {
		return "dev"
	}
	return strings.TrimSpace(out)
}

// runIn executa um comando no diretório do clone, com stdout e stderr juntos:
// quando o git ou o go falham, a mensagem que interessa está no stderr.
func runIn(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// firstLine reduz a saída de um comando à primeira linha não vazia, que é o
// que cabe na tela.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return "sem saída"
}

// firstErr devolve o primeiro erro não nulo.
func firstErr(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}
