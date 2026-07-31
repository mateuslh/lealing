// Package service contém os casos de uso do lealing. É a única camada que
// orquestra portas de saída para atender portas de entrada.
package service

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/port"
)

// CatalogService implementa port.Catalog e port.Preferences sobre um
// repositório de tools, um buscador e um store de uso.
//
// O estado de uso é mantido em memória e escrito de forma write-through: a
// home é redesenhada a cada tecla e não pode pagar I/O por frame.
type CatalogService struct {
	repo     port.ToolRepository
	searcher port.Searcher
	usage    port.UsageStore
	clock    port.Clock

	mu     sync.RWMutex
	cache  map[domain.ToolID]domain.Usage
	loaded bool
}

// Garante em tempo de compilação que os contratos são satisfeitos.
var (
	_ port.Catalog     = (*CatalogService)(nil)
	_ port.Preferences = (*CatalogService)(nil)
)

// NewCatalog monta o serviço. Um clock nil cai para o relógio de sistema.
func NewCatalog(repo port.ToolRepository, searcher port.Searcher, usage port.UsageStore, clock port.Clock) *CatalogService {
	if clock == nil {
		clock = port.SystemClock
	}
	return &CatalogService{
		repo:     repo,
		searcher: searcher,
		usage:    usage,
		clock:    clock,
		cache:    make(map[domain.ToolID]domain.Usage),
	}
}

// Browse implementa port.Catalog.
//
// O pipeline é: filtros estruturados → ranqueamento textual → ordenação →
// recorte de página. O texto livre roda depois dos filtros porque estes são
// baratos e reduzem muito o conjunto entregue ao Searcher.
func (s *CatalogService) Browse(ctx context.Context, q domain.Query) (domain.Page, error) {
	q = q.Normalize()

	all, err := s.repo.All(ctx)
	if err != nil {
		return domain.Page{}, err
	}
	if err := s.ensureUsage(ctx); err != nil {
		return domain.Page{}, err
	}

	filtered := make([]domain.Tool, 0, len(all))
	for _, t := range all {
		if q.Matches(t, s.usageOf(t.ID)) {
			filtered = append(filtered, t)
		}
	}

	var matches []domain.Match
	if q.Term != "" && s.searcher != nil {
		matches = s.searcher.Rank(q.Term, filtered)
		for i := range matches {
			matches[i].Usage = s.usageOf(matches[i].Tool.ID)
		}
	} else {
		matches = make([]domain.Match, len(filtered))
		for i, t := range filtered {
			matches[i] = domain.Match{Tool: t, Usage: s.usageOf(t.ID)}
		}
	}

	s.sortMatches(matches, q.Sort)

	total := len(matches)
	if q.Offset >= total {
		return domain.Page{Total: total, Offset: q.Offset}, nil
	}
	end := min(q.Offset+q.Limit, total)
	return domain.Page{
		Items:  matches[q.Offset:end],
		Total:  total,
		Offset: q.Offset,
	}, nil
}

// sortMatches aplica o critério pedido. SortRelevance preserva a ordem do
// Searcher; os demais reordenam de forma estável.
func (s *CatalogService) sortMatches(matches []domain.Match, by domain.SortBy) {
	now := s.clock.Now()
	switch by {
	case domain.SortRelevance:
		// Já vem ranqueado pelo Searcher.
	case domain.SortSmart:
		sort.SliceStable(matches, func(i, j int) bool {
			si, sj := matches[i].Usage.Score(now), matches[j].Usage.Score(now)
			if si != sj {
				return si > sj
			}
			return matches[i].Tool.Title() < matches[j].Tool.Title()
		})
	case domain.SortAlpha:
		sort.SliceStable(matches, func(i, j int) bool {
			return strings.ToLower(matches[i].Tool.Title()) < strings.ToLower(matches[j].Tool.Title())
		})
	case domain.SortRecent:
		sort.SliceStable(matches, func(i, j int) bool {
			return matches[i].Usage.LastRun.After(matches[j].Usage.LastRun)
		})
	}
}

// Lookup implementa port.Catalog.
func (s *CatalogService) Lookup(ctx context.Context, id domain.ToolID) (domain.Tool, error) {
	return s.repo.ByID(ctx, id)
}

// Categories implementa port.Catalog, resolvendo a contagem de tools.
func (s *CatalogService) Categories(ctx context.Context) ([]port.CategoryView, error) {
	cats, err := s.repo.Categories(ctx)
	if err != nil {
		return nil, err
	}
	all, err := s.repo.All(ctx)
	if err != nil {
		return nil, err
	}

	counts := make(map[domain.CategoryID]int, len(cats))
	for _, t := range all {
		counts[t.Category]++
	}

	views := make([]port.CategoryView, len(cats))
	for i, c := range cats {
		views[i] = port.CategoryView{Category: c, Count: counts[c.ID]}
	}
	return views, nil
}

