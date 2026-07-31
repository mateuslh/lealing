package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/port/inbound"
	"github.com/mateuslh/lealing/internal/core/service"
)

type runnerStub struct {
	kind  domain.Kind
	calls int
}

func (r *runnerStub) Supports(kind domain.Kind) bool { return kind == r.kind }
func (r *runnerStub) Run(
	_ context.Context,
	tool domain.Tool,
	_ domain.Args,
) (<-chan domain.Session, error) {
	r.calls++
	updates := make(chan domain.Session, 1)
	updates <- domain.Session{ToolID: tool.ID, Phase: domain.PhaseSucceeded}
	close(updates)
	return updates, nil
}

type recorderStub struct{ calls int }

func (r *recorderStub) RecordRun(context.Context, domain.ToolID) error {
	r.calls++
	return nil
}

func TestLauncherAplicaPoliticaDeRiscoAntesDoAdapter(t *testing.T) {
	repo := &fakeRepo{tools: []domain.Tool{{
		ID:   "danger",
		Kind: domain.KindProcess,
		Risk: domain.RiskDestructive,
	}}}
	runner := &runnerStub{kind: domain.KindProcess}
	recorder := &recorderStub{}
	svc := service.NewLauncher(repo, recorder, fixedClock(), nil, runner)

	_, err := svc.Launch(context.Background(), "danger", nil, inbound.LaunchOptions{})
	if !errors.Is(err, domain.ErrConfirmationRequired) {
		t.Fatalf("erro = %v, quero ErrConfirmationRequired", err)
	}
	if runner.calls != 0 || recorder.calls != 0 {
		t.Errorf("runner=%d recorder=%d antes da confirmação", runner.calls, recorder.calls)
	}

	session, err := svc.Launch(context.Background(), "danger", nil, inbound.LaunchOptions{
		Confirmed: true,
	})
	if err != nil {
		t.Fatalf("Launch confirmado: %v", err)
	}
	if session.ToolID != "danger" || runner.calls != 1 || recorder.calls != 1 {
		t.Errorf("session=%+v runner=%d recorder=%d", session, runner.calls, recorder.calls)
	}
}

func TestLauncherRecusaKindSemRunner(t *testing.T) {
	repo := &fakeRepo{tools: []domain.Tool{{ID: "remote", Kind: domain.KindRemote}}}
	svc := service.NewLauncher(repo, nil, fixedClock(), nil)

	if _, err := svc.Launch(
		context.Background(),
		"remote",
		nil,
		inbound.LaunchOptions{},
	); err == nil {
		t.Fatal("Launch = nil, quero erro de runner ausente")
	}
}
