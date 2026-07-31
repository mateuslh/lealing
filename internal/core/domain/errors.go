package domain

import (
	"errors"
	"fmt"
	"strings"
)

// Erros sentinela do domínio. Adapters traduzem estes valores para o
// vocabulário deles (exit code, HTTP status, toast na TUI) via errors.Is.
var (
	// ErrToolNotFound: o ID consultado não existe no catálogo.
	ErrToolNotFound = errors.New("tool não encontrada")
	// ErrCategoryNotFound: a categoria consultada não existe.
	ErrCategoryNotFound = errors.New("categoria não encontrada")
	// ErrDuplicateTool: dois providers declararam o mesmo ToolID.
	ErrDuplicateTool = errors.New("tool duplicada")
	// ErrInvalidTool: a tool não passou na validação estrutural.
	ErrInvalidTool = errors.New("tool inválida")
	// ErrConfirmationRequired: execução barrada por política de risco.
	ErrConfirmationRequired = errors.New("confirmação obrigatória")
	// ErrCanceled: operação interrompida pelo usuário.
	ErrCanceled = errors.New("operação cancelada")
)

// ToolError anexa o ID da tool a um erro sentinela, preservando errors.Is.
type ToolError struct {
	ID  ToolID
	Err error
}

// Error implementa error.
func (e *ToolError) Error() string { return fmt.Sprintf("%s: %v", e.ID, e.Err) }

// Unwrap permite errors.Is/errors.As atravessarem o wrapper.
func (e *ToolError) Unwrap() error { return e.Err }

// WrapTool anexa contexto de tool a um erro. Devolve nil para err nil, de
// modo que o call site não precise de guarda.
func WrapTool(id ToolID, err error) error {
	if err == nil {
		return nil
	}
	return &ToolError{ID: id, Err: err}
}

// ValidationError descreve por que uma tool foi rejeitada no registro.
type ValidationError struct {
	ID     ToolID
	Field  string
	Reason string
}

// Error implementa error.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("tool %q: campo %q %s", e.ID, e.Field, e.Reason)
}

// Unwrap conecta a ErrInvalidTool.
func (e *ValidationError) Unwrap() error { return ErrInvalidTool }

// Validate checa os invariantes mínimos de uma Tool. É chamado pelo registry
// no momento do registro, para que um provider malformado falhe cedo e alto
// em vez de produzir uma linha vazia na lista.
func (t Tool) Validate() error {
	switch {
	case t.ID == "":
		return &ValidationError{ID: t.ID, Field: "ID", Reason: "é obrigatório"}
	case t.Name == "":
		return &ValidationError{ID: t.ID, Field: "Name", Reason: "é obrigatório"}
	case t.Category == "":
		return &ValidationError{ID: t.ID, Field: "Category", Reason: "é obrigatória"}
	case t.Kind > KindRemote:
		return &ValidationError{ID: t.ID, Field: "Kind", Reason: "é desconhecido"}
	case t.Risk > RiskDestructive:
		return &ValidationError{ID: t.ID, Field: "Risk", Reason: "é desconhecido"}
	case !t.Platforms.Valid():
		return &ValidationError{ID: t.ID, Field: "Platforms", Reason: "tem bit desconhecido"}
	}
	seenRequirements := make(map[string]bool, len(t.Requirements))
	for _, requirement := range t.Requirements {
		switch {
		case requirement.Executable == "":
			return &ValidationError{ID: t.ID, Field: "Requirements", Reason: "tem executável vazio"}
		case strings.ContainsAny(requirement.Executable, `/\`+" \t\r\n"):
			return &ValidationError{ID: t.ID, Field: "Requirements", Reason: "deve declarar nome sem caminho ou argumentos"}
		case seenRequirements[requirement.Executable]:
			return &ValidationError{ID: t.ID, Field: "Requirements", Reason: "tem executável duplicado"}
		}
		seenRequirements[requirement.Executable] = true
	}
	return nil
}
