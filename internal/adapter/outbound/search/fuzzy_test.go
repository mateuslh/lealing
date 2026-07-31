package search_test

import (
	"testing"

	"github.com/mateuslh/lealing/internal/adapter/outbound/search"
	"github.com/mateuslh/lealing/internal/core/domain"
)

// TestTermoLiteralGanhaDeSubsequencia trava o comportamento que o score cru do
// fuzzy não garante: quem tem o termo inteiro no corpus vem primeiro.
//
// O caso é real. "pmset" está nas keywords do controle de energia, mas as
// cinco letras também aparecem, espalhadas, no resumo em português de outras
// tools — e o fuzzy, que penaliza a distância até o início do texto, colocava
// a tool errada no topo conforme o acervo crescia.
func TestTermoLiteralGanhaDeSubsequencia(t *testing.T) {
	tools := []domain.Tool{
		{
			ID:       "self-update",
			Name:     "Atualizar o lealing",
			Summary:  "Compare a versão instalada com o último release e atualize sem sair da TUI.",
			Keywords: []string{"update", "atualizar", "upgrade", "versão", "release"},
		},
		{
			ID:       "power-control",
			Name:     "Controle de Energia",
			Summary:  "Defina se a máquina dorme na bateria e no carregador, e ajustes avançados de energia.",
			Keywords: []string{"pmset", "powercfg", "energia", "bateria", "dormir"},
		},
	}

	got := search.NewFuzzy().Rank("pmset", tools)
	if len(got) == 0 {
		t.Fatal("busca por “pmset” não achou nada")
	}
	if got[0].Tool.ID != "power-control" {
		t.Errorf("primeiro resultado = %s, quero power-control", got[0].Tool.ID)
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
		{ID: "outra", Name: "Uso de Tokens", Keywords: []string{"energia"}},
		{ID: "power-control", Name: "Energia", Summary: "Perfis de energia."},
	}

	got := search.NewFuzzy().Rank("energia", tools)
	if got[0].Tool.ID != "power-control" {
		t.Errorf("primeiro resultado = %s, quero power-control", got[0].Tool.ID)
	}
}
