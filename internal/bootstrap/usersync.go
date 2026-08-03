package bootstrap

import (
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/mateuslh/lealing/internal/adapter/outbound/githubauth"
	"github.com/mateuslh/lealing/internal/adapter/outbound/githubstate"
	"github.com/mateuslh/lealing/internal/adapter/outbound/persistence"
	"github.com/mateuslh/lealing/internal/adapter/outbound/settingsstore"
	"github.com/mateuslh/lealing/internal/adapter/outbound/usersyncstore"
	"github.com/mateuslh/lealing/internal/core/marketplace"
	"github.com/mateuslh/lealing/internal/core/port/outbound"
	"github.com/mateuslh/lealing/internal/core/settings"
	"github.com/mateuslh/lealing/internal/core/toolinstall"
	"github.com/mateuslh/lealing/internal/core/usersync"
	"github.com/mateuslh/lealing/internal/platform/logging"
	"github.com/mateuslh/lealing/internal/platform/secrets"
	"github.com/mateuslh/lealing/internal/platform/xdg"
)

// githubClientID é o OAuth App do lealing no device flow.
//
// Fica no código de propósito. No device flow não existe segredo de cliente
// — é justamente o que o torna adequado a um binário distribuído —, e este
// valor já viaja dentro de todo binário publicado, ao alcance de um
// `strings`. Deixá-lo só no -ldflags quebrava o build local, o `go install`
// e o wrapper do `make install`, que é como a engine roda no dia a dia.
//
// Um fork sobrescreve com -ldflags ou com LEALING_GITHUB_CLIENT_ID para que
// seus usuários não apareçam como este aplicativo na página de autorizações.
var githubClientID = "Ov23liI8rDaUJ7L0Ac93"

// SyncSettingsFileName guarda os ajustes de sincronização desta máquina.
const SyncSettingsFileName = "sync.json"

// secretService nomeia o item no chaveiro do macOS. Separado do que a tool de
// contas usa: apagar um não pode levar o outro junto.
const secretService = "lealing-sync"

// SettingsFileName guarda os ajustes que o usuário mudou.
const SettingsFileName = "settings.json"

// newSettings monta a configuração da engine com os padrões que só o
// composition root conhece e as linhas de ambiente que a tela exibe.
func newSettings(directories xdg.Directories, engineVersion string) (*settings.Service, error) {
	return settings.NewService(settings.Config{
		Store: settingsstore.New(filepath.Join(directories.Config, SettingsFileName)),
		Defaults: map[settings.Key]string{
			settings.KeyGitHubClientID:   githubClientID,
			settings.KeyMarketplaceIndex: DefaultMarketplaceURL,
		},
		Lookup: os.LookupEnv,
		Rows: []settings.InfoRow{
			{Section: settings.SectionUpdates.ID, Label: "versão", Value: engineVersion},
			{Section: settings.SectionEnvironment.ID, Label: "configuração", Value: directories.Config},
			{Section: settings.SectionEnvironment.ID, Label: "dados", Value: directories.Data},
			{Section: settings.SectionEnvironment.ID, Label: "cache", Value: directories.Cache},
			{Section: settings.SectionEnvironment.ID, Label: "tools", Value: directories.Tools},
		},
	})
}

// SyncManager compõe a sincronização para a CLI, sem TUI por perto.
func SyncManager(engineVersion string) (usersync.Manager, error) {
	platform := currentPlatform()
	directories := directoriesFor(platform)
	config, err := newSettings(directories, engineVersion)
	if err != nil {
		return nil, err
	}
	usage := persistence.NewUsageFile(
		filepath.Join(directories.Data, UsageFileName), usageDebounce)
	defer usage.Close()
	log := outbound.Logger(logging.NewDiscard())
	catalog := newToolRepository(directories, platform, false, log)
	indexURL := config.String(settings.KeyMarketplaceIndex)
	return newSyncManager(engineVersion, directories, config,
		usage, marketplaceSourceStore(directories), newToolManager(directories.Tools), catalog, indexURL), nil
}

func newSyncManager(
	engineVersion string,
	directories xdg.Directories,
	config *settings.Service,
	usage outbound.UsageStore,
	sources marketplace.SourceStore,
	installed toolinstall.Manager,
	catalog outbound.ToolRepository,
	indexURL string,
) usersync.Manager {
	client := &http.Client{Timeout: time.Minute}
	return usersync.NewService(usersync.Config{
		Auth: githubauth.New(githubauth.Config{
			Client:   client,
			ClientID: func() string { return config.String(settings.KeyGitHubClientID) },
		}),
		Tokens: usersyncstore.NewTokens(
			secrets.New(secretService, directories.Data),
		),
		Remote: githubstate.New(githubstate.Config{Client: client}),
		Local: usersync.NewLocalState(
			usage, sources, installed, catalog, builtinSources(indexURL),
		),
		Settings: usersyncstore.NewSettings(
			filepath.Join(directories.Config, SyncSettingsFileName),
		),
		Device: deviceName(),
		Engine: engineVersion,
	})
}

// deviceName identifica esta máquina no documento. O hostname basta: ele já
// é o nome pelo qual o usuário chama o computador, e nada aqui depende de
// unicidade global.
func deviceName() string {
	name, err := os.Hostname()
	if err != nil || name == "" {
		return "máquina desconhecida"
	}
	return name
}

// UsageFileName e usageDebounce são compartilhados pela TUI e pela CLI: o
// debounce de meio segundo agrupa rajadas de favoritos em uma escrita só, sem
// que o usuário perceba atraso.
const (
	UsageFileName = "usage.json"
	usageDebounce = 500 * time.Millisecond
)
