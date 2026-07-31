// Package externalcatalog descobre tools instaladas lendo somente manifests.
package externalcatalog

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/port/outbound"
	"github.com/mateuslh/lealing/internal/toolmanifest"
)

type Provider struct {
	root       string
	categories map[domain.CategoryID]bool
	reserved   map[domain.ToolID]bool
	target     toolmanifest.Target
	strict     bool
	log        outbound.Logger

	mu     sync.Mutex
	loaded bool
	tools  []domain.Tool
	err    error
}

var _ outbound.ToolProvider = (*Provider)(nil)

type Options struct {
	Root       string
	Categories []domain.Category
	Reserved   []domain.ToolID
	Target     toolmanifest.Target
	Strict     bool
	Logger     outbound.Logger
}

func New(opts Options) *Provider {
	categories := make(map[domain.CategoryID]bool, len(opts.Categories))
	for _, category := range opts.Categories {
		categories[category.ID] = true
	}
	reserved := make(map[domain.ToolID]bool, len(opts.Reserved))
	for _, id := range opts.Reserved {
		reserved[id] = true
	}
	return &Provider{
		root: opts.Root, categories: categories, reserved: reserved,
		target: opts.Target, strict: opts.Strict, log: opts.Logger,
	}
}

func (*Provider) Name() string { return "installed-tools" }

// Provide é lazy e cacheado. Nenhum caminho deste método abre ou executa o
// binário declarado: a existência só será conferida pelo runtime no spawn.
func (p *Provider) Provide(ctx context.Context) ([]domain.Tool, []domain.Category, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.loaded {
		p.tools, p.err = p.discover(ctx)
		p.loaded = true
	}
	return append([]domain.Tool(nil), p.tools...), nil, p.err
}

// Invalidate força a próxima leitura a redescobrir manifests. O método não
// abre nem executa binários e só é chamado depois de uma instalação explícita.
func (p *Provider) Invalidate() {
	p.mu.Lock()
	p.loaded = false
	p.tools = nil
	p.err = nil
	p.mu.Unlock()
}

func (p *Provider) discover(ctx context.Context) ([]domain.Tool, error) {
	entries, err := os.ReadDir(p.root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("ler diretório de tools: %w", err)
	}

	var tools []domain.Tool
	seen := map[domain.ToolID]bool{}
	for _, entry := range entries {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		tool, err := p.readActive(entry.Name())
		if err != nil {
			if p.strict {
				return nil, err
			}
			p.warn("tool instalada ignorada", "diretório", entry.Name(), "err", err)
			continue
		}
		if tool == nil {
			continue
		}
		if p.reserved[tool.ID] {
			err := fmt.Errorf("tool externa %q tenta sobrescrever ID builtin reservado", tool.ID)
			if p.strict {
				return nil, err
			}
			p.warn("ID builtin reservado ignorado", "tool", tool.ID)
			continue
		}
		if seen[tool.ID] {
			err := fmt.Errorf("%w: %s", domain.ErrDuplicateTool, tool.ID)
			if p.strict {
				return nil, err
			}
			p.warn("ID externo duplicado ignorado", "tool", tool.ID)
			continue
		}
		seen[tool.ID] = true
		tools = append(tools, *tool)
	}
	return tools, nil
}

func (p *Provider) readActive(directory string) (*domain.Tool, error) {
	idDir, err := confinedJoin(p.root, directory)
	if err != nil {
		return nil, err
	}
	activeRaw, err := os.ReadFile(filepath.Join(idDir, "active"))
	if err != nil {
		return nil, fmt.Errorf("%s: ponteiro active: %w", directory, err)
	}
	version := strings.TrimSpace(string(activeRaw))
	versionDir, err := confinedJoin(idDir, version)
	if err != nil {
		return nil, fmt.Errorf("%s: versão ativa insegura: %w", directory, err)
	}
	raw, err := os.ReadFile(filepath.Join(versionDir, "manifest.yaml"))
	if err != nil {
		return nil, fmt.Errorf("%s@%s: %w", directory, version, err)
	}
	manifest, err := toolmanifest.ParseAndValidate(raw, toolmanifest.ValidationOptions{
		Categories: p.categories,
		Target:     p.target,
	})
	if err != nil {
		return nil, err
	}
	if manifest.ID != directory || manifest.Version != version {
		return nil, fmt.Errorf("manifest %s@%s não corresponde ao diretório %s@%s", manifest.ID, manifest.Version, directory, version)
	}
	if !manifest.Supports(p.target) {
		return nil, nil
	}
	executable, err := confinedJoin(versionDir, manifest.ExecutableName(p.target))
	if err != nil {
		return nil, fmt.Errorf("manifest %s: executable escapa da instalação: %w", manifest.ID, err)
	}
	tool, err := manifest.Tool(versionDir, executable)
	if err != nil {
		return nil, err
	}
	return &tool, nil
}

func confinedJoin(root, element string) (string, error) {
	if element == "" || filepath.IsAbs(element) {
		return "", fmt.Errorf("caminho vazio ou absoluto")
	}
	joined := filepath.Join(root, element)
	rel, err := filepath.Rel(root, joined)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("caminho escapa de %s", root)
	}
	return joined, nil
}

func (p *Provider) warn(message string, kv ...any) {
	if p.log != nil {
		p.log.Warn(message, kv...)
	}
}
