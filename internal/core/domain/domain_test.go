package domain_test

import (
	"testing"
	"time"

	"github.com/mateuslh/lealing/internal/core/domain"
)

func TestUsageScore(t *testing.T) {
	now := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	week := 7 * 24 * time.Hour

	t.Run("uso zerado não pontua", func(t *testing.T) {
		if got := (domain.Usage{}).Score(now); got != 0 {
			t.Fatalf("Score = %v, quero 0", got)
		}
	})

	t.Run("recência decai pela metade a cada semana", func(t *testing.T) {
		fresh := domain.Usage{Runs: 8, LastRun: now}
		old := domain.Usage{Runs: 8, LastRun: now.Add(-week)}
		if got, want := old.Score(now), fresh.Score(now)/2; got != want {
			t.Fatalf("Score após uma semana = %v, quero %v", got, want)
		}
	})

	t.Run("favorita supera qualquer frequência", func(t *testing.T) {
		fav := domain.Usage{Favorite: true}
		heavy := domain.Usage{Runs: 100, LastRun: now}
		if fav.Score(now) <= heavy.Score(now) {
			t.Fatalf("favorita (%v) deveria superar a mais usada (%v)",
				fav.Score(now), heavy.Score(now))
		}
	})

	t.Run("LastRun no futuro não infla o score", func(t *testing.T) {
		// Relógio do sistema pode andar para trás; o score não pode explodir.
		future := domain.Usage{Runs: 4, LastRun: now.Add(week)}
		if got, want := future.Score(now), 4.0; got != want {
			t.Fatalf("Score = %v, quero %v", got, want)
		}
	})
}

func TestQueryNormalize(t *testing.T) {
	tests := []struct {
		name     string
		in       domain.Query
		wantTerm string
		check    func(*testing.T, domain.Query)
	}{
		{
			name:     "extrai filtros inline e preserva o texto livre",
			in:       domain.Query{Term: "tag:git kind:process branch"},
			wantTerm: "branch",
			check: func(t *testing.T, q domain.Query) {
				if len(q.Tags) != 1 || q.Tags[0] != "git" {
					t.Errorf("Tags = %v, quero [git]", q.Tags)
				}
				if len(q.Kinds) != 1 || q.Kinds[0] != domain.KindProcess {
					t.Errorf("Kinds = %v, quero [process]", q.Kinds)
				}
			},
		},
		{
			name:     "kind desconhecido não vira filtro nem some do termo",
			in:       domain.Query{Term: "kind:banana"},
			wantTerm: "",
			check: func(t *testing.T, q domain.Query) {
				if len(q.Kinds) != 0 {
					t.Errorf("Kinds = %v, quero vazio", q.Kinds)
				}
			},
		},
		{
			name:     "is:fav liga o filtro de favoritas",
			in:       domain.Query{Term: "is:fav deploy"},
			wantTerm: "deploy",
			check: func(t *testing.T, q domain.Query) {
				if !q.FavoritesOnly {
					t.Error("FavoritesOnly = false, quero true")
				}
			},
		},
		{
			name:     "texto com dois-pontos e chave desconhecida vira termo",
			in:       domain.Query{Term: "http://exemplo"},
			wantTerm: "http://exemplo",
			check:    func(*testing.T, domain.Query) {},
		},
		{
			name:     "busca vazia troca relevância por ordenação inteligente",
			in:       domain.Query{},
			wantTerm: "",
			check: func(t *testing.T, q domain.Query) {
				if q.Sort != domain.SortSmart {
					t.Errorf("Sort = %v, quero SortSmart", q.Sort)
				}
				if q.Limit != domain.DefaultPageSize {
					t.Errorf("Limit = %d, quero %d", q.Limit, domain.DefaultPageSize)
				}
			},
		},
		{
			name:     "offset negativo é saneado",
			in:       domain.Query{Offset: -5},
			wantTerm: "",
			check: func(t *testing.T, q domain.Query) {
				if q.Offset != 0 {
					t.Errorf("Offset = %d, quero 0", q.Offset)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.in.Normalize()
			if got.Term != tc.wantTerm {
				t.Errorf("Term = %q, quero %q", got.Term, tc.wantTerm)
			}
			tc.check(t, got)
		})
	}
}

func TestQueryMatches(t *testing.T) {
	tool := domain.Tool{
		ID: "git/push", Name: "push", Category: "git",
		Kind: domain.KindProcess, Risk: domain.RiskCaution,
		Tags: []domain.Tag{"git", "remoto"},
	}

	caution := domain.RiskCaution
	safe := domain.RiskSafe

	tests := []struct {
		name  string
		query domain.Query
		usage domain.Usage
		want  bool
	}{
		{"sem filtros passa", domain.Query{}, domain.Usage{}, true},
		{"categoria certa passa", domain.Query{Categories: []domain.CategoryID{"git"}}, domain.Usage{}, true},
		{"categoria errada barra", domain.Query{Categories: []domain.CategoryID{"k8s"}}, domain.Usage{}, false},
		{"todas as tags precisam bater", domain.Query{Tags: []domain.Tag{"git", "remoto"}}, domain.Usage{}, true},
		{"uma tag ausente barra", domain.Query{Tags: []domain.Tag{"git", "local"}}, domain.Usage{}, false},
		{"risco dentro do teto passa", domain.Query{MaxRisk: &caution}, domain.Usage{}, true},
		{"risco acima do teto barra", domain.Query{MaxRisk: &safe}, domain.Usage{}, false},
		{"favoritas barra não-favorita", domain.Query{FavoritesOnly: true}, domain.Usage{}, false},
		{"favoritas passa favorita", domain.Query{FavoritesOnly: true}, domain.Usage{Favorite: true}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.query.Matches(tool, tc.usage); got != tc.want {
				t.Errorf("Matches = %v, quero %v", got, tc.want)
			}
		})
	}
}

func TestToolValidate(t *testing.T) {
	valid := domain.Tool{ID: "a/b", Name: "b", Category: "a"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("tool válida rejeitada: %v", err)
	}

	invalid := map[string]domain.Tool{
		"sem ID":        {Name: "b", Category: "a"},
		"sem nome":      {ID: "a/b", Category: "a"},
		"sem categoria": {ID: "a/b", Name: "b"},
		"kind inválido": {ID: "a/b", Name: "b", Category: "a", Kind: 99},
		"risk inválido": {ID: "a/b", Name: "b", Category: "a", Risk: 99},
	}
	for name, tool := range invalid {
		t.Run(name, func(t *testing.T) {
			err := tool.Validate()
			if err == nil {
				t.Fatal("Validate = nil, quero erro")
			}
			var target *domain.ValidationError
			if !errorsAs(err, &target) {
				t.Fatalf("erro %v não é *ValidationError", err)
			}
		})
	}
}

func TestToolIDNamespace(t *testing.T) {
	tests := map[string]string{
		"git/status": "git",
		"status":     "",
		"/leading":   "",
		"a/b/c":      "a",
	}
	for id, want := range tests {
		if got := domain.ToolID(id).Namespace(); got != want {
			t.Errorf("ToolID(%q).Namespace() = %q, quero %q", id, got, want)
		}
	}
}

// errorsAs isola o uso de errors.As para manter a tabela acima legível.
func errorsAs(err error, target **domain.ValidationError) bool {
	for err != nil {
		if v, ok := err.(*domain.ValidationError); ok {
			*target = v
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
