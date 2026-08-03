// Package toolmanage contém o caso de uso que ativa, desativa e remove tools.
// O catálogo bruto continua completo para validação e manutenção; a visão que
// a home consome esconde apenas as referências que o usuário desligou.
package toolmanage

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/port/outbound"
	"github.com/mateuslh/lealing/internal/core/toolinstall"
)

// State é a preferência persistida. Guardar somente as exceções faz uma tool
// nova nascer habilitada; cada referência precisa trazer provider e ID.
type State struct {
	Disabled []domain.ToolRef `json:"disabled,omitempty"`
}

// Store é a porta de saída da preferência de ativação.
type Store interface {
	Load(ctx context.Context) (State, error)
	Save(ctx context.Context, state State) error
}

// Catalog é a visão completa anterior ao filtro de ativação.
type Catalog interface {
	All(ctx context.Context) ([]domain.Tool, error)
	ByID(ctx context.Context, id domain.ToolID) (domain.Tool, error)
	Categories(ctx context.Context) ([]domain.Category, error)
}

// Reloader recompõe o catálogo bruto depois de uma desinstalação.
type Reloader interface {
	Reload(ctx context.Context) error
}

// Item é a projeção usada pelos adapters de entrada para gerenciar o acervo.
type Item struct {
	Tool            domain.Tool
	Enabled         bool
	Installed       bool
	ActiveVersion   string
	PreviousVersion string
}

// Manager é a porta de entrada consumida pela TUI e pela CLI.
type Manager interface {
	List(ctx context.Context) ([]Item, error)
	SetEnabled(ctx context.Context, id domain.ToolID, enabled bool) error
	Remove(ctx context.Context, id domain.ToolID) (toolinstall.Removal, error)
}

// Service implementa simultaneamente o gerenciamento explícito e o catálogo
// ativo. A validação de wiring recebe o catálogo bruto, enquanto home, busca e
// launcher recebem esta visão filtrada.
type Service struct {
	catalog   Catalog
	store     Store
	installer toolinstall.Manager
	reloader  Reloader

	mu       sync.RWMutex
	loaded   bool
	disabled map[domain.ToolRef]bool
}

var (
	_ Manager                 = (*Service)(nil)
	_ outbound.ToolRepository = (*Service)(nil)
)

func NewService(catalog Catalog, store Store, installer toolinstall.Manager, reloader Reloader) *Service {
	return &Service{
		catalog: catalog, store: store, installer: installer, reloader: reloader,
		disabled: make(map[domain.ToolRef]bool),
	}
}

