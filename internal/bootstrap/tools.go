package bootstrap

import (
	"os"
	"path/filepath"

	"github.com/mateuslh/lealing/internal/adapter/outbound/toolstore"
	"github.com/mateuslh/lealing/internal/catalog"
	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/toolinstall"
	"github.com/mateuslh/lealing/internal/toolmanifest"
)

// ToolManager compõe o instalador local para a plataforma corrente. A CLI
// recebe somente a porta de entrada; paths, manifest e trocas atômicas
// permanecem nos adapters ligados aqui.
func ToolManager() toolinstall.Manager {
	platform := currentPlatform()
	directories := directoriesFor(platform)
	store := toolstore.New(directories.Tools, catalog.Categories(), currentToolTarget(), nil)
	return toolinstall.NewService(store)
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
