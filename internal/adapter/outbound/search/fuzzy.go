// Package search implementa a porta Searcher.
package search

import (
	"strings"

	"github.com/sahilm/fuzzy"

	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/port/outbound"
)

// Fuzzy ranqueia tools por casamento aproximado sobre um corpus que inclui
// nome, ID, resumo, keywords e tags.
//
// O score produzido aqui é exclusivamente textual. Favoritos, frequência e
// recência pertencem à política do caso de uso e são combinados pelo serviço
// do catálogo.
type Fuzzy struct{}

var _ outbound.Searcher = (*Fuzzy)(nil)

// NewFuzzy monta o buscador.
func NewFuzzy() *Fuzzy { return &Fuzzy{} }

// corpus adapta []domain.Tool à interface fuzzy.Source, evitando alocar um
// []string paralelo a cada tecla digitada.
type corpus struct {
	tools []domain.Tool
	text  []string
}

func (c corpus) String(i int) string { return c.text[i] }
func (c corpus) Len() int            { return len(c.tools) }

// Rank implementa outbound.Searcher.
func (f *Fuzzy) Rank(term string, candidates []domain.Tool) []domain.Match {
	term = strings.TrimSpace(strings.ToLower(term))
	if term == "" || len(candidates) == 0 {
		out := make([]domain.Match, len(candidates))
		for i, t := range candidates {
			out[i] = domain.Match{Tool: t}
		}
		return out
	}

	src := corpus{tools: candidates, text: make([]string, len(candidates))}
	for i, t := range candidates {
		src.text[i] = t.SearchCorpus()
	}

	found := fuzzy.FindFrom(term, src)
	out := make([]domain.Match, 0, len(found))

	for _, m := range found {
		tool := candidates[m.Index]
		score := float64(m.Score)

		// O termo inteiro dentro do corpus vale mais que uma subsequência
		// espalhada, e o score do fuzzy não distingue os dois: ele penaliza
		// a distância até o início do texto, então uma tool com "pmset" nas
		// keywords perde para outra onde as cinco letras aparecem soltas ao
		// longo de um resumo mais curto. Sem este reforço, cada tool nova
		// com resumo comprido embaralha as buscas por termo exato.
		if strings.Contains(src.text[m.Index], term) {
			score += 100
		}

		// Casar no início do nome é o sinal mais forte de intenção.
		lowerName := strings.ToLower(tool.Name)
		switch {
		case strings.HasPrefix(lowerName, term):
			score += 120
		case strings.Contains(lowerName, term):
			score += 60
		}
		if strings.HasPrefix(strings.ToLower(string(tool.ID)), term) {
			score += 40
		}

		match := domain.Match{Tool: tool, Score: score}

		// As posições do fuzzy apontam para o corpus concatenado; só as
		// mantemos quando caem dentro do nome, que é o que a lista desenha.
		match.Positions = clampPositions(m.MatchedIndexes, len(tool.Name))
		out = append(out, match)
	}

	// Reordena após a reponderação (FindFrom devolve pela ordem dele).
	insertionSortByScore(out)
	return out
}

// clampPositions descarta índices fora do trecho renderizado.
func clampPositions(idx []int, limit int) []int {
	if len(idx) == 0 {
		return nil
	}
	out := make([]int, 0, len(idx))
	for _, i := range idx {
		if i < limit {
			out = append(out, i)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// insertionSortByScore ordena por score decrescente. Insertion sort é a
// escolha certa aqui: o conjunto pós-filtro é pequeno (dezenas) e quase
// ordenado, então isso bate sort.Slice sem alocar closure nem interface.
func insertionSortByScore(m []domain.Match) {
	for i := 1; i < len(m); i++ {
		cur := m[i]
		j := i - 1
		for j >= 0 && m[j].Score < cur.Score {
			m[j+1] = m[j]
			j--
		}
		m[j+1] = cur
	}
}
