package bootstrap

import (
	"context"
	"fmt"

	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/port/outbound"
)

// validateWiring falha no arranque quando catálogo e composição divergem.
//
// Manifests interativos usam a tela genérica. As outras execuções precisam de
// um runner para o Kind declarado; o composition root é o único lugar que
// possui os dois lados e pode conferir esse contrato antes de abrir a TUI.
func validateWiring(
	ctx context.Context,
	repo outbound.ToolRepository,
	runners []outbound.ToolRunner,
) error {
	tools, err := repo.All(ctx)
	if err != nil {
		return fmt.Errorf("validar composição: %w", err)
	}

	for _, tool := range tools {
		switch {
		case tool.Interactive():
			// Todas as screen-v1 usam a mesma factory genérica criada pela home.
		case !runnerSupports(runners, tool.Kind):
			return fmt.Errorf("tool %q de kind %s sem runner", tool.ID, tool.Kind)
		}
	}
	return nil
}

func runnerSupports(runners []outbound.ToolRunner, kind domain.Kind) bool {
	for _, runner := range runners {
		if runner != nil && runner.Supports(kind) {
			return true
		}
	}
	return false
}
