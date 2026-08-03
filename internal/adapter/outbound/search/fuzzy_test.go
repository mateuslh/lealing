package search_test

import (
	"testing"

	"github.com/mateuslh/lealing/internal/adapter/outbound/search"
	"github.com/mateuslh/lealing/internal/core/domain"
)

// TestTermoLiteralGanhaDeSubsequencia trava o comportamento que o score cru do
// fuzzy não garante: quem tem o termo inteiro no corpus vem primeiro.
//
// O termo literal está nas keywords de uma extensão, mas suas letras também
// aparecem espalhadas no resumo de outra. O fuzzy puro penaliza a distância
// até o início e pode colocar a subsequência errada no topo.
func TestTermoLiteralGanhaDeSubsequencia(t *testing.T) {
	tools := []domain.Tool{
		{
			ID:       "another-tool",
			Name:     "Outra extensão",
			Summary:  "Compara medidas passadas e apresenta tendências para o usuário.",
			Keywords: []string{"comparar", "medidas", "tendência"},
		},
		{
			ID:       "example-tool",
			Name:     "Example Tool",
			Summary:  "Extensão usada para validar a relevância da busca.",
			Keywords: []string{"literal", "exato", "fixture"},
		},
	}

	got := search.NewFuzzy().Rank("literal", tools)
	if len(got) == 0 {
		t.Fatal("busca por “literal” não achou nada")
	}
	if got[0].Tool.ID != "example-tool" {
		t.Errorf("primeiro resultado = %s, quero example-tool", got[0].Tool.ID)
	}
}

func TestTermoVazioDevolveTudoNaOrdemOriginal(t *testing.T) {
	tools := []domain.Tool{{ID: "a", Name: "A"}, {ID: "b", Name: "B"}}

	got := search.NewFuzzy().Rank("   ", tools)
	if len(got) != len(tools) {
		t.Fatalf("devolveu %d, quero %d", len(got), len(tools))
	}
	for i, m := range got {
		if m.Tool.ID != tools[i].ID {
			t.Errorf("posição %d = %s, quero %s", i, m.Tool.ID, tools[i].ID)
		}
	}
}

// TestNomeGanhaDeKeyword garante que digitar o nome da tool traz a tool com
// esse nome, mesmo quando outra menciona o termo entre as palavras-chave.
func TestNomeGanhaDeKeyword(t *testing.T) {
	tools := []domain.Tool{
		{ID: "another-tool", Name: "Outra extensão", Keywords: []string{"exemplo"}},
		{ID: "example-tool", Name: "Exemplo", Summary: "Extensão de exemplo."},
	}

	got := search.NewFuzzy().Rank("exemplo", tools)
	if got[0].Tool.ID != "example-tool" {
		t.Errorf("primeiro resultado = %s, quero example-tool", got[0].Tool.ID)
	}
}
