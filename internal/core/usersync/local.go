package usersync

import (
	"context"
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
	usage     outbound.UsageStore
	sources   marketplace.SourceStore
	installed toolinstall.Manager
}

var _ Local = (*LocalState)(nil)

func NewLocalState(
	usage outbound.UsageStore,
	sources marketplace.SourceStore,
	installed toolinstall.Manager,
) *LocalState {
	return &LocalState{usage: usage, sources: sources, installed: installed}
}

func (l *LocalState) Collect(ctx context.Context) (State, error) {
	state := State{Version: StateVersion}

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
			state.Usage = append(state.Usage, ToolUsage{
				ID: string(id), Runs: usage.Runs,
				LastRun: usage.LastRun.UTC(), Favorite: usage.Favorite,
			})
		}
	}

	if l.sources != nil {
		stored, err := l.sources.Load(ctx)
		if err != nil {
			return State{}, err
		}
		for _, origin := range stored.Custom {
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
			state.Tools = append(state.Tools, InstalledTool{
				ID: tool.ID, Version: tool.ActiveVersion,
			})
		}
	}

	state.Normalize()
	return state, nil
}

// Apply grava o documento remoto nesta máquina.
//
// Instalar tools está deliberadamente fora: baixar e executar código de
// terceiros é uma decisão que o usuário toma tool a tool, no marketplace, e
// não algo que um "baixar preferências" possa disparar em silêncio. A lista
// viaja para a tela poder oferecer a instalação; aplicar é outra coisa.
func (l *LocalState) Apply(ctx context.Context, state State, selection Selection) (Applied, error) {
	applied := Applied{}

	if selection.Enabled(SectionUsage) && l.usage != nil {
		for _, usage := range state.Usage {
			if err := l.usage.Save(ctx, domain.Usage{
				ToolID: domain.ToolID(usage.ID), Runs: usage.Runs,
				LastRun: usage.LastRun, Favorite: usage.Favorite,
			}); err != nil {
				return applied, err
			}
			applied[SectionUsage]++
		}
	}

	if selection.Enabled(SectionSources) && l.sources != nil {
		current, err := l.sources.Load(ctx)
		if err != nil {
			return applied, err
		}
		custom := make([]marketplace.Origin, 0, len(state.Sources))
		for _, source := range state.Sources {
			origin := marketplace.Origin{
				Name: source.Name, Label: source.Label,
				Kind: marketplace.OriginKind(source.Kind), Ref: source.Ref, Enabled: source.Enabled,
			}
			// Uma origem inválida vinda do remoto é descartada, não propagada:
			// o documento pode ter sido escrito por uma versão que aceitava
			// outro formato, ou editado à mão no site.
			if origin.Validate() != nil {
				continue
			}
			custom = append(custom, origin)
			applied[SectionSources]++
		}
		disabled := append([]string(nil), state.DisabledBuiltins...)
		sort.Strings(disabled)
		current.Custom, current.DisabledBuiltins = custom, disabled
		if err := l.sources.Save(ctx, current); err != nil {
			return applied, err
		}
	}

	return applied, nil
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
		present[tool.ID] = true
	}
	missing := make([]InstalledTool, 0, len(state.Tools))
	for _, tool := range state.Tools {
		if !present[tool.ID] {
			missing = append(missing, tool)
		}
	}
	return missing, nil
}