// All implementa outbound.ToolRepository com o recorte ativo.
func (s *Service) All(ctx context.Context) ([]domain.Tool, error) {
	tools, err := s.catalog.All(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.ensureState(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	active := make([]domain.Tool, 0, len(tools))
	for _, tool := range tools {
		if !s.disabled[tool.Ref()] {
			active = append(active, tool)
		}
	}
	return active, nil
}

// ByID não ressuscita uma tool desativada por favorito ou histórico antigo.
func (s *Service) ByID(ctx context.Context, id domain.ToolID) (domain.Tool, error) {
	tool, err := s.catalog.ByID(ctx, id)
	if err != nil {
		return domain.Tool{}, err
	}
	if err := s.ensureState(ctx); err != nil {
		return domain.Tool{}, err
	}
	s.mu.RLock()
	disabled := s.disabled[tool.Ref()]
	s.mu.RUnlock()
	if disabled {
		return domain.Tool{}, domain.WrapTool(id, domain.ErrToolNotFound)
	}
	return tool, nil
}

// Categories preserva a declaração completa; o caso de uso de catálogo é
// quem conta somente as categorias que ainda possuem tools ativas.
func (s *Service) Categories(ctx context.Context) ([]domain.Category, error) {
	return s.catalog.Categories(ctx)
}

// List combina catálogo e instalações, inclusive as desativadas.
func (s *Service) List(ctx context.Context) ([]Item, error) {
	tools, err := s.catalog.All(ctx)
	if err != nil {
		return nil, err
	}
	installed, err := s.installer.ListInstalled(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.ensureState(ctx); err != nil {
		return nil, err
	}

	versions := make(map[domain.ToolID]toolinstall.Installed, len(installed))
	for _, item := range installed {
		versions[domain.ToolID(item.ID)] = item
	}

	s.mu.RLock()
	items := make([]Item, 0, len(tools)+len(installed))
	seen := make(map[domain.ToolID]bool, len(tools))
	for _, tool := range tools {
		version, external := versions[tool.ID]
		items = append(items, Item{
			Tool: tool, Enabled: !s.disabled[tool.Ref()],
			Installed: external, ActiveVersion: version.ActiveVersion, PreviousVersion: version.PreviousVersion,
		})
		seen[tool.ID] = true
	}
	// Uma instalação com manifest quebrado precisa continuar removível. Ela
	// não entra no catálogo executável, mas não pode ficar presa no disco.
	for id, version := range versions {
		if seen[id] {
			continue
		}
		items = append(items, Item{
			Tool:    domain.Tool{ID: id, Name: string(id), Kind: domain.KindProcess},
			Enabled: true, Installed: true,
			ActiveVersion: version.ActiveVersion, PreviousVersion: version.PreviousVersion,
		})
	}
	s.mu.RUnlock()

	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(items[i].Tool.Title()) < strings.ToLower(items[j].Tool.Title())
	})
	return items, nil
}

// SetEnabled grava primeiro e só então publica o novo estado em memória.
func (s *Service) SetEnabled(ctx context.Context, id domain.ToolID, enabled bool) error {
	tool, err := s.catalog.ByID(ctx, id)
	if err != nil {
		return err
	}
	ref := tool.Ref()
	if strings.TrimSpace(ref.Host) == "" {
		return domain.WrapTool(id, errors.New("provider da tool não informou um host"))
	}
	if err := s.ensureState(ctx); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneSet(s.disabled)
	if enabled {
		delete(next, ref)
	} else {
		next[ref] = true
	}
	if err := s.saveLocked(ctx, next); err != nil {
		return domain.WrapTool(id, err)
	}
	s.disabled = next
	return nil
}

// Remove desinstala o pacote e limpa seu estado de ativação.
func (s *Service) Remove(ctx context.Context, id domain.ToolID) (toolinstall.Removal, error) {
	tool, lookupErr := s.catalog.ByID(ctx, id)
	removed, removeErr := s.installer.Remove(ctx, string(id))
	if removeErr != nil {
		return toolinstall.Removal{}, removeErr
	}
	if s.reloader != nil {
		if reloadErr := s.reloader.Reload(ctx); reloadErr != nil {
			return removed, fmt.Errorf("tool removida, mas o catálogo não recarregou: %w", reloadErr)
		}
	}
	var stateErr error
	if lookupErr == nil && tool.Host != "" {
		stateErr = s.clearDisabled(ctx, tool.Ref())
	} else {
		// Um manifest quebrado não entra no catálogo, mas sua instalação ainda
		// precisa ser removível. Sem a procedência, só resta limpar as entradas
		// desse ID para não deixar preferência órfã no arquivo.
		stateErr = s.clearDisabledID(ctx, id)
	}
	if stateErr != nil {
		return removed, fmt.Errorf("tool removida, mas a preferência de ativação não foi limpa: %w", stateErr)
	}
	return removed, nil
}

func (s *Service) ensureState(ctx context.Context) error {
	s.mu.RLock()
	loaded := s.loaded
	s.mu.RUnlock()
	if loaded {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loaded {
		return nil
	}
	if s.store == nil {
		s.loaded = true
		return nil
	}
	state, err := s.store.Load(ctx)
	if err != nil {
		return err
	}
	for _, ref := range state.Disabled {
		if ref.ID == "" || strings.TrimSpace(ref.Host) == "" {
			return errors.New("estado de tools contém referência incompleta")
		}
		s.disabled[ref] = true
	}
	s.loaded = true
	return nil
}

func (s *Service) clearDisabled(ctx context.Context, ref domain.ToolRef) error {
	if err := s.ensureState(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.disabled[ref] {
		return nil
	}
	next := cloneSet(s.disabled)
	delete(next, ref)
	if err := s.saveLocked(ctx, next); err != nil {
		return err
	}
	s.disabled = next
	return nil
}

func (s *Service) clearDisabledID(ctx context.Context, id domain.ToolID) error {
	if err := s.ensureState(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := cloneSet(s.disabled)
	changed := false
	for ref := range next {
		if ref.ID == id {
			delete(next, ref)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	if err := s.saveLocked(ctx, next); err != nil {
		return err
	}
	s.disabled = next
	return nil
}

func (s *Service) saveLocked(ctx context.Context, disabled map[domain.ToolRef]bool) error {
	if s.store == nil {
		return nil
	}
	refs := make([]domain.ToolRef, 0, len(disabled))
	for ref := range disabled {
		refs = append(refs, ref)
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].Host != refs[j].Host {
			return refs[i].Host < refs[j].Host
		}
		return refs[i].ID < refs[j].ID
	})
	return s.store.Save(ctx, State{Disabled: refs})
}

func cloneSet(source map[domain.ToolRef]bool) map[domain.ToolRef]bool {
	copy := make(map[domain.ToolRef]bool, len(source))
	for ref, value := range source {
		copy[ref] = value
	}
	return copy
}
