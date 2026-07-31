// Package runner implementa a porta ToolRunner com as estratégias de
// execução suportadas pelo lealing.
package runner

import (
	"context"
	"errors"
	"os/exec"

	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/port"
)

// CommandResolver traduz uma tool + args na linha de comando concreta.
// É injetável para que o runner permaneça testável sem tocar em processos
// reais, e para que providers definam a própria convenção de argumentos.
type CommandResolver func(t domain.Tool, args domain.Args) (name string, argv []string, err error)

// Process implementa port.ToolRunner para KindProcess e KindScript.
type Process struct {
	resolve CommandResolver
	log     port.Logger
}

var _ port.ToolRunner = (*Process)(nil)

// NewProcess monta o runner de processos externos.
func NewProcess(resolve CommandResolver, log port.Logger) *Process {
	return &Process{resolve: resolve, log: log}
}

// Supports implementa port.ToolRunner.
func (p *Process) Supports(kind domain.Kind) bool {
	return kind == domain.KindProcess || kind == domain.KindScript
}

// Run implementa port.ToolRunner.
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

// BuiltinFunc é a assinatura de uma tool que roda dentro do processo da TUI.
type BuiltinFunc func(ctx context.Context, args domain.Args) error

// Builtin implementa port.ToolRunner para KindBuiltin, despachando por ID.
type Builtin struct {
	handlers map[domain.ToolID]BuiltinFunc
}

var _ port.ToolRunner = (*Builtin)(nil)

// NewBuiltin monta o runner interno.
func NewBuiltin() *Builtin {
	return &Builtin{handlers: make(map[domain.ToolID]BuiltinFunc)}
}

// Register associa um handler a um ToolID.
func (b *Builtin) Register(id domain.ToolID, fn BuiltinFunc) { b.handlers[id] = fn }

// Supports implementa port.ToolRunner.
func (b *Builtin) Supports(kind domain.Kind) bool { return kind == domain.KindBuiltin }

// Run implementa port.ToolRunner.
func (b *Builtin) Run(ctx context.Context, t domain.Tool, args domain.Args) (<-chan domain.Session, error) {
	fn, ok := b.handlers[t.ID]
	if !ok {
		return nil, domain.WrapTool(t.ID, domain.ErrToolNotFound)
	}

	updates := make(chan domain.Session, 3)
	updates <- domain.Session{ToolID: t.ID, Phase: domain.PhaseRunning}

	go func() {
		defer close(updates)
		final := domain.Session{ToolID: t.ID, Phase: domain.PhaseSucceeded}
		if err := fn(ctx, args); err != nil {
			final.Phase = domain.PhaseFailed
			final.Err = err
			if errors.Is(err, context.Canceled) {
				final.Phase = domain.PhaseCanceled
			}
		}
		updates <- final
	}()

	return updates, nil
}
