package usersync

import (
	"context"
	"errors"
	"sort"

	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/marketplace"
	"github.com/mateuslh/lealing/internal/core/port/outbound"
	"github.com/mateuslh/lealing/internal/core/toolinstall"
)

// LocalState traduz entre o documento sincronizado e os stores da engine.
//
// Mora no núcleo porque é política, não infraestrutura: o que vira preferência
// sincronizável, o que é aplicado ao baixar e — principalmente — o que nunca
// é aplicado sozinho.
type LocalState struct {
	usage          outbound.UsageStore
	sources        marketplace.SourceStore
	installed      toolinstall.Manager
	catalog        outbound.ToolRepository
	builtinSources []marketplace.Origin
	reconciler     marketplace.StateReconciler
}

var _ Local = (*LocalState)(nil)

func NewLocalState(
	usage outbound.UsageStore,
	sources marketplace.SourceStore,
	installed toolinstall.Manager,
	catalog outbound.ToolRepository,
	builtinSources []marketplace.Origin,
	reconciler marketplace.StateReconciler,
) *LocalState {
	return &LocalState{
		usage: usage, sources: sources, installed: installed, catalog: catalog,
		builtinSources: append([]marketplace.Origin(nil), builtinSources...),
		reconciler:     reconciler,
	}
}

func (l *LocalState) Collect(ctx context.Context) (State, error) {
	state := State{Version: StateVersion}
	managedHosts := make(map[string]bool, len(l.builtinSources))
	for _, origin := range l.builtinSources {
		managedHosts[origin.Name] = true
	}

	if l.usage != nil {
		stored, err := l.usage.Load(ctx)
		if err != nil {
			return State{}, err
		}
		for id, usage := range stored {
			// Uma tool sem uso nem favorito não é preferência: é ausência de
			// preferência, e enviá-la só engorda o documento.
			if usage.Runs == 0 && !usage.Favorite {
				continue
			}
			host := usage.Host
			if host == "" && l.catalog != nil {
				if tool, lookupErr := l.catalog.ByID(ctx, id); lookupErr == nil {
					host = tool.Host
				}
			}
			// Estado v3 nunca publica uma referência ambígua. Uma preferência
			// legada sem tool instalada fica apenas no cache local até que a
			// procedência possa ser resolvida novamente.
			if host == "" {
				continue
			}
			state.Usage = append(state.Usage, ToolUsage{
				Host: host, ID: string(id), Runs: usage.Runs,
				LastRun: usage.LastRun.UTC(), Favorite: usage.Favorite,
			})
		}
	}

	if l.sources != nil {
		stored, err := l.sources.Load(ctx)
		if err != nil {
			return State{}, err
		}
		disabled := make(map[string]bool, len(stored.DisabledBuiltins))
		for _, name := range stored.DisabledBuiltins {
			disabled[name] = true
		}
		builtins := make(map[string]bool, len(l.builtinSources))
		for _, origin := range l.builtinSources {
			builtins[origin.Name] = true
			state.Sources = append(state.Sources, MarketplaceSource{
				Name: origin.Name, Label: origin.Label,
				Kind: string(origin.Kind), Ref: origin.Ref, Enabled: !disabled[origin.Name],
			})
		}
		for _, origin := range stored.Custom {
			// Um cliente antigo pode ter gravado uma origem embutida como
			// personalizada. A definição do composition root sempre vence.
			if builtins[origin.Name] {
				continue
			}
			managedHosts[origin.Name] = true
			state.Sources = append(state.Sources, MarketplaceSource{
				Name: origin.Name, Label: origin.Label,
				Kind: string(origin.Kind), Ref: origin.Ref, Enabled: origin.Enabled,
			})
		}
		state.DisabledBuiltins = append(state.DisabledBuiltins, stored.DisabledBuiltins...)
	}

	if l.installed != nil {
		stored, err := l.installed.ListInstalled(ctx)
		if err != nil {
			return State{}, err
		}
		for _, tool := range stored {
			host := tool.Host
			if host == "" && l.catalog != nil {
				if catalogTool, lookupErr := l.catalog.ByID(ctx, domain.ToolID(tool.ID)); lookupErr == nil {
					host = catalogTool.Host
				}
			}
			if host == "" {
				continue
			}
			// Instalações avulsas não têm índice capaz de reproduzi-las em
			// outra máquina. Elas permanecem locais e fora da fonte da verdade.
			if l.sources != nil && !managedHosts[host] {
				continue
			}
			state.Tools = append(state.Tools, InstalledTool{
				Host: host, ID: tool.ID, Version: tool.ActiveVersion,
			})
		}
	}

	state.Normalize()
	return state, nil
}

