package marketplace

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/mateuslh/lealing/internal/core/toolinstall"
)

// DesiredTool é uma referência instalável completa do estado declarativo.
type DesiredTool struct {
	Host    string
	ID      string
	Version string
}

// StateReconcileRequest separa ausência de uma seção de uma seção vazia.
// Sources nil preserva as origens locais; ExactTools false preserva todas as
// tools, exceto as pertencentes a origens removidas.
type StateReconcileRequest struct {
	Sources    *SourceState
	Tools      []DesiredTool
	ExactTools bool
}

type StateReconciliation struct {
	Sources     int
	Tools       int
	RecoveryDir string
}

// StateReconciler aplica origens e instalações como uma única intenção.
type StateReconciler interface {
	ReconcileState(ctx context.Context, request StateReconcileRequest) (StateReconciliation, error)
}

var _ StateReconciler = (*Service)(nil)

func (s *Service) ReconcileState(ctx context.Context, request StateReconcileRequest) (StateReconciliation, error) {
	s.mutation.Lock()
	defer s.mutation.Unlock()
	return s.reconcileState(ctx, request)
}

func (s *Service) reconcileState(ctx context.Context, request StateReconcileRequest) (StateReconciliation, error) {
	currentSources, err := s.loadState(ctx)
	if err != nil {
		return StateReconciliation{}, err
	}
	nextSources := cloneSourceState(currentSources)
	if request.Sources != nil {
		nextSources = cloneSourceState(*request.Sources)
	}
	currentTools, err := s.config.Installer.ListInstalled(ctx)
	if err != nil {
		return StateReconciliation{}, err
	}

	currentHosts := originNames(s.merge(currentSources))
	nextOrigins := s.merge(nextSources)
	nextHosts := originNames(nextOrigins)
	managedHosts := cloneNames(currentHosts)
	for host := range nextHosts {
		managedHosts[host] = true
	}

	desired, err := desiredInstalled(request, currentTools, managedHosts, nextHosts)
	if err != nil {
		return StateReconciliation{}, err
	}
	requests, cleanups, err := s.prepareReconciliation(ctx, desired, currentTools, nextOrigins)
	if err != nil {
		return StateReconciliation{}, err
	}
	defer func() {
		for _, cleanup := range cleanups {
			if cleanup != nil {
				_ = cleanup()
			}
		}
	}()

	reconciler, ok := s.config.Installer.(toolinstall.Reconciler)
	if !ok {
		if reconciliationChanges(currentTools, desired) != 0 {
			return StateReconciliation{}, errors.New("instalador não oferece reconciliação atômica")
		}
	} else {
		// A união temporária fecha a janela de crash entre dois stores: origens
		// novas já existem antes de suas tools entrarem, e origens removidas só
		// somem depois que suas tools saíram.
		bridgedSources := false
		if request.Sources != nil {
			bridge := bridgeSourceStates(currentSources, nextSources)
			if sourceChanges(currentSources, bridge) != 0 {
				if saveErr := s.saveState(ctx, bridge); saveErr != nil {
					return StateReconciliation{}, saveErr
				}
				bridgedSources = true
			}
		}
		toolResult, reconcileErr := reconciler.Reconcile(ctx, requests)
		if reconcileErr != nil {
			if bridgedSources {
				if restoreErr := s.saveState(context.WithoutCancel(ctx), currentSources); restoreErr != nil {
					return StateReconciliation{}, fmt.Errorf(
						"reconciliar tools: %v; restaurar origens após a falha: %w", reconcileErr, restoreErr)
				}
			}
			return StateReconciliation{}, reconcileErr
		}
		if request.Sources != nil {
			if saveErr := s.saveState(ctx, nextSources); saveErr != nil {
				restoreCtx := context.WithoutCancel(ctx)
				if restoreErr := reconciler.Restore(restoreCtx, toolResult); restoreErr != nil {
					return StateReconciliation{}, fmt.Errorf(
						"salvar origens: %v; restaurar tools após a falha: %w", saveErr, restoreErr)
				}
				if bridgedSources {
					if sourceErr := s.saveState(restoreCtx, currentSources); sourceErr != nil {
						return StateReconciliation{}, fmt.Errorf(
							"salvar origens: %v; tools restauradas, mas origens não: %w", saveErr, sourceErr)
					}
				}
				return StateReconciliation{}, saveErr
			}
		}
		if toolResult.Changed > 0 && s.config.CatalogReloader != nil {
			if reloadErr := s.config.CatalogReloader.Reload(ctx); reloadErr != nil {
				return StateReconciliation{}, fmt.Errorf("estado reconciliado, mas o catálogo não recarregou: %w", reloadErr)
			}
		}
		return StateReconciliation{
			Sources:     sourceChanges(currentSources, nextSources),
			Tools:       toolResult.Changed,
			RecoveryDir: toolResult.RecoveryDir,
		}, nil
	}

	if request.Sources != nil {
		if err := s.saveState(ctx, nextSources); err != nil {
			return StateReconciliation{}, err
		}
	}
	return StateReconciliation{Sources: sourceChanges(currentSources, nextSources)}, nil
}

