package bootstrap

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/mateuslh/lealing-sdk/protocol"
	"github.com/mateuslh/lealing/internal/adapter/outbound/externalcatalog"
	"github.com/mateuslh/lealing/internal/adapter/outbound/marketplacefile"
	"github.com/mateuslh/lealing/internal/adapter/outbound/marketplacehttp"
	"github.com/mateuslh/lealing/internal/adapter/outbound/marketplacesources"
	"github.com/mateuslh/lealing/internal/adapter/outbound/registry"
	"github.com/mateuslh/lealing/internal/adapter/outbound/toolstate"
	"github.com/mateuslh/lealing/internal/adapter/outbound/toolstore"
	"github.com/mateuslh/lealing/internal/catalog"
	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/marketplace"
	"github.com/mateuslh/lealing/internal/core/port/outbound"
	"github.com/mateuslh/lealing/internal/core/toolinstall"
	"github.com/mateuslh/lealing/internal/core/toolmanage"
	"github.com/mateuslh/lealing/internal/platform/logging"
	"github.com/mateuslh/lealing/internal/platform/xdg"
	"github.com/mateuslh/lealing/internal/toolmanifest"
)

// DefaultMarketplaceURL é o índice público consolidado. O endereço não
// contém versão numérica: cada entrada do índice aponta para artefatos
// imutáveis e a engine escolhe a versão compatível mais recente.
const DefaultMarketplaceURL = "https://raw.githubusercontent.com/mateuslh/lealing-tools/main/marketplace/index.json"

// DefaultSourceName é o nome da origem padrão. Fixo porque aparece nas
// referências qualificadas "lealing/example-tool" e no arquivo de origens.
const DefaultSourceName = "lealing"

// SourcesFileName guarda os repositórios de tools adicionados pelo usuário.
const SourcesFileName = "marketplace-sources.json"

// ToolStateFileName guarda as referências qualificadas desativadas. Vincular
// ID e host impede que outro provider com o mesmo ID herde a preferência.
const ToolStateFileName = "tool-state.json"

// marketplaceSourceStore abre o arquivo de origens. O marketplace e a
// sincronização leem o mesmo disco; a escrita atômica é o que os mantém
// coerentes sem precisarem se conhecer.
func marketplaceSourceStore(directories xdg.Directories) marketplace.SourceStore {
	return marketplacesources.New(filepath.Join(directories.Config, SourcesFileName))
}

// originRouter escolhe o adapter pelo tipo da origem.
//
// O roteamento mora no composition root porque é aqui que as duas
// implementações se encontram: nem o core deve saber que existe HTTP, nem um
// driven adapter pode compor outro.
type originRouter struct {
	remote *marketplacehttp.Source
	local  *marketplacefile.Source
}

var (
	_ marketplace.IndexSource   = originRouter{}
	_ marketplace.PackageSource = originRouter{}
)

func (r originRouter) Fetch(ctx context.Context, origin marketplace.Origin) (marketplace.Index, error) {
	if origin.Kind == marketplace.OriginLocal {
		return r.local.Fetch(ctx, origin)
	}
	return r.remote.Fetch(ctx, origin)
}

func (r originRouter) Prepare(ctx context.Context, origin marketplace.Origin, artifact marketplace.Artifact) (marketplace.PreparedPackage, error) {
	if origin.Kind == marketplace.OriginLocal {
		return r.local.Prepare(ctx, origin, artifact)
	}
	return r.remote.Prepare(ctx, origin, artifact)
}

// builtinSources devolve as origens que acompanham a engine.
//
// É a única origem confiável: só ela pode exibir os selos official e
// verified, e é por isso que trocá-la exige a flag explícita
// -marketplace-url em vez de uma entrada no arquivo de configuração.
func builtinSources(indexURL string) []marketplace.Origin {
	if indexURL == "" {
		indexURL = DefaultMarketplaceURL
	}
	return []marketplace.Origin{{
		Name:    DefaultSourceName,
		Label:   "índice padrão",
		Kind:    marketplace.OriginRemote,
		Ref:     indexURL,
		Trusted: true,
		Builtin: true,
		Enabled: true,
	}}
}

