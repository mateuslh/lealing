// Package catalog declara somente a taxonomia aceita pela engine.
//
// Tools são extensões instaladas e descobertas por manifest. A engine não
// publica mais tools padrão nem reserva IDs para implementações internas.
package catalog

import (
	"context"

	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/port/outbound"
)

// Categorias do acervo. A ordem de declaração é a ordem na sidebar.
var (
	System      = domain.Category{ID: "system", Name: "Sistema", Glyph: "⌬", Accent: 0, Order: 10, Description: "a máquina local: energia, hardware, diagnóstico"}
	AI          = domain.Category{ID: "ai", Name: "IA", Glyph: "✧", Accent: 1, Order: 20, Description: "consumo, custos e ferramentas de modelos"}
	Network     = domain.Category{ID: "network", Name: "Rede", Glyph: "⇄", Accent: 2, Order: 30, Description: "conectividade e diagnóstico de rede"}
	Media       = domain.Category{ID: "media", Name: "Mídia", Glyph: "◈", Accent: 3, Order: 40, Description: "áudio, vídeo e imagem"}
	Development = domain.Category{ID: "dev", Name: "Desenvolvimento", Glyph: "⚙", Accent: 4, Order: 50, Description: "build, testes e fluxo de trabalho"}
	Utilities   = domain.Category{ID: "utilities", Name: "Utilitários", Glyph: "▤", Accent: 5, Order: 60, Description: "ferramentas de uso geral"}
)

var categories = []domain.Category{System, AI, Network, Media, Development, Utilities}

// taxonomyProvider entrega categorias ao registry sem introduzir tools
// compiladas na engine.
type taxonomyProvider struct{}

var _ outbound.ToolProvider = taxonomyProvider{}

func (taxonomyProvider) Name() string { return "taxonomy" }

func (taxonomyProvider) Provide(context.Context) ([]domain.Tool, []domain.Category, error) {
	return nil, Categories(), nil
}

// Providers devolve apenas a taxonomia. Todas as tools vêm do provider de
// instalações externas ligado pelo bootstrap.
func Providers() []outbound.ToolProvider { return []outbound.ToolProvider{taxonomyProvider{}} }

// Categories devolve uma cópia das categorias aceitas por manifests externos.
func Categories() []domain.Category { return append([]domain.Category(nil), categories...) }

// ReservedIDs é vazio porque não existem mais tools padrão a sombrear.
func ReservedIDs() []domain.ToolID { return nil }
