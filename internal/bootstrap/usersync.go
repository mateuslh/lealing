package bootstrap

import (
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/mateuslh/lealing/internal/adapter/outbound/githubauth"
	"github.com/mateuslh/lealing/internal/adapter/outbound/githubstate"
	"github.com/mateuslh/lealing/internal/adapter/outbound/persistence"
	"github.com/mateuslh/lealing/internal/adapter/outbound/usersyncstore"
	"github.com/mateuslh/lealing/internal/core/marketplace"
	"github.com/mateuslh/lealing/internal/core/port/outbound"
	"github.com/mateuslh/lealing/internal/core/toolinstall"
	"github.com/mateuslh/lealing/internal/core/usersync"
	"github.com/mateuslh/lealing/internal/platform/secrets"
	"github.com/mateuslh/lealing/internal/platform/xdg"
)

// githubClientID é o OAuth App usado no device flow.
//
// Fica vazio no código e é injetado no build com
// -ldflags "-X ...bootstrap.githubClientID=Iv1.xxxx", porque cada fork
// precisa registrar o próprio app: um client_id compartilhado faria todo
// mundo aparecer como o mesmo aplicativo na página de autorizações do
// usuário. A variável de ambiente cobre o desenvolvimento local.
var githubClientID = ""

// SyncSettingsFileName guarda os ajustes de sincronização desta máquina.
const SyncSettingsFileName = "sync.json"

// secretService nomeia o item no chaveiro do macOS. Separado do que a tool de
// contas usa: apagar um não pode levar o outro junto.
const secretService = "lealing-sync"

// GitHubClientID resolve o app do build, com a variável de ambiente na
// frente para quem está desenvolvendo.
func GitHubClientID() string {
	if fromEnv := os.Getenv("LEALING_GITHUB_CLIENT_ID"); fromEnv != "" {
		return fromEnv
	}
	return githubClientID
}

// SyncManager compõe a sincronização para a CLI, sem TUI por perto.
func SyncManager(engineVersion string) usersync.Manager {
	platform := currentPlatform()
	directories := directoriesFor(platform)
	usage := persistence.NewUsageFile(
		filepath.Join(directories.Data, UsageFileName), usageDebounce)
	defer usage.Close()
	return newSyncManager(engineVersion, directories,
		usage, marketplaceSourceStore(directories), newToolManager(directories.Tools))
}

func newSyncManager(
	engineVersion string,
	directories xdg.Directories,
	usage outbound.UsageStore,
	sources marketplace.SourceStore,
	installed toolinstall.Manager,
) usersync.Manager {
	client := &http.Client{Timeout: time.Minute}
	return usersync.NewService(usersync.Config{
		Auth: githubauth.New(githubauth.Config{
			Client:   client,
			ClientID: GitHubClientID(),
		}),
		Tokens: usersyncstore.NewTokens(
			secrets.New(secretService, directories.Data),
		),
		Remote: githubstate.New(githubstate.Config{Client: client}),
		Local:  usersync.NewLocalState(usage, sources, installed),
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
