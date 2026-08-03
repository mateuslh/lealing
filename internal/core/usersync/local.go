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
	usage          outbound.UsageStore
	sources        marketplace.SourceStore
	installed      toolinstall.Manager
	catalog        outbound.ToolRepository
	builtinSources []marketplace.Origin
}

var _ Local = (*LocalState)(nil)

func NewLocalState(
	usage outbound.UsageStore,
	sources marketplace.SourceStore,
	installed toolinstall.Manager,
	catalog outbound.ToolRepository,
	builtinSources []marketplace.Origin,
) *LocalState {
	return &LocalState{
		usage: usage, sources: sources, installed: installed, catalog: catalog,
		builtinSources: append([]marketplace.Origin(nil), builtinSources...),
	}
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
// Instalar tools está deliberadamente fora: baixar e executar código de
// terceiros é uma decisão que o usuário toma tool a tool, no marketplace, e
// não algo que um "baixar preferências" possa disparar em silêncio. A lista
// viaja para a tela poder oferecer a instalação; aplicar é outra coisa.
func (l *LocalState) Apply(ctx context.Context, state State, selection Selection) (Applied, error) {
	applied := Applied{}

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

	if selection.Enabled(SectionSources) && l.sources != nil {
		current, err := l.sources.Load(ctx)
		if err != nil {
			return applied, err
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
		for _, source := range state.Sources {
			if builtins[source.Name] {
				// O remoto pode sincronizar somente o estado ligado/desligado.
				// Endereço e confiança da origem embutida pertencem à engine.
				if source.Enabled {
					delete(disabled, source.Name)
				} else {
					disabled[source.Name] = true
				}
				applied[SectionSources]++
				continue
			}
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
		disabledNames := make([]string, 0, len(disabled))
		for name := range disabled {
			disabledNames = append(disabledNames, name)
		}
		sort.Strings(disabledNames)
		current.Custom, current.DisabledBuiltins = custom, disabledNames
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
