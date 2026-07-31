// Package requirements verifica pré-requisitos externos das tools.
package requirements

import (
	"context"
	"os/exec"

	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/port/outbound"
)

// PathChecker procura executáveis no PATH da aplicação.
type PathChecker struct{}

var _ outbound.RequirementChecker = (*PathChecker)(nil)

// NewPathChecker monta o checador portátil de executáveis.
func NewPathChecker() *PathChecker { return &PathChecker{} }

// Missing implementa outbound.RequirementChecker.
func (*PathChecker) Missing(ctx context.Context, requirements []domain.Requirement) []domain.Requirement {
	missing := make([]domain.Requirement, 0, len(requirements))
	for i, requirement := range requirements {
		select {
		case <-ctx.Done():
			return append(missing, requirements[i:]...)
		default:
		}
		if _, err := exec.LookPath(requirement.Executable); err != nil {
			missing = append(missing, requirement)
		}
	}
	return missing
}
