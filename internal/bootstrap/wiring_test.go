package bootstrap

import (
	"context"
	"strings"
	"testing"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
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

func screenFactory() tui.ScreenFactory { return func() tui.Screen { return nil } }

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
		{ID: "native", Kind: domain.KindBuiltin},
		{ID: "process", Kind: domain.KindProcess},
	}}
	screens := tui.Screens{"native": screenFactory()}
	runners := []outbound.ToolRunner{wiringRunner{kind: domain.KindProcess}}

	if err := validateWiring(context.Background(), repo, screens, runners); err != nil {
		t.Fatalf("validateWiring: %v", err)
	}
}

func TestValidateWiringRecusaToolNativaSemFactory(t *testing.T) {
	repo := wiringRepo{tools: []domain.Tool{{ID: "native", Kind: domain.KindBuiltin}}}

	err := validateWiring(context.Background(), repo, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "native") {
		t.Fatalf("erro = %v, quero factory ausente", err)
	}
}

func TestValidateWiringRecusaFactoryNula(t *testing.T) {
	repo := wiringRepo{tools: []domain.Tool{{ID: "native", Kind: domain.KindBuiltin}}}

	err := validateWiring(
		context.Background(),
		repo,
		tui.Screens{"native": nil},
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "factory") {
		t.Fatalf("erro = %v, quero factory nula", err)
	}
}

func TestValidateWiringRecusaFactoryOrfa(t *testing.T) {
	err := validateWiring(
		context.Background(),
		wiringRepo{},
		tui.Screens{"fantasma": screenFactory()},
		nil,
	)
	if err == nil || !strings.Contains(err.Error(), "fantasma") {
		t.Fatalf("erro = %v, quero factory órfã", err)
	}
}

func TestValidateWiringRecusaProcessoSemRunner(t *testing.T) {
	repo := wiringRepo{tools: []domain.Tool{{ID: "process", Kind: domain.KindProcess}}}

	err := validateWiring(context.Background(), repo, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "runner") {
		t.Fatalf("erro = %v, quero runner ausente", err)
	}
}
