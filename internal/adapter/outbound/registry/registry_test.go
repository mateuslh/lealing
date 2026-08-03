package registry_test

import (
	"context"
	"testing"

	"github.com/mateuslh/lealing/internal/adapter/outbound/registry"
	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/port/outbound"
)

func acervo() []outbound.ToolProvider {
	cats := []domain.Category{{ID: "system", Name: "Sistema"}}
	return []outbound.ToolProvider{&registry.Static{
		Label:      "teste",
		Categories: cats,
		Tools: []domain.Tool{
			{ID: "so-mac", Name: "Só Mac", Category: "system", Platforms: domain.Darwin},
			{ID: "so-win", Name: "Só Win", Category: "system", Platforms: domain.Windows},
			{ID: "ambos", Name: "Ambos", Category: "system", Platforms: domain.Darwin | domain.Windows},
			{ID: "portavel", Name: "Portável", Category: "system"},
		},
	}}
}

func ids(t *testing.T, r *registry.Registry) map[domain.ToolID]bool {
	t.Helper()
	tools, err := r.All(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := make(map[domain.ToolID]bool, len(tools))
	for _, tool := range tools {
		out[tool.ID] = true
	}
	return out
}

func TestPlatformEscondeOQueNaoRodaAqui(t *testing.T) {
	got := ids(t, registry.New(acervo(), registry.WithPlatform(domain.Windows)))

	for _, want := range []domain.ToolID{"so-win", "ambos", "portavel"} {
		if !got[want] {
			t.Errorf("%q sumiu do acervo do Windows", want)
		}
	}
	if got["so-mac"] {
		t.Error("uma tool exclusiva do macOS apareceu no Windows")
	}
}

// Sem o filtro, o acervo é o completo: é o que a documentação e a matriz de
// suporte precisam enxergar, independentemente da máquina que as gera.
func TestSemPlatformOAcervoEInteiro(t *testing.T) {
	got := ids(t, registry.New(acervo()))
	if len(got) != 4 {
		t.Errorf("%d tools sem filtro, quero 4: %v", len(got), got)
	}
}

func TestRegistryPreservaHostPersistidoPeloProviderDeInstalacoes(t *testing.T) {
	providers := acervo()
	static := providers[0].(*registry.Static)
	static.Tools[0].Host = "origem-persistida"
	repository := registry.New(providers)
	tool, err := repository.ByID(context.Background(), "so-mac")
	if err != nil {
		t.Fatal(err)
	}
	want := domain.ToolRef{Host: "origem-persistida", ID: "so-mac"}
	if tool.Ref() != want {
		t.Fatalf("referência = %+v, quero %+v", tool.Ref(), want)
	}
}

func TestRegistryUsaNomeDoProviderQuandoToolNaoInformaHost(t *testing.T) {
	repository := registry.New(acervo())
	tool, err := repository.ByID(context.Background(), "so-mac")
	if err != nil {
		t.Fatal(err)
	}
	if tool.Host != "teste" {
		t.Fatalf("host = %q, quero teste", tool.Host)
	}
}

func TestRegistryRecusaHostDuplicado(t *testing.T) {
	providers := acervo()
	providers = append(providers, &registry.Static{Label: "teste"})
	if _, err := registry.New(providers).All(context.Background()); err == nil {
		t.Fatal("dois providers com o mesmo host foram aceitos")
	}
}

// A tool escondida some de verdade: ByID também precisa recusá-la, senão um
// favorito antigo abriria uma tela sem adapter por trás.
func TestByIDNaoRessuscitaToolDeOutraPlataforma(t *testing.T) {
	r := registry.New(acervo(), registry.WithPlatform(domain.Windows))
	if _, err := r.ByID(context.Background(), "so-mac"); err == nil {
		t.Error("ByID devolveu uma tool que não roda nesta plataforma")
	}
}