func desiredInstalled(
	request StateReconcileRequest,
	current []toolinstall.Installed,
	managedHosts, nextHosts map[string]bool,
) ([]DesiredTool, error) {
	if !request.ExactTools {
		desired := make([]DesiredTool, 0, len(current))
		for _, item := range current {
			// Uma origem removida deixa de fazer parte de nextHosts. Instalações
			// locais avulsas nunca estiveram em managedHosts e são preservadas.
			if managedHosts[item.Host] && !nextHosts[item.Host] {
				continue
			}
			desired = append(desired, DesiredTool{Host: item.Host, ID: item.ID, Version: item.ActiveVersion})
		}
		return desired, nil
	}

	desired := append([]DesiredTool(nil), request.Tools...)
	seen := make(map[string]bool, len(desired))
	for _, tool := range desired {
		if !nextHosts[tool.Host] {
			return nil, fmt.Errorf("tool %s/%s referencia uma origem ausente", tool.Host, tool.ID)
		}
		if seen[tool.ID] {
			return nil, fmt.Errorf("ID de tool repetido entre origens no estado desejado: %s", tool.ID)
		}
		seen[tool.ID] = true
	}
	// Instalações avulsas não são reproduzíveis por marketplace e, por isso,
	// ficam fora do snapshot sem serem apagadas por ele.
	for _, item := range current {
		if !managedHosts[item.Host] && !seen[item.ID] {
			desired = append(desired, DesiredTool{Host: item.Host, ID: item.ID, Version: item.ActiveVersion})
			seen[item.ID] = true
		}
	}
	return desired, nil
}

func (s *Service) prepareReconciliation(
	ctx context.Context,
	desired []DesiredTool,
	current []toolinstall.Installed,
	origins []Origin,
) ([]toolinstall.InstallRequest, []func() error, error) {
	active := make(map[string]toolinstall.Installed, len(current))
	for _, item := range current {
		active[item.ID] = item
	}
	originByName := make(map[string]Origin, len(origins))
	for _, origin := range origins {
		originByName[origin.Name] = origin
	}
	indexes := map[string]Index{}
	requests := make([]toolinstall.InstallRequest, 0, len(desired))
	cleanups := make([]func() error, 0, len(desired))
	fail := func(err error) ([]toolinstall.InstallRequest, []func() error, error) {
		for _, cleanup := range cleanups {
			if cleanup != nil {
				_ = cleanup()
			}
		}
		return nil, nil, err
	}

	for _, tool := range desired {
		installed := active[tool.ID]
		if installed.Host == tool.Host && installed.ActiveVersion == tool.Version {
			requests = append(requests, toolinstall.InstallRequest{
				Host: tool.Host, ExpectedID: tool.ID, ExpectedVersion: tool.Version,
			})
			continue
		}
		origin, known := originByName[tool.Host]
		if !known {
			// Só instalações avulsas já presentes chegam aqui sem origem.
			return fail(fmt.Errorf("não é possível reproduzir %s@%s sem uma origem", tool.ID, tool.Version))
		}
		index, loaded := indexes[tool.Host]
		if !loaded {
			entries, fetchErr := s.fetchOrigin(ctx, origin)
			if fetchErr != nil {
				return fail(fmt.Errorf("consultar origem %s: %w", tool.Host, fetchErr))
			}
			index = Index{APIVersion: APIVersion, Tools: entries}
			indexes[tool.Host] = index
		}
		entry, artifact, selectErr := index.SelectVersion(
			tool.ID, tool.Version, s.config.Platform, s.config.EngineVersion, s.config.Protocol)
		if selectErr != nil {
			return fail(fmt.Errorf("%s/%s@%s: %w", tool.Host, tool.ID, tool.Version, selectErr))
		}
		prepared, prepareErr := s.config.Packages.Prepare(ctx, origin, artifact)
		if prepareErr != nil {
			return fail(prepareErr)
		}
		cleanups = append(cleanups, prepared.Cleanup)
		requests = append(requests, installRequest(entry, prepared.Directory))
	}
	sort.Slice(requests, func(i, j int) bool { return requests[i].ExpectedID < requests[j].ExpectedID })
	return requests, cleanups, nil
}