// ToolManager compõe o instalador local para a plataforma corrente. A CLI
// recebe somente a porta de entrada; paths, manifest e trocas atômicas
// permanecem nos adapters ligados aqui.
func ToolManager() toolinstall.Manager {
	platform := currentPlatform()
	directories := directoriesFor(platform)
	return newToolManager(directories.Tools)
}

func newToolManager(root string) toolinstall.Manager {
	store := toolstore.New(root, catalog.Categories(), currentToolTarget(), nil)
	return toolinstall.NewService(store)
}

// ToolManagement compõe a visão completa usada pelos comandos explícitos da
// CLI. Diferentemente da home, ela continua listando IDs desativados.
func ToolManagement() toolmanage.Manager {
	platform := currentPlatform()
	directories := directoriesFor(platform)
	log := outbound.Logger(logging.NewDiscard())
	repo := newToolRepository(directories, platform, false, log)
	installer := newToolManager(directories.Tools)
	return toolmanage.NewService(
		repo,
		toolstate.New(filepath.Join(directories.Config, ToolStateFileName)),
		installer,
		repo,
	)
}

func newToolRepository(
	directories xdg.Directories,
	platform domain.Platform,
	strict bool,
	log outbound.Logger,
) *registry.Registry {
	providers := catalog.Providers()
	providers = append(providers, externalcatalog.New(externalcatalog.Options{
		Root:        directories.Tools,
		Categories:  catalog.Categories(),
		Reserved:    catalog.ReservedIDs(),
		Target:      currentToolTarget(),
		DefaultHost: DefaultSourceName,
		Strict:      strict,
		Logger:      log,
	}))
	return registry.New(providers,
		registry.WithLogger(log),
		registry.WithStrict(strict),
		registry.WithPlatform(platform),
	)
}

// MarketplaceManager compõe o catálogo remoto para comandos da CLI. A TUI
// usa a mesma composição com o registry vivo como recarregador.
func MarketplaceManager(engineVersion, indexURL string) marketplace.Manager {
	directories := directoriesFor(currentPlatform())
	return newMarketplaceManager(engineVersion, indexURL, newToolManager(directories.Tools), nil, directories)
}

func newMarketplaceManager(
	engineVersion, indexURL string,
	installer toolinstall.Manager,
	reloader outbound.ReloadableToolRepository,
	directories xdg.Directories,
) *marketplace.Service {
	sources := originRouter{
		remote: marketplacehttp.New(marketplacehttp.Config{
			Client:        &http.Client{Timeout: 2 * time.Minute},
			TemporaryRoot: filepath.Join(directories.Cache, "marketplace"),
		}),
		local: marketplacefile.New(marketplacefile.Config{}),
	}
	categories := make(map[string]bool)
	for _, category := range catalog.Categories() {
		categories[string(category.ID)] = true
	}
	platforms := map[string]bool{
		"darwin-amd64": true, "darwin-arm64": true,
		"windows-amd64": true, "windows-arm64": true,
	}
	target := currentToolTarget()
	return marketplace.NewService(marketplace.Config{
		Platform:        target.OS + "-" + target.Arch,
		EngineVersion:   engineVersion,
		Protocol:        marketplace.VersionRange{Min: protocol.Version1, Max: protocol.Version1},
		Validation:      marketplace.ValidationOptions{Categories: categories, Platforms: platforms},
		BuiltinSources:  builtinSources(indexURL),
		Sources:         marketplaceSourceStore(directories),
		Index:           sources,
		Packages:        sources,
		Installer:       installer,
		CatalogReloader: reloader,
	})
}

// ValidateToolManifest valida um arquivo sem abrir nem verificar o binário.
// É o comando usado por autores de tools e pela pipeline de artefatos.
func ValidateToolManifest(path string) (domain.ToolID, string, error) {
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		path = filepath.Join(path, "manifest.yaml")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	categories := make(map[domain.CategoryID]bool)
	for _, category := range catalog.Categories() {
		categories[category.ID] = true
	}
	manifest, err := toolmanifest.ParseAndValidate(raw, toolmanifest.ValidationOptions{
		Categories: categories,
		Target:     currentToolTarget(),
	})
	if err != nil {
		return "", "", err
	}
	return domain.ToolID(manifest.ID), manifest.Version, nil
}
