package usersync

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Manager é a porta de entrada usada pela tela e pela CLI.
type Manager interface {
	Status(ctx context.Context) (Status, error)
	StartLogin(ctx context.Context) (DeviceCode, error)
	CompleteLogin(ctx context.Context, code DeviceCode) (Identity, error)
	Logout(ctx context.Context) error
	Push(ctx context.Context, force bool) (Result, error)
	Pull(ctx context.Context, force bool) (Result, error)
	SetSection(ctx context.Context, section Section, enabled bool) error
}

// Status é o retrato completo para a tela desenhar sem fazer mais perguntas.
type Status struct {
	Connected  bool
	Identity   Identity
	Repository string
	Selection  Selection

	Local State
	// Remote só vem preenchido quando a consulta funcionou; RemoteErr carrega
	// a falha sem impedir o resto de aparecer.
	Remote        State
	RemoteMissing bool
	RemoteErr     error
	// Diverged indica que o remoto mudou desde a última sincronização desta
	// máquina — o sinal de que um envio vai sobrescrever trabalho alheio.
	Diverged bool
	LastSync time.Time
}

// Result descreve o que uma sincronização fez.
type Result struct {
	Applied  Applied
	Revision string
	State    State
}

type Config struct {
	Auth     Authenticator
	Tokens   TokenStore
	Remote   Remote
	Local    Local
	Settings SettingsStore
	Now      func() time.Time
	// Device identifica esta máquina no documento; Engine, a versão que
	// escreveu. Ambos existem para que a tela do outro lado saiba de onde
	// veio o que está prestes a sobrescrever.
	Device string
	Engine string
}

type Service struct{ config Config }

var _ Manager = (*Service)(nil)

func NewService(config Config) *Service {
	if config.Now == nil {
		config.Now = time.Now
	}
	return &Service{config: config}
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	settings, err := s.config.Settings.Load(ctx)
	if err != nil {
		return Status{}, err
	}
	status := Status{
		Identity:   settings.Identity,
		Repository: repositoryName(settings),
		Selection:  settings.Selection(),
		LastSync:   settings.LastSync,
	}
	if status.Selection.Empty() {
		status.Selection = DefaultSelection()
	}

	local, err := s.config.Local.Collect(ctx)
	if err != nil {
		return status, err
	}
	status.Local = local.Filter(status.Selection)

	credential, err := s.credential(ctx)
	if errors.Is(err, ErrNotAuthenticated) {
		return status, nil
	}
	if err != nil {
		return status, err
	}
	status.Connected = true

	snapshot, err := s.config.Remote.Read(ctx, credential, status.Repository)
	if err != nil {
		// Sem rede o painel ainda serve: mostra o que há na máquina e diz por
		// que não sabe o que há do outro lado.
		status.RemoteErr = err
		return status, nil
	}
	status.Remote = snapshot.State.Filter(status.Selection)
	status.RemoteMissing = snapshot.Missing
	status.Diverged = !snapshot.Missing && settings.Revision != "" && snapshot.Revision != settings.Revision
	return status, nil
}

func (s *Service) StartLogin(ctx context.Context) (DeviceCode, error) {
	if s.config.Auth == nil {
		return DeviceCode{}, errors.New("sincronização não configurada")
	}
	return s.config.Auth.RequestDevice(ctx)
}

// CompleteLogin espera a aprovação, guarda o token no cofre e prepara o
// repositório. Só devolve sucesso quando as três coisas deram certo: um
// login que não consegue criar o destino não é um login útil.
func (s *Service) CompleteLogin(ctx context.Context, code DeviceCode) (Identity, error) {
	credential, err := s.config.Auth.Wait(ctx, code)
	if err != nil {
		return Identity{}, err
	}
	identity, err := s.config.Auth.Identity(ctx, credential)
	if err != nil {
		return Identity{}, err
	}
	if err := s.config.Tokens.Save(ctx, credential); err != nil {
		return Identity{}, fmt.Errorf("guardar a credencial: %w", err)
	}

	settings, err := s.config.Settings.Load(ctx)
	if err != nil {
		return Identity{}, err
	}
	settings.Identity = identity
	if settings.Repository == "" {
		settings.Repository = DefaultRepository
	}
	if len(settings.Sections) == 0 {
		settings = settings.WithSelection(DefaultSelection())
	}
	if _, err := s.config.Remote.Ensure(ctx, credential, settings.Repository); err != nil {
		return identity, fmt.Errorf("preparar o repositório %s: %w", settings.Repository, err)
	}
	if err := s.config.Settings.Save(ctx, settings); err != nil {
		return identity, err
	}
	return identity, nil
}

// Logout esquece a credencial desta máquina. O token segue válido no GitHub
// até ser revogado lá — dizer isso é responsabilidade da tela.
func (s *Service) Logout(ctx context.Context) error {
	if err := s.config.Tokens.Delete(ctx); err != nil {
		return err
	}
	settings, err := s.config.Settings.Load(ctx)
	if err != nil {
		return err
	}
	settings.Identity = Identity{}
	settings.Revision = ""
	return s.config.Settings.Save(ctx, settings)
}

