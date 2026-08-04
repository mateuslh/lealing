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
	// PermissionsAccepted sinaliza que o usuário já viu e aprovou a
	// ampliação de permissão desta atualização (ver PermissionEscalationError).
	// Sem instalação anterior, ou sem ampliação nenhuma, esta flag não faz
	// diferença.
	PermissionsAccepted bool
}

// ToolPermissions é o subconjunto do manifest relevante para decidir se uma
// atualização amplia o que uma tool pode fazer.
type ToolPermissions struct {
	ReadPaths  []string
	WritePaths []string
	Network    bool
	Subprocess bool
	// WorkingDir é "", "read" ou "write".
	WorkingDir string
}

// Empty reporta se nenhuma permissão está presente.
func (p ToolPermissions) Empty() bool {
	return len(p.ReadPaths) == 0 && len(p.WritePaths) == 0 &&
		!p.Network && !p.Subprocess && p.WorkingDir == ""
}

// PermissionsAdded calcula só o que newPermissions tem e oldPermissions não
// tinha. Remoção de permissão nunca conta como escalada — só o que uma tool
// passa a poder fazer que antes não podia importa para a aprovação do
// usuário.
func PermissionsAdded(oldPermissions, newPermissions ToolPermissions) ToolPermissions {
	return ToolPermissions{
		ReadPaths:  pathsAdded(oldPermissions.ReadPaths, newPermissions.ReadPaths),
		WritePaths: pathsAdded(oldPermissions.WritePaths, newPermissions.WritePaths),
		Network:    !oldPermissions.Network && newPermissions.Network,
		Subprocess: !oldPermissions.Subprocess && newPermissions.Subprocess,
		WorkingDir: workingDirAdded(oldPermissions.WorkingDir, newPermissions.WorkingDir),
	}
}

func pathsAdded(oldPaths, newPaths []string) []string {
	existing := make(map[string]bool, len(oldPaths))
	for _, path := range oldPaths {
		existing[path] = true
	}
	var added []string
	for _, path := range newPaths {
		if !existing[path] {
			added = append(added, path)
		}
	}
	return added
}

// workingDirRank ordena "" < "read" < "write": subir de nível também é
// escalada, mesmo quando o campo já não estava vazio.
func workingDirRank(value string) int {
	switch value {
	case "write":
		return 2
	case "read":
		return 1
	default:
		return 0
	}
}

func workingDirAdded(oldValue, newValue string) string {
	if workingDirRank(newValue) > workingDirRank(oldValue) {
		return newValue
	}
	return ""
}

func permissionsFromExpectation(expectation ManifestExpectation) ToolPermissions {
	return ToolPermissions{
		ReadPaths:  expectation.FilesystemRead,
		WritePaths: expectation.FilesystemWrite,
		Network:    expectation.Network,
		Subprocess: expectation.Subprocess,
		WorkingDir: expectation.WorkingDir,
	}
}

// ToolPermissionEscalation descreve, para uma tool já instalada, as
// permissões que a versão nova pede além da versão ativa.
type ToolPermissionEscalation struct {
	ID    string
	Added ToolPermissions
}

// PermissionEscalationError bloqueia uma instalação ou reconciliação porque
// ao menos uma tool pede permissão que sua versão ativa não tinha. A
// operação inteira não é aplicada quando este erro é devolvido — nada muda
// em disco. Quem chamou mostra Escalations ao usuário e, só depois de
// aprovação explícita, repete a mesma operação com PermissionsAccepted (ou
// o equivalente de mais alto nível) true.
type PermissionEscalationError struct {
	Escalations []ToolPermissionEscalation
}

func (e *PermissionEscalationError) Error() string {
	ids := make([]string, len(e.Escalations))
	for i, escalation := range e.Escalations {
		ids[i] = escalation.ID
	}
	return fmt.Sprintf("permissão ampliada sem aprovação do usuário: %s", strings.Join(ids, ", "))
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
	// ActivePermissions devolve as permissões declaradas pela versão
	// atualmente ativa de uma tool, sem executá-la. installed é falso e o
	// erro é nil quando a tool simplesmente ainda não está instalada; um
	// manifest ativo corrompido ou ilegível é erro, nunca "sem permissão".
	ActivePermissions(ctx context.Context, id string) (permissions ToolPermissions, installed bool, err error)
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
	if err := s.checkPermissionEscalations(ctx, []InstallRequest{request}); err != nil {
		return Installation{}, err
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
	if err := s.checkPermissionEscalations(ctx, requests); err != nil {
		return Reconciliation{}, err
	}
	return store.Reconcile(ctx, requests)
}

// checkPermissionEscalations verifica, para cada request com manifest
// esperado conhecido, se a tool já está instalada com permissões menores do
// que as pedidas agora. Nenhuma mutação acontece enquanto esta função não
// devolve nil: descoberta não executa, e aqui vale o mesmo princípio — a
// verificação de permissão nunca troca a versão ativa por conta própria.
func (s *Service) checkPermissionEscalations(ctx context.Context, requests []InstallRequest) error {
	var escalations []ToolPermissionEscalation
	for _, request := range requests {
		if request.PermissionsAccepted || request.ExpectedID == "" || request.ExpectedManifest == nil {
			continue
		}
		escalation, err := s.escalationFor(ctx, request)
		if err != nil {
			return err
		}
		if escalation != nil {
			escalations = append(escalations, *escalation)
		}
	}
	if len(escalations) == 0 {
		return nil
	}
	return &PermissionEscalationError{Escalations: escalations}
}

func (s *Service) escalationFor(ctx context.Context, request InstallRequest) (*ToolPermissionEscalation, error) {
	activePermissions, installed, err := s.store.ActivePermissions(ctx, request.ExpectedID)
	if err != nil {
		return nil, fmt.Errorf("consultar permissões ativas de %s: %w", request.ExpectedID, err)
	}
	if !installed {
		return nil, nil
	}
	added := PermissionsAdded(activePermissions, permissionsFromExpectation(*request.ExpectedManifest))
	if added.Empty() {
		return nil, nil
	}
	return &ToolPermissionEscalation{ID: request.ExpectedID, Added: added}, nil
}

func (s *Service) Restore(ctx context.Context, reconciliation Reconciliation) error {
	store, ok := s.store.(ReconcileStore)
	if !ok {
		return errors.New("instalador não oferece restauração atômica")
	}
	return store.Restore(ctx, reconciliation)
}
