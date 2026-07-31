package bootstrap

import (
	"context"
	"fmt"
	"sort"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/port/outbound"
)

// validateWiring falha no arranque quando catálogo e composição divergem.
//
// Sem esta validação uma tool nativa aparece normalmente, mas só denuncia a
// factory esquecida quando o usuário aperta Enter; o mesmo vale para uma
// tool de processo sem runner. O composition root possui todos os lados e é
// o único lugar capaz de conferir o contrato completo.
func validateWiring(
	ctx context.Context,
	repo outbound.ToolRepository,
	screens tui.Screens,
	runners []outbound.ToolRunner,
) error {
	tools, err := repo.All(ctx)
	if err != nil {
		return fmt.Errorf("validar composição: %w", err)
	}

	byID := make(map[domain.ToolID]domain.Tool, len(tools))
	for _, tool := range tools {
		byID[tool.ID] = tool
		switch {
		case tool.Kind == domain.KindBuiltin && !screens.Has(tool.ID):
			return fmt.Errorf("tool nativa %q sem factory de tela", tool.ID)
		case tool.Kind != domain.KindBuiltin && !runnerSupports(runners, tool.Kind):
			return fmt.Errorf("tool %q de kind %s sem runner", tool.ID, tool.Kind)
		}
	}

	ids := make([]domain.ToolID, 0, len(screens))
	for id := range screens {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })

	for _, id := range ids {
		tool, ok := byID[id]
		switch {
		case !ok:
			return fmt.Errorf("factory de tela %q sem tool no catálogo", id)
		case tool.Kind != domain.KindBuiltin:
			return fmt.Errorf("factory de tela %q ligada a kind %s", id, tool.Kind)
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
