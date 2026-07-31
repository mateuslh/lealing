package bootstrap

import (
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/mateuslh/lealing/internal/adapter/outbound/marketplacehttp"
	"github.com/mateuslh/lealing/internal/adapter/outbound/toolstore"
	"github.com/mateuslh/lealing/internal/catalog"
	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/marketplace"
	"github.com/mateuslh/lealing/internal/core/port/outbound"
	"github.com/mateuslh/lealing/internal/core/toolinstall"
	"github.com/mateuslh/lealing/internal/toolmanifest"
	"github.com/mateuslh/lealing/sdk/protocol"
)

// DefaultMarketplaceURL é o índice público consolidado. O endereço não
// contém versão numérica: cada entrada do índice aponta para artefatos
// imutáveis e a engine escolhe a versão compatível mais recente.
const DefaultMarketplaceURL = "https://raw.githubusercontent.com/mateuslh/lealing-tools/main/marketplace/index.json"

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

// MarketplaceManager compõe o catálogo remoto para comandos da CLI. A TUI
// usa a mesma composição com o registry vivo como recarregador.
func MarketplaceManager(engineVersion, indexURL string) marketplace.Manager {
	directories := directoriesFor(currentPlatform())
	return newMarketplaceManager(engineVersion, indexURL, newToolManager(directories.Tools), nil, directories.Cache)
}

func newMarketplaceManager(
	engineVersion, indexURL string,
	installer toolinstall.Manager,
	reloader outbound.ReloadableToolRepository,
	cacheRoot string,
) marketplace.Manager {
	if indexURL == "" {
		indexURL = DefaultMarketplaceURL
	}
	remote := marketplacehttp.New(marketplacehttp.Config{
		Client:        &http.Client{Timeout: 2 * time.Minute},
		IndexURL:      indexURL,
		TemporaryRoot: filepath.Join(cacheRoot, "marketplace"),
	})
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
		Index:           remote,
		Packages:        remote,
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
