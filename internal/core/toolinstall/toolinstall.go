// Package toolinstall contém os casos de uso de instalação e rollback de
// tools externas. O core só descreve operações; disco, manifest e troca
// atômica pertencem à porta Store.
package toolinstall

import (
	"context"
	"encoding/hex"
	"errors"
	"regexp"
	"strings"
)

var ErrInvalidChecksum = errors.New("checksum SHA-256 inválido")

var validHost = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

type InstallRequest struct {
	// Host é o nome estável da origem do marketplace. Instalações feitas de
	// um diretório local usam "local".
	Host           string
	SourceDir      string
	ExpectedSHA256 string
	// ExpectedID e ExpectedVersion ligam um pacote remoto à entrada escolhida
	// no índice antes de qualquer diretório ativo ser alterado.
	ExpectedID       string
	ExpectedVersion  string
	ExpectedManifest *ManifestExpectation
}

// ManifestExpectation fixa os campos exibidos e negociados pelo marketplace.
// O adapter de instalação compara estes valores com o manifest empacotado
// antes de criar ou trocar qualquer versão ativa.
type ManifestExpectation struct {
	ID, Version, Name, Summary, Detail, Category, Risk, Glyph string
	ProtocolMin, ProtocolMax                                  int
	FilesystemRead, FilesystemWrite                           []string
	Network, Subprocess                                       bool
}

type Installation struct {
	Host            string
	ID              string
	Version         string
	PreviousVersion string
	SHA256          string
	Path            string
}

type Installed struct {
	Host            string
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
	request.Host = strings.TrimSpace(request.Host)
	if request.Host == "" {
		request.Host = "local"
	}
	if !validHost.MatchString(request.Host) {
		return Installation{}, errors.New("host da tool é inválido")
	}
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
