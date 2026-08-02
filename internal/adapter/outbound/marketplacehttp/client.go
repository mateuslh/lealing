// Package marketplacehttp lê índices e prepara pacotes do marketplace sem
// executar binários. Downloads são limitados, conferidos por SHA-256 e
// extraídos em diretórios temporários confinados.
package marketplacehttp

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/mateuslh/lealing/internal/core/marketplace"
)

const (
	defaultIndexLimit   int64 = 4 << 20
	defaultArchiveLimit int64 = 128 << 20
	defaultExtractLimit int64 = 256 << 20
	defaultFileLimit          = 32
)

type HTTPClient interface {
	Do(request *http.Request) (*http.Response, error)
}

type Config struct {
	Client HTTPClient
	// TemporaryRoot hospeda os downloads em andamento; a URL de cada índice
	// chega em Fetch, porque a engine consulta várias origens.
	TemporaryRoot string
	// AllowHTTP existe para testes herméticos e índices locais de
	// desenvolvimento. O bootstrap público nunca o habilita.
	AllowHTTP                              bool
	IndexLimit, ArchiveLimit, ExtractLimit int64
	FileLimit                              int
}

type Source struct {
	config Config
}

var (
	_ marketplace.IndexSource   = (*Source)(nil)
	_ marketplace.PackageSource = (*Source)(nil)
)

func New(config Config) *Source {
	if config.Client == nil {
		config.Client = http.DefaultClient
	}
	if config.IndexLimit <= 0 {
		config.IndexLimit = defaultIndexLimit
	}
	if config.ArchiveLimit <= 0 {
		config.ArchiveLimit = defaultArchiveLimit
	}
	if config.ExtractLimit <= 0 {
		config.ExtractLimit = defaultExtractLimit
	}
	if config.FileLimit <= 0 {
		config.FileLimit = defaultFileLimit
	}
	return &Source{config: config}
}

func (s *Source) Fetch(ctx context.Context, origin marketplace.Origin) (marketplace.Index, error) {
	response, err := s.get(ctx, origin.Ref)
	if err != nil {
		return marketplace.Index{}, fmt.Errorf("baixar índice de %s: %w", origin.Name, err)
	}
	defer response.Body.Close()
	if response.ContentLength > s.config.IndexLimit {
		return marketplace.Index{}, fmt.Errorf("índice excede o limite de %d bytes", s.config.IndexLimit)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, s.config.IndexLimit+1))
	if err != nil {
		return marketplace.Index{}, err
	}
	if int64(len(raw)) > s.config.IndexLimit {
		return marketplace.Index{}, fmt.Errorf("índice excede o limite de %d bytes", s.config.IndexLimit)
	}
	var index marketplace.Index
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&index); err != nil {
		return marketplace.Index{}, fmt.Errorf("JSON do índice inválido: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return marketplace.Index{}, errors.New("JSON do índice contém dados adicionais")
	}
	return index, nil
}

func (s *Source) Prepare(ctx context.Context, _ marketplace.Origin, artifact marketplace.Artifact) (_ marketplace.PreparedPackage, resultErr error) {
	if s.config.TemporaryRoot == "" {
		return marketplace.PreparedPackage{}, errors.New("diretório temporário do marketplace não configurado")
	}
	if err := os.MkdirAll(s.config.TemporaryRoot, 0o700); err != nil {
		return marketplace.PreparedPackage{}, err
	}
	root, err := os.MkdirTemp(s.config.TemporaryRoot, "package-")
	if err != nil {
		return marketplace.PreparedPackage{}, err
	}
	cleanup := func() error { return os.RemoveAll(root) }
	defer func() {
		if resultErr != nil {
			_ = cleanup()
		}
	}()

	archivePath := filepath.Join(root, "download")
	if err := s.download(ctx, artifact, archivePath); err != nil {
		return marketplace.PreparedPackage{}, err
	}
	content := filepath.Join(root, "content")
	if err := os.Mkdir(content, 0o700); err != nil {
		return marketplace.PreparedPackage{}, err
	}
	parsed, err := url.Parse(artifact.URL)
	if err != nil {
		return marketplace.PreparedPackage{}, err
	}
	switch {
	case strings.HasSuffix(strings.ToLower(parsed.Path), ".zip"):
		err = s.extractZIP(ctx, archivePath, content)
	case strings.HasSuffix(strings.ToLower(parsed.Path), ".tar.gz"), strings.HasSuffix(strings.ToLower(parsed.Path), ".tgz"):
		err = s.extractTarGZ(ctx, archivePath, content)
	default:
		err = errors.New("formato de pacote desconhecido; use .zip, .tar.gz ou .tgz")
	}
	if err != nil {
		return marketplace.PreparedPackage{}, err
	}
	return marketplace.PreparedPackage{Directory: content, Cleanup: cleanup}, nil
}

