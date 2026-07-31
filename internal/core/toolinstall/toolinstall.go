// Package toolinstall contém os casos de uso de instalação e rollback de
// tools externas. O core só descreve operações; disco, manifest e troca
// atômica pertencem à porta Store.
package toolinstall

import (
	"context"
	"encoding/hex"
	"errors"
	"strings"
)

var ErrInvalidChecksum = errors.New("checksum SHA-256 inválido")

type InstallRequest struct {
	SourceDir      string
	ExpectedSHA256 string
}

type Installation struct {
	ID              string
	Version         string
	PreviousVersion string
	SHA256          string
	Path            string
}

type Installed struct {
	ID              string
	ActiveVersion   string
	PreviousVersion string
}

type Removal struct {
	ID          string
	RecoveryDir string
}

// Store é a porta de saída que realiza as mutações recuperáveis no diretório
// de tools.
type Store interface {
	Install(ctx context.Context, request InstallRequest) (Installation, error)
	List(ctx context.Context) ([]Installed, error)
	Rollback(ctx context.Context, id string) (Installation, error)
	Remove(ctx context.Context, id string) (Removal, error)
}

// Manager é a porta de entrada consumida pela CLI e, futuramente, pelo
// marketplace da TUI.
type Manager interface {
	InstallLocal(ctx context.Context, request InstallRequest) (Installation, error)
	ListInstalled(ctx context.Context) ([]Installed, error)
	Rollback(ctx context.Context, id string) (Installation, error)
	Remove(ctx context.Context, id string) (Removal, error)
}

type Service struct{ store Store }

var _ Manager = (*Service)(nil)

func NewService(store Store) *Service { return &Service{store: store} }

func (s *Service) InstallLocal(ctx context.Context, request InstallRequest) (Installation, error) {
	request.ExpectedSHA256 = strings.ToLower(strings.TrimSpace(request.ExpectedSHA256))
	if request.ExpectedSHA256 != "" {
		decoded, err := hex.DecodeString(request.ExpectedSHA256)
		if err != nil || len(decoded) != 32 {
			return Installation{}, ErrInvalidChecksum
		}
	}
	return s.store.Install(ctx, request)
}

func (s *Service) ListInstalled(ctx context.Context) ([]Installed, error) {
	return s.store.List(ctx)
}

func (s *Service) Rollback(ctx context.Context, id string) (Installation, error) {
	if strings.TrimSpace(id) == "" {
		return Installation{}, errors.New("ID da tool é obrigatório")
	}
	return s.store.Rollback(ctx, id)
}

func (s *Service) Remove(ctx context.Context, id string) (Removal, error) {
	if strings.TrimSpace(id) == "" {
		return Removal{}, errors.New("ID da tool é obrigatório")
	}
	return s.store.Remove(ctx, id)
}