func installRequest(entry Entry, sourceDir string) toolinstall.InstallRequest {
	return toolinstall.InstallRequest{
		Host: entry.Origin.Name, SourceDir: sourceDir,
		ExpectedID: entry.ID, ExpectedVersion: entry.Version,
		ExpectedManifest: &toolinstall.ManifestExpectation{
			ID: entry.ID, Version: entry.Version, Name: entry.Name, Summary: entry.Summary,
			Detail: entry.Detail, Category: entry.Category, Risk: entry.Risk, Glyph: entry.Glyph,
			ProtocolMin: entry.Protocol.Min, ProtocolMax: entry.Protocol.Max,
			FilesystemRead:  append([]string(nil), entry.Permissions.Filesystem.Read...),
			FilesystemWrite: append([]string(nil), entry.Permissions.Filesystem.Write...),
			Network:         entry.Permissions.Network, Subprocess: entry.Permissions.Subprocess,
			WorkingDir: entry.Permissions.WorkingDir,
		},
	}
}

func reconciliationChanges(current []toolinstall.Installed, desired []DesiredTool) int {
	left := make(map[string]string, len(current))
	for _, item := range current {
		left[item.ID] = item.Host + "\x00" + item.ActiveVersion
	}
	right := make(map[string]string, len(desired))
	for _, item := range desired {
		right[item.ID] = item.Host + "\x00" + item.Version
	}
	changed := 0
	for id, value := range left {
		if right[id] != value {
			changed++
		}
	}
	for id, value := range right {
		if left[id] == "" && value != "" {
			changed++
		}
	}
	return changed
}

func originNames(origins []Origin) map[string]bool {
	names := make(map[string]bool, len(origins))
	for _, origin := range origins {
		names[origin.Name] = true
	}
	return names
}

func cloneNames(source map[string]bool) map[string]bool {
	cloned := make(map[string]bool, len(source))
	for name := range source {
		cloned[name] = true
	}
	return cloned
}

func cloneSourceState(state SourceState) SourceState {
	return SourceState{
		Custom:           append([]Origin(nil), state.Custom...),
		DisabledBuiltins: append([]string(nil), state.DisabledBuiltins...),
	}
}

func bridgeSourceStates(current, next SourceState) SourceState {
	bridge := cloneSourceState(current)
	positions := make(map[string]int, len(bridge.Custom))
	for index, origin := range bridge.Custom {
		positions[origin.Name] = index
	}
	for _, origin := range next.Custom {
		if index, exists := positions[origin.Name]; exists {
			// O nome estável mantém a procedência conhecida durante uma troca de
			// endereço; o pacote novo já foi preparado antes desta escrita.
			bridge.Custom[index] = origin
			continue
		}
		positions[origin.Name] = len(bridge.Custom)
		bridge.Custom = append(bridge.Custom, origin)
	}
	return bridge
}

func sourceChanges(current, next SourceState) int {
	left := make(map[string]Origin, len(current.Custom))
	for _, origin := range current.Custom {
		left[origin.Name] = origin
	}
	right := make(map[string]Origin, len(next.Custom))
	for _, origin := range next.Custom {
		right[origin.Name] = origin
	}
	changed := 0
	for name, origin := range left {
		if right[name] != origin {
			changed++
		}
	}
	for name, origin := range right {
		if _, exists := left[name]; !exists && origin.Name != "" {
			changed++
		}
	}
	if !sameNames(current.DisabledBuiltins, next.DisabledBuiltins) {
		changed++
	}
	return changed
}

func sameNames(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	a, b := append([]string(nil), left...), append([]string(nil), right...)
	sort.Strings(a)
	sort.Strings(b)
	for index := range a {
		if a[index] != b[index] {
			return false
		}
	}
	return true
}
