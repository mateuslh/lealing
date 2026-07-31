// Package registry agrega múltiplos ToolProvider em um único repositório.
//
// É o adapter que permite ao lealing escalar para centenas de tools sem um
// arquivo monolítico: cada domínio publica seu provider e o registry
// consolida, valida e indexa tudo uma única vez.
package registry

import (
	"context"
	"fmt"
	"sync"

	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/port/outbound"
)

// Registry implementa outbound.ToolRepository.
//
// A carga é lazy e roda no máximo uma vez (sync.Once), então o custo de
// validar centenas de tools é pago no primeiro acesso e nunca mais.
type Registry struct {
	providers []outbound.ToolProvider
	log       outbound.Logger
	// strict decide o que fazer com uma tool inválida: abortar a carga ou
	// descartá-la com um warn. Em desenvolvimento, strict pega erros cedo.
	strict bool
	// platform descarta as tools que não rodam nesta máquina. Zero desliga o
	// filtro, que é o que a documentação e os testes querem: eles precisam
	// enxergar o acervo inteiro, não o recorte da máquina que os roda.
	platform domain.Platform

	loadMu sync.Mutex
	loaded bool
	err    error
	mu     sync.RWMutex
	tools  []domain.Tool
	byID   map[domain.ToolID]domain.Tool
	cats   []domain.Category
}

var _ outbound.ToolRepository = (*Registry)(nil)
var _ outbound.ReloadableToolRepository = (*Registry)(nil)

// Option configura o Registry na construção.
type Option func(*Registry)

// WithLogger injeta o logger usado para reportar tools descartadas.
func WithLogger(l outbound.Logger) Option { return func(r *Registry) { r.log = l } }

// WithStrict faz a carga falhar ao encontrar a primeira tool inválida.
func WithStrict(strict bool) Option { return func(r *Registry) { r.strict = strict } }

// WithPlatform restringe o acervo às tools que rodam na plataforma indicada.
//
// Uma tool escondida é melhor que uma tool que abre e diz "não funciona
// aqui": a segunda ocupa espaço na busca e nas sugestões para não fazer nada.
func WithPlatform(p domain.Platform) Option { return func(r *Registry) { r.platform = p } }

// New monta o registry sobre os providers informados.
func New(providers []outbound.ToolProvider, opts ...Option) *Registry {
	r := &Registry{providers: providers}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// All implementa outbound.ToolRepository.
func (r *Registry) All(ctx context.Context) ([]domain.Tool, error) {
	if err := r.load(ctx); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tools, nil
}

// ByID implementa outbound.ToolRepository.
func (r *Registry) ByID(ctx context.Context, id domain.ToolID) (domain.Tool, error) {
	if err := r.load(ctx); err != nil {
		return domain.Tool{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.byID[id]
	if !ok {
		return domain.Tool{}, domain.WrapTool(id, domain.ErrToolNotFound)
	}
	return t, nil
}

// Categories implementa outbound.ToolRepository.
func (r *Registry) Categories(ctx context.Context) ([]domain.Category, error) {
	if err := r.load(ctx); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cats, nil
}

// load consolida todos os providers. Idempotente e seguro para concorrência.
func (r *Registry) load(ctx context.Context) error {
	r.loadMu.Lock()
	defer r.loadMu.Unlock()
	if r.loaded {
		return r.err
	}
	r.err = r.doLoad(ctx)
	r.loaded = true
	return r.err
}

// Reload invalida providers com cache e recompõe o índice em memória. Ele é
// chamado apenas depois de uma mutação explícita no diretório de tools.
func (r *Registry) Reload(ctx context.Context) error {
	r.loadMu.Lock()
	defer r.loadMu.Unlock()
	for _, provider := range r.providers {
		if invalidatable, ok := provider.(interface{ Invalidate() }); ok {
			invalidatable.Invalidate()
		}
	}
	r.err = r.doLoad(ctx)
	r.loaded = true
	return r.err
}

func (r *Registry) doLoad(ctx context.Context) error {
	var (
		tools = make([]domain.Tool, 0, 256)
		byID  = make(map[domain.ToolID]domain.Tool, 256)
		cats  []domain.Category
		seen  = make(map[domain.CategoryID]bool)
	)

	for _, p := range r.providers {
		ts, cs, err := p.Provide(ctx)
		if err != nil {
			return fmt.Errorf("provider %s: %w", p.Name(), err)
		}

		for _, c := range cs {
			if c.ID == "" || seen[c.ID] {
				continue
			}
			seen[c.ID] = true
			cats = append(cats, c)
		}

		for _, t := range ts {
			if err := t.Validate(); err != nil {
				if r.strict {
					return fmt.Errorf("provider %s: %w", p.Name(), err)
				}
				r.warn("tool descartada", "provider", p.Name(), "err", err)
				continue
			}
			if r.platform != 0 && !t.RunsOn(r.platform) {
				r.debug("tool fora desta plataforma", "tool", t.ID, "suporta", t.SupportedPlatforms())
				continue
			}
			if _, dup := byID[t.ID]; dup {
				if r.strict {
					return fmt.Errorf("provider %s: %w", p.Name(), domain.WrapTool(t.ID, domain.ErrDuplicateTool))
				}
				r.warn("tool duplicada ignorada", "provider", p.Name(), "tool", t.ID)
				continue
			}
			byID[t.ID] = t
			tools = append(tools, t)
		}
	}

	// Uma tool apontando para categoria inexistente ficaria invisível na
	// navegação por categoria — melhor detectar aqui do que na tela.
	for _, t := range tools {
		if !seen[t.Category] {
			if r.strict {
				return &domain.ValidationError{ID: t.ID, Field: "Category", Reason: "não foi declarada por nenhum provider"}
			}
			r.warn("categoria não declarada", "tool", t.ID, "categoria", t.Category)
		}
	}

	domain.SortCategories(cats)

	r.mu.Lock()
	r.tools, r.byID, r.cats = tools, byID, cats
	r.mu.Unlock()
	return nil
}

func (r *Registry) warn(msg string, kv ...any) {
	if r.log != nil {
		r.log.Warn(msg, kv...)
	}
}

// debug reporta o que é esperado, não um defeito: uma tool de macOS ausente
// no Windows é o filtro funcionando.
func (r *Registry) debug(msg string, kv ...any) {
	if r.log != nil {
		r.log.Debug(msg, kv...)
	}
}

// Static é o ToolProvider mais simples possível: um lote fixo em memória.
// Serve tanto para as tools embutidas quanto para testes.
type Static struct {
	Label      string
	Tools      []domain.Tool
	Categories []domain.Category
}

var _ outbound.ToolProvider = (*Static)(nil)

// Name implementa outbound.ToolProvider.
func (s *Static) Name() string {
	if s.Label == "" {
		return "static"
	}
	return s.Label
}

// Provide implementa outbound.ToolProvider.
func (s *Static) Provide(context.Context) ([]domain.Tool, []domain.Category, error) {
	return s.Tools, s.Categories, nil
}
