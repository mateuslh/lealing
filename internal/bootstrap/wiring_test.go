package bootstrap

import (
	"context"
	"strings"
	"testing"

	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/port/outbound"
)

type wiringRepo struct{ tools []domain.Tool }

func (r wiringRepo) All(context.Context) ([]domain.Tool, error) { return r.tools, nil }
func (r wiringRepo) ByID(_ context.Context, id domain.ToolID) (domain.Tool, error) {
	for _, tool := range r.tools {
		if tool.ID == id {
			return tool, nil
		}
	}
	return domain.Tool{}, domain.ErrToolNotFound
}
func (wiringRepo) Categories(context.Context) ([]domain.Category, error) { return nil, nil }

type wiringRunner struct{ kind domain.Kind }

func (r wiringRunner) Supports(kind domain.Kind) bool { return kind == r.kind }
func (wiringRunner) Run(
	context.Context,
	domain.Tool,
	domain.Args,
) (<-chan domain.Session, error) {
	return nil, nil
}

func TestValidateWiringAceitaLigacaoCompleta(t *testing.T) {
	repo := wiringRepo{tools: []domain.Tool{
		{ID: "example-tool", Kind: domain.KindProcess},
		{ID: "another-tool", Kind: domain.KindProcess, Runtime: &domain.ExternalRuntime{UIMode: "screen-v1"}},
	}}
	runners := []outbound.ToolRunner{wiringRunner{kind: domain.KindProcess}}

	if err := validateWiring(context.Background(), repo, runners); err != nil {
		t.Fatalf("validateWiring: %v", err)
	}
}

func TestValidateWiringRecusaProcessoSemRunner(t *testing.T) {
	repo := wiringRepo{tools: []domain.Tool{{ID: "process", Kind: domain.KindProcess}}}

	err := validateWiring(context.Background(), repo, nil)
	if err == nil || !strings.Contains(err.Error(), "runner") {
		t.Fatalf("erro = %v, quero runner ausente", err)
	}
}

func TestValidateWiringAceitaScreenV1SemFactoryEspecifica(t *testing.T) {
	repo := wiringRepo{tools: []domain.Tool{{
		ID: "external", Kind: domain.KindProcess,
		Runtime: &domain.ExternalRuntime{UIMode: "screen-v1"},
	}}}

	if err := validateWiring(context.Background(), repo, nil); err != nil {
		t.Fatalf("screen-v1 deveria usar a tela genérica: %v", err)
	}
}