// Push publica o estado desta máquina.
//
// Sem force, a escrita é recusada quando o remoto avançou desde a última
// sincronização daqui: o outro lado pode ter favoritado algo que este
// documento não conhece, e sobrescrever seria descartar isso em silêncio.
func (s *Service) Push(ctx context.Context, force bool) (Result, error) {
	credential, settings, err := s.session(ctx)
	if err != nil {
		return Result{}, err
	}
	selection := settings.Selection()
	if selection.Empty() {
		return Result{}, errors.New("nenhuma seção habilitada para sincronizar")
	}

	local, err := s.config.Local.Collect(ctx)
	if err != nil {
		return Result{}, err
	}
	document := local.Filter(selection)
	document.Version = StateVersion
	document.UpdatedAt = s.config.Now().UTC()
	document.Device = s.config.Device
	document.Engine = s.config.Engine
	document.Normalize()

	expected := settings.Revision
	if force {
		// A revisão vazia manda o adapter escrever por cima do que houver.
		expected = ""
	}
	revision, err := s.config.Remote.Write(ctx, credential, repositoryName(settings), document, expected)
	if err != nil {
		return Result{}, err
	}

	settings.Revision = revision
	settings.LastSync = s.config.Now().UTC()
	if err := s.config.Settings.Save(ctx, settings); err != nil {
		return Result{}, err
	}
	return Result{Revision: revision, State: document}, nil
}

// Pull traz o estado remoto para esta máquina.
func (s *Service) Pull(ctx context.Context, force bool) (Result, error) {
	credential, settings, err := s.session(ctx)
	if err != nil {
		return Result{}, err
	}
	selection := settings.Selection()
	if selection.Empty() {
		return Result{}, errors.New("nenhuma seção habilitada para sincronizar")
	}

	snapshot, err := s.config.Remote.Read(ctx, credential, repositoryName(settings))
	if err != nil {
		return Result{}, err
	}
	if snapshot.Missing {
		return Result{}, ErrNoRemote
	}
	if err := snapshot.State.Validate(); err != nil {
		return Result{}, err
	}
	// Sem force, baixar por cima de mudanças locais ainda não enviadas é
	// recusado pelo mesmo motivo do push: o lado perdido não volta.
	if !force && settings.Revision != "" && snapshot.Revision == settings.Revision {
		if changed, err := s.localChanged(ctx, selection, snapshot.State); err != nil {
			return Result{}, err
		} else if changed {
			return Result{}, ErrConflict
		}
	}

	applied, err := s.config.Local.Apply(ctx, snapshot.State, selection)
	if err != nil {
		return Result{}, err
	}
	settings.Revision = snapshot.Revision
	settings.LastSync = s.config.Now().UTC()
	if err := s.config.Settings.Save(ctx, settings); err != nil {
		return Result{}, err
	}
	return Result{Applied: applied, Revision: snapshot.Revision, State: snapshot.State}, nil
}

// localChanged compara o que há na máquina com o documento remoto já
// aplicado antes. Serve para não avisar de conflito quando o usuário só
// quer refazer um pull idempotente.
func (s *Service) localChanged(ctx context.Context, selection Selection, remote State) (bool, error) {
	local, err := s.config.Local.Collect(ctx)
	if err != nil {
		return false, err
	}
	left, right := local.Filter(selection), remote.Filter(selection)
	left.Normalize()
	right.Normalize()
	return !equalContent(left, right), nil
}

func (s *Service) SetSection(ctx context.Context, section Section, enabled bool) error {
	if !section.Valid() {
		return fmt.Errorf("seção desconhecida: %q", section)
	}
	settings, err := s.config.Settings.Load(ctx)
	if err != nil {
		return err
	}
	selection := settings.Selection()
	if selection == nil {
		selection = Selection{}
	}
	selection[section] = enabled
	return s.config.Settings.Save(ctx, settings.WithSelection(selection))
}

// session reúne credencial e ajustes, que toda operação remota precisa.
func (s *Service) session(ctx context.Context) (Credential, Settings, error) {
	credential, err := s.credential(ctx)
	if err != nil {
		return Credential{}, Settings{}, err
	}
	settings, err := s.config.Settings.Load(ctx)
	if err != nil {
		return Credential{}, Settings{}, err
	}
	return credential, settings, nil
}

func (s *Service) credential(ctx context.Context) (Credential, error) {
	if s.config.Tokens == nil || s.config.Remote == nil || s.config.Local == nil || s.config.Settings == nil {
		return Credential{}, errors.New("sincronização não configurada")
	}
	credential, err := s.config.Tokens.Load(ctx)
	if err != nil {
		return Credential{}, err
	}
	if credential.Empty() {
		return Credential{}, ErrNotAuthenticated
	}
	return credential, nil
}

func repositoryName(settings Settings) string {
	if name := strings.TrimSpace(settings.Repository); name != "" {
		return name
	}
	return DefaultRepository
}

// equalContent compara só o conteúdo sincronizável: carimbo de tempo,
// máquina e versão da engine mudam a cada envio e não significam divergência.
func equalContent(a, b State) bool {
	if len(a.Usage) != len(b.Usage) || len(a.Sources) != len(b.Sources) || len(a.Tools) != len(b.Tools) {
		return false
	}
	for index := range a.Usage {
		left, right := a.Usage[index], b.Usage[index]
		if left.ID != right.ID || left.Runs != right.Runs || left.Favorite != right.Favorite {
			return false
		}
	}
	for index := range a.Sources {
		if a.Sources[index] != b.Sources[index] {
			return false
		}
	}
	if len(a.DisabledBuiltins) != len(b.DisabledBuiltins) {
		return false
	}
	for index := range a.DisabledBuiltins {
		if a.DisabledBuiltins[index] != b.DisabledBuiltins[index] {
			return false
		}
	}
	for index := range a.Tools {
		if a.Tools[index] != b.Tools[index] {
			return false
		}
	}
	return true
}
