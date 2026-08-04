// Package toolinstall contém os casos de uso de instalação e rollback de
// tools externas. O core só descreve operações; disco, manifest e troca
// atômica pertencem à porta Store.
package toolinstall

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
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
	WorkingDir                                                string
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

// Reconciliation descreve uma troca declarativa do catálogo instalado.
// RecoveryDir preserva o estado anterior inteiro para rollback caso outra
// parte da mesma operação (como a persistência de origens) falhe depois.
type Reconciliation struct {
	Changed         int
	RecoveryDir     string
	PreviousPresent bool
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

// ReconcileStore troca atomicamente o conjunto instalado pela lista pedida.
// SourceDir vazio reutiliza a versão já instalada indicada por ExpectedID e
// ExpectedVersion; preenchido instala o pacote previamente preparado.
type ReconcileStore interface {
	Reconcile(ctx context.Context, requests []InstallRequest) (Reconciliation, error)
	Restore(ctx context.Context, reconciliation Reconciliation) error
}

// Reconciler é a capacidade opcional usada por operações declarativas. Ela
// fica separada de Manager para que instalações unitárias continuem simples.
type Reconciler interface {
	Reconcile(ctx context.Context, requests []InstallRequest) (Reconciliation, error)
	Restore(ctx context.Context, reconciliation Reconciliation) error
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

func (s *Service) Reconcile(ctx context.Context, requests []InstallRequest) (Reconciliation, error) {
	store, ok := s.store.(ReconcileStore)
	if !ok {
		return Reconciliation{}, errors.New("instalador não oferece reconciliação atômica")
	}
	for index := range requests {
		requests[index].Host = strings.TrimSpace(requests[index].Host)
		requests[index].ExpectedID = strings.TrimSpace(requests[index].ExpectedID)
		requests[index].ExpectedVersion = strings.TrimSpace(requests[index].ExpectedVersion)
		if !validHost.MatchString(requests[index].Host) {
			return Reconciliation{}, fmt.Errorf("host da tool %s é inválido", requests[index].ExpectedID)
		}
		if requests[index].ExpectedID == "" || requests[index].ExpectedVersion == "" {
			return Reconciliation{}, errors.New("reconciliação exige ID e versão esperados")
		}
	}
	return store.Reconcile(ctx, requests)
}

func (s *Service) Restore(ctx context.Context, reconciliation Reconciliation) error {
	store, ok := s.store.(ReconcileStore)
	if !ok {
		return errors.New("instalador não oferece restauração atômica")
	}
	return store.Restore(ctx, reconciliation)
}
