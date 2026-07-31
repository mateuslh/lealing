// Package runner implementa a porta ToolRunner com as estratégias de
// execução suportadas pelo lealing.
package runner

import (
	"context"
	"errors"
	"os/exec"

	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/port/outbound"
)

// CommandResolver traduz uma tool + args na linha de comando concreta.
// É injetável para que o runner permaneça testável sem tocar em processos
// reais, e para que providers definam a própria convenção de argumentos.
type CommandResolver func(t domain.Tool, args domain.Args) (name string, argv []string, err error)

// Process implementa outbound.ToolRunner para KindProcess e KindScript.
type Process struct {
	resolve CommandResolver
	log     outbound.Logger
}

var _ outbound.ToolRunner = (*Process)(nil)

// NewProcess monta o runner de processos externos.
func NewProcess(resolve CommandResolver, log outbound.Logger) *Process {
	return &Process{resolve: resolve, log: log}
}

// Supports implementa outbound.ToolRunner.
func (p *Process) Supports(kind domain.Kind) bool {
	return kind == domain.KindProcess || kind == domain.KindScript
}

// Run implementa outbound.ToolRunner.
//
// O canal é bufferizado com folga para as três transições possíveis, de modo
// que a goroutine nunca bloqueia caso ninguém esteja lendo — a TUI pode ter
// mudado de tela no meio da execução.
func (p *Process) Run(ctx context.Context, t domain.Tool, args domain.Args) (<-chan domain.Session, error) {
	if p.resolve == nil {
		return nil, errors.New("runner de processo sem CommandResolver")
	}
	name, argv, err := p.resolve(t, args)
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, name, argv...)
	updates := make(chan domain.Session, 3)

	if err := cmd.Start(); err != nil {
		return nil, err
	}
	updates <- domain.Session{ToolID: t.ID, Phase: domain.PhaseRunning}

	go func() {
		defer close(updates)

		err := cmd.Wait()
		final := domain.Session{ToolID: t.ID, Phase: domain.PhaseSucceeded}

		switch {
		case err == nil:
		case ctx.Err() != nil:
			final.Phase = domain.PhaseCanceled
			final.Err = ctx.Err()
		default:
			final.Phase = domain.PhaseFailed
			final.Err = err
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				final.ExitCode = exitErr.ExitCode()
			}
		}

		if p.log != nil {
			p.log.Debug("processo finalizado", "tool", t.ID, "fase", final.Phase, "exit", final.ExitCode)
		}
		updates <- final
	}()

	return updates, nil
}
