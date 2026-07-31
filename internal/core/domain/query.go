package domain

import "strings"

// SortBy define o critério de ordenação do catálogo.
type SortBy uint8

const (
	// SortRelevance usa o score do Searcher; sem termo de busca, cai para
	// SortSmart automaticamente.
	SortRelevance SortBy = iota
	// SortSmart ordena por uso (favoritas, frequência, recência).
	SortSmart
	// SortAlpha ordena pelo nome exibido.
	SortAlpha
	// SortRecent ordena pela última execução.
	SortRecent
)

// Query descreve uma consulta ao catálogo. É um value object: sempre criado
// completo, nunca mutado por baixo dos panos.
//
// A paginação é obrigatória por design — o catálogo assume centenas de tools
// e a TUI nunca materializa a lista inteira.
type Query struct {
	// Term é texto livre. Suporta filtros inline: "tag:git kind:process foo".
	Term       string
	Categories []CategoryID
	Tags       []Tag
	Kinds      []Kind
	// MaxRisk limita o risco exibido; zero-value RiskSafe seria restritivo
	// demais, então usamos um ponteiro para distinguir "sem filtro".
	MaxRisk       *Risk
	FavoritesOnly bool
	IncludeHidden bool
	Sort          SortBy
	Offset, Limit int
}

// DefaultPageSize é o tamanho de página quando a Query não define Limit.
const DefaultPageSize = 100

// Normalize devolve uma cópia saneada: limites válidos, termo aparado e
// filtros inline extraídos para os campos estruturados.
//
// Manter o parsing aqui — e não no adapter de TUI — garante que qualquer
// driving adapter (CLI, RPC, testes) aceite a mesma sintaxe de busca.
func (q Query) Normalize() Query {
	out := q
	out.Term = ""

	for _, field := range strings.Fields(q.Term) {
		key, value, ok := strings.Cut(field, ":")
		if !ok || value == "" {
			out.Term += field + " "
			continue
		}
		switch strings.ToLower(key) {
		case "tag", "t":
			out.Tags = append(out.Tags, Tag(strings.ToLower(value)))
		case "cat", "category", "c":
			out.Categories = append(out.Categories, CategoryID(strings.ToLower(value)))
		case "kind", "k":
			if kind, ok := ParseKind(value); ok {
				out.Kinds = append(out.Kinds, kind)
			}
		case "is":
			if strings.EqualFold(value, "fav") || strings.EqualFold(value, "favorite") {
				out.FavoritesOnly = true
			}
		default:
			out.Term += field + " "
		}
	}

	out.Term = strings.TrimSpace(out.Term)
	if out.Limit <= 0 {
		out.Limit = DefaultPageSize
	}
	if out.Offset < 0 {
		out.Offset = 0
	}
	if out.Term == "" && out.Sort == SortRelevance {
		out.Sort = SortSmart
	}
	return out
}

// ParseKind converte o nome textual de um Kind. O bool segue a convenção do
// comma-ok: false quando o valor é desconhecido.
func ParseKind(s string) (Kind, bool) {
	for i, name := range kindNames {
		if strings.EqualFold(s, name) {
			return Kind(i), true
		}
	}
	return 0, false
}

// Matches aplica os filtros estruturados (tudo menos o texto livre, que é
// responsabilidade do Searcher).
func (q Query) Matches(t Tool, u Usage) bool {
	if len(q.Categories) > 0 && !containsCategory(q.Categories, t.Category) {
		return false
	}
	if len(q.Kinds) > 0 && !containsKind(q.Kinds, t.Kind) {
		return false
	}
	if q.MaxRisk != nil && t.Risk > *q.MaxRisk {
		return false
	}
	if q.FavoritesOnly && !u.Favorite {
		return false
	}
	for _, tag := range q.Tags {
		if !t.HasTag(tag) {
			return false
		}
	}
	return true
}

func containsCategory(hay []CategoryID, needle CategoryID) bool {
	for _, c := range hay {
		if c == needle {
			return true
		}
	}
	return false
}

func containsKind(hay []Kind, needle Kind) bool {
	for _, k := range hay {
		if k == needle {
			return true
		}
	}
	return false
}

// Match é uma tool já resolvida contra uma consulta: carrega o score, o uso
// e os índices de caracteres que casaram, para o realce na lista.
type Match struct {
	Tool      Tool
	Usage     Usage
	Score     float64
	Positions []int
}

// Page é uma fatia do catálogo mais o total real, para que a TUI saiba
// desenhar o scrollbar sem carregar o resto.
type Page struct {
	Items  []Match
	Total  int
	Offset int
}

// Len devolve a quantidade de itens materializados nesta página.
func (p Page) Len() int { return len(p.Items) }

// HasMore indica se existem itens além do fim desta página.
func (p Page) HasMore() bool { return p.Offset+len(p.Items) < p.Total }
