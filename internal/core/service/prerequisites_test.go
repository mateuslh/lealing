package service_test

import (
	"context"
	"testing"

	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/service"
)

type requirementChecker struct {
	got     []domain.Requirement
	missing []domain.Requirement
}

func (c *requirementChecker) Missing(
	_ context.Context,
	requirements []domain.Requirement,
) []domain.Requirement {
	c.got = append([]domain.Requirement(nil), requirements...)
	return append([]domain.Requirement(nil), c.missing...)
}

func TestPrerequisitesResolveToolEConsultaChecker(t *testing.T) {
	requirements := []domain.Requirement{{Executable: "git"}, {Executable: "gh"}}
	repo := &fakeRepo{tools: []domain.Tool{{ID: "clone", Requirements: requirements}}}
	checker := &requirementChecker{missing: requirements[1:]}
	svc := service.NewPrerequisites(repo, checker)

	missing, err := svc.Missing(context.Background(), "clone")
	if err != nil {
		t.Fatalf("Missing: %v", err)
	}
	if len(checker.got) != 2 || len(missing) != 1 || missing[0].Executable != "gh" {
		t.Errorf("checker recebeu %v e devolveu %v", checker.got, missing)
	}
}

func TestPrerequisitesSemCheckerFalhaFechado(t *testing.T) {
	requirements := []domain.Requirement{{Executable: "git"}}
	repo := &fakeRepo{tools: []domain.Tool{{ID: "clone", Requirements: requirements}}}
	svc := service.NewPrerequisites(repo, nil)

	missing, err := svc.Missing(context.Background(), "clone")
	if err != nil {
		t.Fatalf("Missing: %v", err)
	}
	if len(missing) != 1 || missing[0].Executable != "git" {
		t.Errorf("missing = %v, quero todos os requisitos", missing)
	}
}

func TestPrerequisitesRejeitaToolInexistente(t *testing.T) {
	svc := service.NewPrerequisites(&fakeRepo{}, &requirementChecker{})

	if _, err := svc.Missing(context.Background(), "inexistente"); err == nil {
		t.Fatal("Missing = nil, quero ErrToolNotFound")
	}
}
