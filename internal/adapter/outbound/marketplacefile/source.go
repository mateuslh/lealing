// Package marketplacefile lê índices de tools que moram no disco.
//
// É o adapter que torna "meu próprio repositório" um caminho de primeira
// classe: quem desenvolve uma tool aponta a engine para o diretório do
// projeto e instala o build local pelo mesmo fluxo do índice público, sem
// publicar release nem subir artefato em lugar nenhum.
package marketplacefile

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/mateuslh/lealing/internal/core/marketplace"
)

const (
	// IndexName é o arquivo procurado quando a origem aponta para um
	// diretório.
	IndexName = "index.json"

	defaultIndexLimit int64 = 4 << 20
)

type Config struct {
	// IndexLimit protege contra um arquivo enorme; zero usa o padrão.
	IndexLimit int64
}

type Source struct{ config Config }

var (
	_ marketplace.IndexSource   = (*Source)(nil)
	_ marketplace.PackageSource = (*Source)(nil)
)

func New(config Config) *Source {
	if config.IndexLimit <= 0 {
		config.IndexLimit = defaultIndexLimit
	}
	return &Source{config: config}
}

func (s *Source) Fetch(_ context.Context, origin marketplace.Origin) (marketplace.Index, error) {
	path, err := indexPath(origin)
	if err != nil {
		return marketplace.Index{}, err
	}
	file, err := os.Open(path)
	if err != nil {
		return marketplace.Index{}, fmt.Errorf("abrir índice local de %s: %w", origin.Name, err)
	}
	defer file.Close()

	raw, err := io.ReadAll(io.LimitReader(file, s.config.IndexLimit+1))
	if err != nil {
		return marketplace.Index{}, err
	}
	if int64(len(raw)) > s.config.IndexLimit {
		return marketplace.Index{}, fmt.Errorf("índice de %s excede o limite de %d bytes", origin.Name, s.config.IndexLimit)
	}

	var index marketplace.Index
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if err := decoder.Decode(&index); err != nil {
		return marketplace.Index{}, fmt.Errorf("JSON do índice de %s inválido: %w", origin.Name, err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return marketplace.Index{}, fmt.Errorf("índice de %s contém dados adicionais", origin.Name)
	}
	return index, nil
}

// Prepare devolve o diretório do build sem copiá-lo. Nada é baixado nem
// extraído: o instalador local já valida o manifest e faz a troca atômica, e
// duplicar centenas de megabytes a cada instalação de teste tornaria o ciclo
// de desenvolvimento mais lento do que o de publicação.
func (s *Source) Prepare(_ context.Context, origin marketplace.Origin, artifact marketplace.Artifact) (marketplace.PreparedPackage, error) {
	root, err := originRoot(origin)
	if err != nil {
		return marketplace.PreparedPackage{}, err
	}
	target, err := resolve(root, artifact.URL)
	if err != nil {
		return marketplace.PreparedPackage{}, err
	}
	info, err := os.Stat(target)
	if err != nil {
		return marketplace.PreparedPackage{}, fmt.Errorf("artefato local de %s: %w", origin.Name, err)
	}
	if !info.IsDir() {
		return marketplace.PreparedPackage{}, fmt.Errorf(
			"artefato local %s precisa ser um diretório com manifest.yaml", artifact.URL)
	}
	// Cleanup não remove nada: o diretório é o projeto do usuário, não uma
	// cópia temporária desta instalação.
	return marketplace.PreparedPackage{Directory: target, Cleanup: func() error { return nil }}, nil
}

// indexPath aceita tanto o diretório do repositório quanto o arquivo direto.
func indexPath(origin marketplace.Origin) (string, error) {
	root, err := originRoot(origin)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(root)
	if err != nil {
		return "", fmt.Errorf("origem local %s: %w", origin.Name, err)
	}
	if info.IsDir() {
		return filepath.Join(root, IndexName), nil
	}
	return root, nil
}

// originRoot devolve o diretório base para resolver artefatos relativos.
func originRoot(origin marketplace.Origin) (string, error) {
	if origin.Kind != marketplace.OriginLocal {
		return "", fmt.Errorf("origem %s não é local", origin.Name)
	}
	reference := strings.TrimSpace(origin.Ref)
	reference = strings.TrimPrefix(reference, "file://")
	if reference == "" {
		return "", errors.New("origem local sem caminho")
	}
	if !filepath.IsAbs(reference) {
		return "", fmt.Errorf("caminho da origem %s precisa ser absoluto: %q", origin.Name, reference)
	}
	return filepath.Clean(reference), nil
}

// resolve fixa o artefato dentro do repositório da origem. O core já recusa
// travessia na validação; conferir de novo aqui é o que impede um índice
// baixado de terceiros e apontado como local de alcançar ~/.ssh por um
// symlink no meio do caminho.
func resolve(root, reference string) (string, error) {
	base := root
	if info, err := os.Stat(root); err == nil && !info.IsDir() {
		base = filepath.Dir(root)
	}
	target := filepath.Join(base, filepath.FromSlash(strings.ReplaceAll(reference, `\`, "/")))

	resolvedBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", err
	}
	resolvedTarget, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", fmt.Errorf("artefato local %s: %w", reference, err)
	}
	relative, err := filepath.Rel(resolvedBase, resolvedTarget)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("artefato %q escapa do diretório da origem", reference)
	}
	return resolvedTarget, nil
}
