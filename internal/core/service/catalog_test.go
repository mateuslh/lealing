package service_test

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/port/outbound"
	"github.com/mateuslh/lealing/internal/core/service"
)

// --- Duplos de teste ---------------------------------------------------

// fakeRepo é um ToolRepository em memória.
type fakeRepo struct {
	tools []domain.Tool
	cats  []domain.Category
	err   error
}

func (r *fakeRepo) All(context.Context) ([]domain.Tool, error) { return r.tools, r.err }

func (r *fakeRepo) ByID(_ context.Context, id domain.ToolID) (domain.Tool, error) {
	for _, t := range r.tools {
		if t.ID == id {
			return t, nil
		}
	}
	return domain.Tool{}, domain.WrapTool(id, domain.ErrToolNotFound)
}

func (r *fakeRepo) Categories(context.Context) ([]domain.Category, error) { return r.cats, r.err }

// fakeStore é um UsageStore em memória que conta as escritas.
type fakeStore struct {
	data   map[domain.ToolID]domain.Usage
	writes int
	loads  int
}

func newStore(seed ...domain.Usage) *fakeStore {
	s := &fakeStore{data: make(map[domain.ToolID]domain.Usage)}
	for _, u := range seed {
		s.data[u.ToolID] = u
	}
	return s
}

func (s *fakeStore) Load(context.Context) (map[domain.ToolID]domain.Usage, error) {
	s.loads++
	out := make(map[domain.ToolID]domain.Usage, len(s.data))
	for k, v := range s.data {
		out[k] = v
	}
	return out, nil
}

func (s *fakeStore) Save(_ context.Context, u domain.Usage) error {
	s.writes++
	s.data[u.ToolID] = u
	return nil
}

// prefixSearcher ranqueia por prefixo do nome — determinístico e suficiente
// para testar o pipeline sem depender do algoritmo fuzzy real.
//
// Ordena o resultado porque o contrato de outbound.Searcher exige relevância
// decrescente: SortRelevance confia nessa ordem e não reordena.
type prefixSearcher struct{}

