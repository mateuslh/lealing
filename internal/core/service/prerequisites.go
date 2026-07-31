package service

import (
	"context"

	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/port/inbound"
	"github.com/mateuslh/lealing/internal/core/port/outbound"
)

// PrerequisiteService resolve dependências externas antes da abertura de uma
// tool. A regra fica na aplicação para que toda porta de entrada (TUI, CLI ou
// futura API) observe o mesmo comportamento.
type PrerequisiteService struct {
	repo    outbound.ToolRepository
	checker outbound.RequirementChecker
}

var _ inbound.Prerequisites = (*PrerequisiteService)(nil)

// NewPrerequisites monta o caso de uso.
func NewPrerequisites(
	repo outbound.ToolRepository,
	checker outbound.RequirementChecker,
) *PrerequisiteService {
	return &PrerequisiteService{repo: repo, checker: checker}
}

// Missing resolve a tool pelo ID e devolve seus requisitos ausentes.
//
// Checker nil é uma configuração inválida do composition root. Em vez de
// liberar uma tool que falhará no primeiro comando, o serviço devolve todos
// os requisitos declarados como ausentes.
func (s *PrerequisiteService) Missing(
	ctx context.Context,
	id domain.ToolID,
) ([]domain.Requirement, error) {
	tool, err := s.repo.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if len(tool.Requirements) == 0 {
		return nil, nil
	}
	if s.checker == nil {
		return append([]domain.Requirement(nil), tool.Requirements...), nil
	}
	return s.checker.Missing(ctx, tool.Requirements), nil
}
