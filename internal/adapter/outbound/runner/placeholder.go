package runner

import (
	"context"
	"time"

	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/port"
)

// Placeholder aceita qualquer Kind e reporta sucesso sem fazer nada.
//
// Existe porque o catálogo embutido descreve as tools antes de todas terem
// implementação: sem ele, metade da home devolveria "nenhum runner para
// kind X" e o fluxo de navegação ficaria impossível de exercitar. Conforme
// cada tool ganha runner de verdade, ele passa a ser consultado primeiro —
// o LauncherService escolhe o primeiro runner que declara suporte, e o
// Placeholder é sempre registrado por último.
type Placeholder struct {
	log port.Logger
	// Delay simula trabalho, para que a transição de fase seja observável.
	Delay time.Duration
}

var _ port.ToolRunner = (*Placeholder)(nil)

// NewPlaceholder monta o runner de fallback.
func NewPlaceholder(log port.Logger) *Placeholder {
	return &Placeholder{log: log, Delay: 400 * time.Millisecond}
}

// Supports implementa port.ToolRunner: aceita tudo.
func (p *Placeholder) Supports(domain.Kind) bool { return true }

// Run implementa port.ToolRunner.
func (p *Placeholder) Run(ctx context.Context, t domain.Tool, _ domain.Args) (<-chan domain.Session, error) {
	if p.log != nil {
		p.log.Info("execução simulada", "tool", t.ID, "kind", t.Kind)
	}

	updates := make(chan domain.Session, 3)
	updates <- domain.Session{ToolID: t.ID, Phase: domain.PhaseRunning}

	go func() {
		defer close(updates)
		select {
		case <-time.After(p.Delay):
			updates <- domain.Session{ToolID: t.ID, Phase: domain.PhaseSucceeded}
		case <-ctx.Done():
			updates <- domain.Session{ToolID: t.ID, Phase: domain.PhaseCanceled, Err: ctx.Err()}
		}
	}()

	return updates, nil
}