func (s *Source) download(ctx context.Context, artifact marketplace.Artifact, destination string) error {
	response, err := s.get(ctx, artifact.URL)
	if err != nil {
		return fmt.Errorf("baixar artefato: %w", err)
	}
	defer response.Body.Close()
	if response.ContentLength > s.config.ArchiveLimit {
		return fmt.Errorf("pacote excede o limite de %d bytes", s.config.ArchiveLimit)
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(file, hash), io.LimitReader(response.Body, s.config.ArchiveLimit+1))
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if written > s.config.ArchiveLimit {
		return fmt.Errorf("pacote excede o limite de %d bytes", s.config.ArchiveLimit)
	}
	got := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(got, artifact.SHA256) {
		return fmt.Errorf("checksum do pacote não confere: esperado %s, recebido %s", artifact.SHA256, got)
	}
	return ctx.Err()
}

func (s *Source) get(ctx context.Context, rawURL string) (*http.Response, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "https" && !(s.config.AllowHTTP && parsed.Scheme == "http")) {
		return nil, errors.New("URL remota insegura")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json, application/zip, application/gzip, application/octet-stream")
	response, err := s.config.Client.Do(request)
	if err != nil {
		return nil, err
	}
	if response.Request != nil && response.Request.URL != nil {
		final := response.Request.URL
		if final.User != nil || (final.Scheme != "https" && !(s.config.AllowHTTP && final.Scheme == "http")) {
			response.Body.Close()
			return nil, errors.New("redirecionamento para URL insegura")
		}
	}
	if response.StatusCode != http.StatusOK {
		response.Body.Close()
		return nil, fmt.Errorf("servidor respondeu %s", response.Status)
	}
	return response, nil
}

func (s *Source) extractZIP(ctx context.Context, archivePath, destination string) error {
	reader, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("abrir ZIP: %w", err)
	}
	defer reader.Close()
	if len(reader.File) > s.config.FileLimit {
		return fmt.Errorf("pacote excede o limite de %d arquivos", s.config.FileLimit)
	}
	var total int64
	for _, item := range reader.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		target, err := safeTarget(destination, item.Name)
		if err != nil {
			return err
		}
		if item.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
			continue
		}
		if !item.Mode().IsRegular() {
			return fmt.Errorf("entrada ZIP não regular recusada: %s", item.Name)
		}
		if item.UncompressedSize64 > uint64(s.config.ExtractLimit-total) {
			return fmt.Errorf("conteúdo extraído excede %d bytes", s.config.ExtractLimit)
		}
		input, err := item.Open()
		if err != nil {
			return err
		}
		written, err := extractFile(ctx, target, input, s.config.ExtractLimit-total)
		input.Close()
		if err != nil {
			return err
		}
		total += written
	}
	return nil
}

func (s *Source) extractTarGZ(ctx context.Context, archivePath, destination string) error {
	file, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer file.Close()
	compressed, err := gzip.NewReader(file)
	if err != nil {
		return fmt.Errorf("abrir gzip: %w", err)
	}
	defer compressed.Close()
	reader := tar.NewReader(compressed)
	var total int64
	files := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		header, err := reader.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("ler tar: %w", err)
		}
		files++
		if files > s.config.FileLimit {
			return fmt.Errorf("pacote excede o limite de %d arquivos", s.config.FileLimit)
		}
		target, err := safeTarget(destination, header.Name)
		if err != nil {
			return err
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if header.Size < 0 || header.Size > s.config.ExtractLimit-total {
				return fmt.Errorf("conteúdo extraído excede %d bytes", s.config.ExtractLimit)
			}
			written, err := extractFile(ctx, target, reader, s.config.ExtractLimit-total)
			if err != nil {
				return err
			}
			total += written
		default:
			return fmt.Errorf("entrada tar não regular recusada: %s", header.Name)
		}
	}
}

func safeTarget(root, archiveName string) (string, error) {
	// ZIP permite barras invertidas mesmo fora do Windows; normalizar antes de
	// path.Clean fecha a mesma travessia em todas as plataformas de build.
	archiveName = strings.ReplaceAll(archiveName, `\`, "/")
	clean := path.Clean(archiveName)
	if clean == "." {
		return root, nil
	}
	if clean == "" || path.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("entrada escapa do pacote: %q", archiveName)
	}
	target := filepath.Join(root, filepath.FromSlash(clean))
	relative, err := filepath.Rel(root, target)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("entrada escapa do pacote: %q", archiveName)
	}
	return target, nil
}

func extractFile(ctx context.Context, target string, input io.Reader, remaining int64) (int64, error) {
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return 0, err
	}
	output, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	written, copyErr := copyContext(ctx, output, io.LimitReader(input, remaining+1))
	closeErr := output.Close()
	if copyErr != nil {
		return written, copyErr
	}
	if closeErr != nil {
		return written, closeErr
	}
	if written > remaining {
		return written, errors.New("conteúdo extraído excede o limite")
	}
	return written, nil
}

func copyContext(ctx context.Context, destination io.Writer, source io.Reader) (int64, error) {
	buffer := make([]byte, 32<<10)
	var written int64
	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		read, readErr := source.Read(buffer)
		if read > 0 {
			n, writeErr := destination.Write(buffer[:read])
			written += int64(n)
			if writeErr != nil {
				return written, writeErr
			}
			if n != read {
				return written, io.ErrShortWrite
			}
		}
		if readErr == io.EOF {
			return written, nil
		}
		if readErr != nil {
			return written, readErr
		}
	}
}