// Highlights implementa port.Catalog, montando os recortes da home.
//
// Sugestões excluem o que já aparece em favoritas ou recentes: repetir a
// mesma tool em três painéis desperdiça a tela.
func (s *CatalogService) Highlights(ctx context.Context, limit int) (domain.Highlights, error) {
	if limit <= 0 {
		limit = 6
	}

	all, err := s.repo.All(ctx)
	if err != nil {
		return domain.Highlights{}, err
	}
	cats, err := s.repo.Categories(ctx)
	if err != nil {
		return domain.Highlights{}, err
	}
	if err := s.ensureUsage(ctx); err != nil {
		return domain.Highlights{}, err
	}

	// Só categorias com tool entram na contagem: a navegação esconde as
	// vazias, e um cabeçalho anunciando doze quando a sidebar mostra duas
	// faria o usuário procurar as dez que faltam.
	populated := make(map[domain.CategoryID]bool, len(cats))
	for _, t := range all {
		populated[t.Category] = true
	}

	h := domain.Highlights{TotalTools: len(all), TotalCategories: len(populated)}
	now := s.clock.Now()

	matches := make([]domain.Match, len(all))
	for i, t := range all {
		matches[i] = domain.Match{Tool: t, Usage: s.usageOf(t.ID)}
	}

	for _, m := range matches {
		if m.Usage.Favorite && len(h.Favorites) < limit {
			h.Favorites = append(h.Favorites, m)
		}
	}
	sort.SliceStable(h.Favorites, func(i, j int) bool {
		return h.Favorites[i].Tool.Title() < h.Favorites[j].Tool.Title()
	})

	recent := make([]domain.Match, 0, len(matches))
	for _, m := range matches {
		if !m.Usage.LastRun.IsZero() {
			recent = append(recent, m)
		}
	}
	sort.SliceStable(recent, func(i, j int) bool {
		return recent[i].Usage.LastRun.After(recent[j].Usage.LastRun)
	})
	h.Recent = recent[:min(limit, len(recent))]

	seen := make(map[domain.ToolID]bool, 2*limit)
	for _, m := range h.Favorites {
		seen[m.Tool.ID] = true
	}
	for _, m := range h.Recent {
		seen[m.Tool.ID] = true
	}

	pool := make([]domain.Match, 0, len(matches))
	for _, m := range matches {
		if !seen[m.Tool.ID] {
			pool = append(pool, m)
		}
	}
	sort.SliceStable(pool, func(i, j int) bool {
		si, sj := pool[i].Usage.Score(now), pool[j].Usage.Score(now)
		if si != sj {
			return si > sj
		}
		return pool[i].Tool.Title() < pool[j].Tool.Title()
	})
	h.Suggested = pool[:min(limit, len(pool))]

	return h, nil
}

// ToggleFavorite implementa port.Preferences.
func (s *CatalogService) ToggleFavorite(ctx context.Context, id domain.ToolID) (bool, error) {
	if _, err := s.repo.ByID(ctx, id); err != nil {
		return false, err
	}
	if err := s.ensureUsage(ctx); err != nil {
		return false, err
	}

	s.mu.Lock()
	u := s.cache[id]
	u.ToolID = id
	u.Favorite = !u.Favorite
	s.cache[id] = u
	s.mu.Unlock()

	if err := s.usage.Save(ctx, u); err != nil {
		return u.Favorite, domain.WrapTool(id, err)
	}
	return u.Favorite, nil
}

// Usage implementa port.Preferences.
func (s *CatalogService) Usage(ctx context.Context, id domain.ToolID) (domain.Usage, error) {
	if err := s.ensureUsage(ctx); err != nil {
		return domain.Usage{}, err
	}
	return s.usageOf(id), nil
}

// RecordRun contabiliza uma execução. Não faz parte de port.Preferences
// porque o chamador é o LauncherService, não um driving adapter.
func (s *CatalogService) RecordRun(ctx context.Context, id domain.ToolID) error {
	if err := s.ensureUsage(ctx); err != nil {
		return err
	}

	s.mu.Lock()
	u := s.cache[id]
	u.ToolID = id
	u.Runs++
	u.LastRun = s.clock.Now()
	s.cache[id] = u
	s.mu.Unlock()

	return domain.WrapTool(id, s.usage.Save(ctx, u))
}

// ensureUsage carrega o store uma única vez, sob lock de escrita.
func (s *CatalogService) ensureUsage(ctx context.Context) error {
	s.mu.RLock()
	loaded := s.loaded
	s.mu.RUnlock()
	if loaded {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return nil
	}
	data, err := s.usage.Load(ctx)
	if err != nil {
		return err
	}
	if data != nil {
		s.cache = data
	}
	s.loaded = true
	return nil
}

// usageOf lê o uso em cache; ausência devolve o zero-value, que é válido.
func (s *CatalogService) usageOf(id domain.ToolID) domain.Usage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	u := s.cache[id]
	u.ToolID = id
	return u
}
