package catalog

import (
	"context"
	"testing"
)

func TestCatalogoDaEngineNaoPublicaToolsPadrao(t *testing.T) {
	providers := Providers()
	if len(providers) != 1 {
		t.Fatalf("providers = %d, esperado somente taxonomia", len(providers))
	}
	tools, gotCategories, err := providers[0].Provide(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 0 {
		t.Fatalf("tools padrão = %d, esperado zero", len(tools))
	}
	if len(gotCategories) != len(Categories()) {
		t.Fatalf("categorias = %d, esperado %d", len(gotCategories), len(Categories()))
	}
	if len(ReservedIDs()) != 0 {
		t.Fatalf("IDs reservados = %v, esperado nenhum", ReservedIDs())
	}
}