// Apply grava o documento remoto nesta máquina.
//
// Origens e tools formam uma única intenção declarativa: as origens são
// resolvidas primeiro, todos os pacotes são preparados e o conjunto instalado
// só é trocado quando a reprodução inteira pode terminar.
func (l *LocalState) Apply(ctx context.Context, state State, selection Selection, acceptPermissionEscalation bool) (Applied, error) {
	applied := Applied{}

	var nextSources *marketplace.SourceState
	if selection.Enabled(SectionSources) && l.sources != nil {
		resolved, count, err := l.resolveSources(ctx, state)
		if err != nil {
			return applied, err
		}
		nextSources = &resolved
		applied[SectionSources] = count
	}

	if nextSources != nil || selection.Enabled(SectionTools) {
		if l.reconciler == nil {
			// O fallback mantém testes e composições mínimas capazes de aplicar
			// somente origens quando não há instalação alguma para reconciliar.
			if selection.Enabled(SectionTools) || l.installed != nil {
				return applied, errors.New("reconciliação declarativa de tools não configurada")
			}
			if err := l.sources.Save(ctx, *nextSources); err != nil {
				return applied, err
			}
		} else {
			request := marketplace.StateReconcileRequest{
				Sources: nextSources, AcceptPermissionEscalation: acceptPermissionEscalation,
			}
			if selection.Enabled(SectionTools) {
				request.ExactTools = true
				request.Tools = make([]marketplace.DesiredTool, 0, len(state.Tools))
				for _, tool := range state.Tools {
					request.Tools = append(request.Tools, marketplace.DesiredTool{
						Host: tool.Host, ID: tool.ID, Version: tool.Version,
					})
				}
			}
			result, err := l.reconciler.ReconcileState(ctx, request)
			if err != nil {
				return applied, err
			}
			if nextSources != nil {
				applied[SectionSources] = result.Sources
			}
			if selection.Enabled(SectionTools) {
				applied[SectionTools] = result.Tools
			}
		}
	}

	if selection.Enabled(SectionUsage) && l.usage != nil {
		for _, usage := range state.Usage {
			// A preferência viaja mesmo quando a tool não está instalada. Se
			// houver uma homônima ativa de outro host, ela não pode herdá-la.
			if l.catalog != nil {
				if tool, lookupErr := l.catalog.ByID(ctx, domain.ToolID(usage.ID)); lookupErr == nil && tool.Host != usage.Host {
					continue
				}
			}
			if err := l.usage.Save(ctx, domain.Usage{
				Host: usage.Host, ToolID: domain.ToolID(usage.ID), Runs: usage.Runs,
				LastRun: usage.LastRun, Favorite: usage.Favorite,
			}); err != nil {
				return applied, err
			}
			applied[SectionUsage]++
		}
	}

	return applied, nil
}

func (l *LocalState) resolveSources(ctx context.Context, state State) (marketplace.SourceState, int, error) {
	current, err := l.sources.Load(ctx)
	if err != nil {
		return marketplace.SourceState{}, 0, err
	}
	builtins := make(map[string]bool, len(l.builtinSources))
	for _, origin := range l.builtinSources {
		builtins[origin.Name] = true
	}
	disabled := make(map[string]bool, len(state.DisabledBuiltins))
	for _, name := range state.DisabledBuiltins {
		disabled[name] = true
	}
	custom := make([]marketplace.Origin, 0, len(state.Sources))
	applied := 0
	for _, source := range state.Sources {
		if builtins[source.Name] {
			// Endereço e confiança da origem embutida pertencem à engine.
			if source.Enabled {
				delete(disabled, source.Name)
			} else {
				disabled[source.Name] = true
			}
			applied++
			continue
		}
		origin := marketplace.Origin{
			Name: source.Name, Label: source.Label,
			Kind: marketplace.OriginKind(source.Kind), Ref: source.Ref, Enabled: source.Enabled,
		}
		if origin.Validate() != nil {
			continue
		}
		custom = append(custom, origin)
		applied++
	}
	disabledNames := make([]string, 0, len(disabled))
	for name := range disabled {
		disabledNames = append(disabledNames, name)
	}
	sort.Strings(disabledNames)
	current.Custom, current.DisabledBuiltins = custom, disabledNames
	return current, applied, nil
}

// MissingTools lista o que a outra máquina tinha instalado e falta aqui. É o
// insumo da tela para oferecer a instalação — sem executá-la.
func (l *LocalState) MissingTools(ctx context.Context, state State) ([]InstalledTool, error) {
	if l.installed == nil {
		return nil, nil
	}
	stored, err := l.installed.ListInstalled(ctx)
	if err != nil {
		return nil, err
	}
	present := make(map[string]bool, len(stored))
	for _, tool := range stored {
		host := tool.Host
		if host == "" && l.catalog != nil {
			if catalogTool, lookupErr := l.catalog.ByID(ctx, domain.ToolID(tool.ID)); lookupErr == nil {
				host = catalogTool.Host
			}
		}
		present[host+"\x00"+tool.ID] = true
	}
	missing := make([]InstalledTool, 0, len(state.Tools))
	for _, tool := range state.Tools {
		if !present[tool.Host+"\x00"+tool.ID] {
			missing = append(missing, tool)
		}
	}
	return missing, nil
}