func (prefixSearcher) Rank(term string, candidates []domain.Tool) []domain.Match {
	var out []domain.Match
	for _, t := range candidates {
		if len(t.Name) >= len(term) && t.Name[:len(term)] == term {
			out = append(out, domain.Match{Tool: t, Score: float64(100 - len(t.Name))})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

var testNow = time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)

func fixedClock() outbound.Clock { return outbound.ClockFunc(func() time.Time { return testNow }) }

func fixture() *fakeRepo {
	return &fakeRepo{
		cats: []domain.Category{
			{ID: "git", Name: "Git", Order: 1},
			{ID: "k8s", Name: "Kubernetes", Order: 2},
		},
		tools: []domain.Tool{
			{ID: "git/status", Name: "status", Category: "git", Kind: domain.KindProcess},
			{ID: "git/stash", Name: "stash", Category: "git", Kind: domain.KindBuiltin},
			{ID: "git/push", Name: "push", Category: "git", Kind: domain.KindProcess, Risk: domain.RiskCaution},
			{ID: "k8s/pods", Name: "pods", Category: "k8s", Kind: domain.KindRemote},
			{ID: "k8s/drain", Name: "drain", Category: "k8s", Kind: domain.KindRemote, Risk: domain.RiskDestructive},
		},
	}
}

// --- Testes ------------------------------------------------------------

func TestBrowseFiltraPorCategoria(t *testing.T) {
	svc := service.NewCatalog(fixture(), prefixSearcher{}, newStore(), fixedClock())

	page, err := svc.Browse(context.Background(), domain.Query{
		Categories: []domain.CategoryID{"k8s"},
	})
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	if page.Total != 2 {
		t.Fatalf("Total = %d, quero 2", page.Total)
	}
	for _, m := range page.Items {
		if m.Tool.Category != "k8s" {
			t.Errorf("veio tool da categoria %q", m.Tool.Category)
		}
	}
}

func TestBrowseAceitaFiltroInlineNoTermo(t *testing.T) {
	svc := service.NewCatalog(fixture(), prefixSearcher{}, newStore(), fixedClock())

	page, err := svc.Browse(context.Background(), domain.Query{Term: "cat:git"})
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	if page.Total != 3 {
		t.Fatalf("Total = %d, quero 3 (as três tools de git)", page.Total)
	}
}

func TestBrowseOrdenaPorUso(t *testing.T) {
	store := newStore(
		domain.Usage{ToolID: "k8s/pods", Runs: 20, LastRun: testNow},
		domain.Usage{ToolID: "git/stash", Favorite: true},
	)
	svc := service.NewCatalog(fixture(), prefixSearcher{}, store, fixedClock())

	page, err := svc.Browse(context.Background(), domain.Query{Sort: domain.SortSmart})
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	// Favorita ganha o piso de 1000, então vem antes da mais executada.
	if got := page.Items[0].Tool.ID; got != "git/stash" {
		t.Errorf("primeiro = %s, quero git/stash", got)
	}
	if got := page.Items[1].Tool.ID; got != "k8s/pods" {
		t.Errorf("segundo = %s, quero k8s/pods", got)
	}
}

func TestBrowsePagina(t *testing.T) {
	svc := service.NewCatalog(fixture(), prefixSearcher{}, newStore(), fixedClock())
	ctx := context.Background()

	page, err := svc.Browse(ctx, domain.Query{Sort: domain.SortAlpha, Limit: 2})
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	if page.Len() != 2 || page.Total != 5 {
		t.Fatalf("página = %d itens de %d, quero 2 de 5", page.Len(), page.Total)
	}
	if !page.HasMore() {
		t.Error("HasMore = false, quero true")
	}

	last, err := svc.Browse(ctx, domain.Query{Sort: domain.SortAlpha, Offset: 4, Limit: 2})
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	if last.Len() != 1 || last.HasMore() {
		t.Errorf("última página = %d itens, HasMore = %v; quero 1 e false", last.Len(), last.HasMore())
	}

	// Offset além do fim devolve página vazia, não erro nem panic.
	beyond, err := svc.Browse(ctx, domain.Query{Offset: 99})
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	if beyond.Len() != 0 || beyond.Total != 5 {
		t.Errorf("além do fim = %d itens de %d, quero 0 de 5", beyond.Len(), beyond.Total)
	}
}

func TestBrowseUsaSearcherQuandoHaTermo(t *testing.T) {
	svc := service.NewCatalog(fixture(), prefixSearcher{}, newStore(), fixedClock())

	page, err := svc.Browse(context.Background(), domain.Query{Term: "st"})
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	if page.Total != 2 {
		t.Fatalf("Total = %d, quero 2 (status e stash)", page.Total)
	}
	// prefixSearcher pontua nomes curtos mais alto; SortRelevance preserva.
	if got := page.Items[0].Tool.Name; got != "stash" {
		t.Errorf("primeiro = %s, quero stash", got)
	}
}

func TestBrowseCombinaRelevanciaTextualComUsoNoCore(t *testing.T) {
	store := newStore(domain.Usage{
		ToolID:  "git/status",
		Runs:    20,
		LastRun: testNow,
	})
	svc := service.NewCatalog(fixture(), prefixSearcher{}, store, fixedClock())

	page, err := svc.Browse(context.Background(), domain.Query{Term: "st"})
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	if got := page.Items[0].Tool.ID; got != "git/status" {
		t.Errorf("primeiro = %s, quero a tool frequente git/status", got)
	}
}

func TestCategoriesContaTools(t *testing.T) {
	svc := service.NewCatalog(fixture(), prefixSearcher{}, newStore(), fixedClock())

	cats, err := svc.Categories(context.Background())
	if err != nil {
		t.Fatalf("Categories: %v", err)
	}
	want := map[domain.CategoryID]int{"git": 3, "k8s": 2}
	for _, c := range cats {
		if got := want[c.ID]; c.Count != got {
			t.Errorf("categoria %s tem Count %d, quero %d", c.ID, c.Count, got)
		}
	}
}

func TestHighlightsNaoRepeteToolEntrePaineis(t *testing.T) {
	store := newStore(
		domain.Usage{ToolID: "git/stash", Favorite: true, Runs: 5, LastRun: testNow},
		domain.Usage{ToolID: "k8s/pods", Runs: 3, LastRun: testNow.Add(-time.Hour)},
	)
	svc := service.NewCatalog(fixture(), prefixSearcher{}, store, fixedClock())

	h, err := svc.Highlights(context.Background(), 5)
	if err != nil {
		t.Fatalf("Highlights: %v", err)
	}
	if h.TotalTools != 5 || h.TotalCategories != 2 {
		t.Errorf("totais = %d tools / %d categorias, quero 5 / 2", h.TotalTools, h.TotalCategories)
	}

	seen := map[domain.ToolID]string{}
	for _, m := range h.Favorites {
		seen[m.Tool.ID] = "favoritas"
	}
	for _, m := range h.Recent {
		seen[m.Tool.ID] = "recentes"
	}
	for _, m := range h.Suggested {
		if painel, dup := seen[m.Tool.ID]; dup {
			t.Errorf("tool %s aparece em sugeridas e em %s", m.Tool.ID, painel)
		}
	}
}

func TestToggleFavoritePersisteEInverte(t *testing.T) {
	store := newStore()
	svc := service.NewCatalog(fixture(), prefixSearcher{}, store, fixedClock())
	ctx := context.Background()

	on, err := svc.ToggleFavorite(ctx, "git/push")
	if err != nil {
		t.Fatalf("ToggleFavorite: %v", err)
	}
	if !on {
		t.Fatal("primeiro toggle = false, quero true")
	}
	if !store.data["git/push"].Favorite {
		t.Error("favorito não chegou ao store")
	}

	off, err := svc.ToggleFavorite(ctx, "git/push")
	if err != nil {
		t.Fatalf("ToggleFavorite: %v", err)
	}
	if off {
		t.Error("segundo toggle = true, quero false")
	}
}

func TestToggleFavoriteRejeitaToolInexistente(t *testing.T) {
	svc := service.NewCatalog(fixture(), prefixSearcher{}, newStore(), fixedClock())

	if _, err := svc.ToggleFavorite(context.Background(), "nao/existe"); err == nil {
		t.Fatal("ToggleFavorite = nil, quero ErrToolNotFound")
	}
}

func TestRecordRunIncrementaEMarcaHorario(t *testing.T) {
	store := newStore()
	svc := service.NewCatalog(fixture(), prefixSearcher{}, store, fixedClock())
	ctx := context.Background()

	for range 3 {
		if err := svc.RecordRun(ctx, "git/status"); err != nil {
			t.Fatalf("RecordRun: %v", err)
		}
	}

	u, err := svc.Usage(ctx, "git/status")
	if err != nil {
		t.Fatalf("Usage: %v", err)
	}
	if u.Runs != 3 {
		t.Errorf("Runs = %d, quero 3", u.Runs)
	}
	if !u.LastRun.Equal(testNow) {
		t.Errorf("LastRun = %v, quero %v", u.LastRun, testNow)
	}
}

func TestUsageStoreCarregaUmaVezSo(t *testing.T) {
	store := newStore()
	svc := service.NewCatalog(fixture(), prefixSearcher{}, store, fixedClock())
	ctx := context.Background()

	// A home chama Browse e Highlights a cada tecla; nenhum deles pode
	// reabrir o arquivo de uso.
	for range 5 {
		if _, err := svc.Browse(ctx, domain.Query{}); err != nil {
			t.Fatalf("Browse: %v", err)
		}
		if _, err := svc.Highlights(ctx, 3); err != nil {
			t.Fatalf("Highlights: %v", err)
		}
	}
	if store.loads != 1 {
		t.Errorf("Load chamado %d vezes, quero 1", store.loads)
	}
}
