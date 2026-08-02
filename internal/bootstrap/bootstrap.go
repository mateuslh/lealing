// Package bootstrap é o composition root do lealing.
//
// É o único lugar do programa onde implementações concretas encontram as
// portas que satisfazem. Todo o resto do código conhece apenas interfaces —
// é isso, e não o desenho de diretórios, que faz a arquitetura ser hexagonal
// de fato.
package bootstrap

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	accountsscreen "github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/ccaccount"
	devkitscreen "github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/devkit"
	gitinsightscreen "github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/gitinsight"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/home"
	marketplacescreen "github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/marketplace"
	powerscreen "github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/power"
	repoclonescreen "github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/repoclone"
	sysinfoscreen "github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/sysinfo"
	updatescreen "github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/update"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	"github.com/mateuslh/lealing/internal/adapter/outbound/claudecli"
	devkitadapter "github.com/mateuslh/lealing/internal/adapter/outbound/devkit"
	"github.com/mateuslh/lealing/internal/adapter/outbound/externalcatalog"
	"github.com/mateuslh/lealing/internal/adapter/outbound/gitinsight"
	"github.com/mateuslh/lealing/internal/adapter/outbound/persistence"
	"github.com/mateuslh/lealing/internal/adapter/outbound/pluginprocess"
	"github.com/mateuslh/lealing/internal/adapter/outbound/registry"
	"github.com/mateuslh/lealing/internal/adapter/outbound/requirements"
	"github.com/mateuslh/lealing/internal/adapter/outbound/search"
	"github.com/mateuslh/lealing/internal/catalog"
	"github.com/mateuslh/lealing/internal/core/ccaccount"
	"github.com/mateuslh/lealing/internal/core/devkit"
	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/hostaction"
	"github.com/mateuslh/lealing/internal/core/interactive"
	"github.com/mateuslh/lealing/internal/core/port/outbound"
	"github.com/mateuslh/lealing/internal/core/service"
	"github.com/mateuslh/lealing/internal/platform/logging"
	"github.com/mateuslh/lealing/sdk/protocol"
)

// Options são as escolhas que o binário oferece na linha de comando.
type Options struct {
	// Ephemeral desliga a persistência de favoritos e estatísticas.
	Ephemeral bool
	// Debug eleva o nível de log e liga a validação estrita do registry.
	Debug bool
	// Version aparece na topbar.
	Version string
	// MarketplaceURL permite testar outro índice sem acoplar o core à origem.
	MarketplaceURL string
}

// App é a aplicação montada, pronta para rodar.
type App struct {
	program *tea.Program
	ui      *tui.App
	closers []func() error
}

// Wire monta o grafo de dependências completo.
//
// A ordem segue de fora para dentro: primeiro a infraestrutura (log, disco),
// depois os adapters de saída, os serviços do core e, por último, as
// factories do adapter de entrada. Só este composition root enxerga
// implementações dos dois lados.
func Wire(opts Options) (*App, error) {
	app := &App{}
	complete := false
	defer func() {
		if !complete {
			app.close()
		}
	}()

	platform := currentPlatform()
	directories := directoriesFor(platform)

	// --- Infraestrutura ---
	log := outbound.Logger(logging.NewDiscard())
	if !opts.Ephemeral {
		level := slog.LevelInfo
		if opts.Debug {
			level = slog.LevelDebug
		}
		// Mesmo fora do debug, manifests corrompidos e stderr de tools precisam
		// de um destino persistente que não seja stdout (que pertence à TUI).
		fileLog, err := logging.NewFile(filepath.Join(directories.State, "lealing.log"), level)
		if err != nil {
			return nil, err
		}
		log = fileLog
		app.closers = append(app.closers, fileLog.Close)
	}

	// --- Adapters de saída ---
	// O catálogo é o mesmo em toda plataforma; o filtro é que decide o que
	// esta máquina enxerga. Manter a declaração única evita que a lista de
	// tools se bifurque por sistema operacional.
	providers := catalog.Providers()
	externalTools := externalcatalog.New(externalcatalog.Options{
		Root:       directories.Tools,
		Categories: catalog.Categories(),
		Reserved:   catalog.ReservedIDs(),
		Target:     currentToolTarget(),
		Strict:     opts.Debug,
		Logger:     log,
	})
	providers = append(providers, externalTools)
	repo := registry.New(providers,
		registry.WithLogger(log),
		registry.WithStrict(opts.Debug),
		registry.WithPlatform(platform),
	)

	var usageStore outbound.UsageStore
	if opts.Ephemeral {
		usageStore = persistence.NewMemoryUsage()
	} else {
		// O debounce de meio segundo agrupa rajadas de favoritos em uma
		// única escrita, sem que o usuário perceba atraso.
		file := persistence.NewUsageFile(filepath.Join(directories.Data, "usage.json"), 500*time.Millisecond)
		app.closers = append(app.closers, file.Close)
		usageStore = file
	}

	clock := outbound.SystemClock
	processRuntime := pluginprocess.New(pluginprocess.Config{
		Protocol: protocol.VersionRange{Min: protocol.Version1, Max: protocol.Version1},
		Logger:   log,
	})
	app.closers = append(app.closers, processRuntime.Close)

	// --- Core ---
	// O Searcher cuida apenas da relevância textual; o serviço combina uso,
	// favoritos e recência. Assim o grafo permanece acíclico.
	catalogSvc := service.NewCatalog(repo, search.NewFuzzy(), usageStore, clock)
	prerequisites := service.NewPrerequisites(repo, requirements.NewPathChecker())
	toolManager := newToolManager(directories.Tools)
	marketplaceSvc := newMarketplaceManager(
		opts.Version, opts.MarketplaceURL, toolManager, repo, directories.Cache,
	)

	var toolRunners []outbound.ToolRunner
	launcher := service.NewLauncher(repo, catalogSvc, clock, log, toolRunners...)

	// --- Telas das tools nativas ---
	// Cada entrada liga uma tool do catálogo à tela que a implementa. As
	// dependências de sistema (pmset, sysctl, logs das CLIs) entram aqui e
	// em nenhum outro lugar: as telas só conhecem as portas.
	th := theme.Default()
	deps := tui.Deps{Theme: th}

	userHome, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	native := adaptersFor(platform, clock.Now, userHome)
	capabilities := []string{
		interactive.CapabilityNavigationBack,
		interactive.CapabilityNotificationShow,
		interactive.CapabilityConfirmationRequest,
	}
	if native.host != nil {
		capabilities = append(capabilities,
			interactive.CapabilityClipboardWrite,
			interactive.CapabilityBrowserOpen,
		)
	}
	interactiveTools := interactive.NewService(repo, processRuntime, interactive.ServiceConfig{
		EngineVersion: opts.Version,
		Platform:      currentToolTarget().OS,
		Architecture:  currentToolTarget().Arch,
		DataRoot:      filepath.Join(directories.Data, "tool-data"),
		CacheRoot:     filepath.Join(directories.Cache, "tools"),
		UserHome:      userHome,
		Capabilities:  capabilities,
	})
	hostActions := hostaction.NewService(native.host)
	gitScanner := gitinsight.New(filepath.Join(userHome, "dev"), clock.Now)
	engineeringRunner := devkitadapter.New()

	// O cofre e o índice das contas do Claude Code: o backup do ~/.claude.json
	// e o índice dos perfis vivem no diretório de dados do lealing; as
	// credenciais, no cofre que cada sistema oferece.
	accounts := ccaccount.NewManager(
		claudecli.NewVault(claudecli.CredentialsPath(userHome)),
		claudecli.NewConfigFile(
			claudecli.ConfigPath(userHome),
			filepath.Join(directories.Data, "claude-json.backup"),
		),
		claudecli.NewStore(filepath.Join(directories.Data, "claude-accounts.json")),
		clock.Now,
	)

	// As tools nativas abaixo continuam ligadas individualmente. Tools
	// screen-v1 são reconhecidas pelo runtime do item do catálogo e abertas
	// pela factory genérica da home, sem uma entrada por ID neste mapa.
	screens := tui.Screens{
		"claude-accounts": func() tui.Screen { return accountsscreen.New(deps, accounts, clock.Now) },
		"marketplace":     func() tui.Screen { return marketplacescreen.New(deps, marketplaceSvc) },
		"self-update": func() tui.Screen {
			return updatescreen.New(deps, Updater(opts.Version), userHome, clock.Now)
		},
		"git-dev-radar": func() tui.Screen { return gitinsightscreen.New(deps, gitScanner) },
	}
	for _, definition := range devkit.Definitions() {
		definition := definition
		screens[domain.ToolID(definition.ToolID)] = func() tui.Screen {
			return devkitscreen.New(deps, engineeringRunner, definition)
		}
	}
	if native.inspector != nil {
		screens["system-info"] = func() tui.Screen {
			return sysinfoscreen.New(deps, native.inspector, clock.Now)
		}
	}
	if native.power != nil {
		screens["power-control"] = func() tui.Screen { return powerscreen.New(deps, native.power) }
	}
	if native.repoClone != nil {
		screens["clone-repo-bradesco"] = func() tui.Screen {
			return repoclonescreen.New(deps, native.repoClone)
		}
	}
	if err := validateWiring(context.Background(), repo, screens, toolRunners); err != nil {
		return nil, err
	}

	// --- Adapter de entrada ---
	root := home.New(home.Config{
		Deps:          deps,
		Catalog:       catalogSvc,
		Prefs:         catalogSvc,
		Launcher:      launcher,
		Prerequisites: prerequisites,
		Now:           clock.Now,
		User:          userNameFor(platform),
		Screens:       screens,
		Interactive:   interactiveTools,
		HostActions:   hostActions,
		Marketplace:   marketplaceSvc,
	})

	app.ui = tui.NewApp(th, root)
	app.program = tea.NewProgram(app.ui,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	complete = true
	return app, nil
}

// RenderStatic desenha um frame isolado, sem entrar no loop interativo.
// É o que alimenta as capturas da documentação e permite conferir o layout
// em dimensões que não temos à mão.
func (a *App) RenderStatic(width, height int, keys string) (string, error) {
	defer a.close()
	parsed, err := tui.ParseKeys(keys)
	if err != nil {
		return "", err
	}
	return tui.RenderStatic(a.ui, width, height, parsed, 8), nil
}

// Run entrega o controle ao loop da TUI e libera os recursos ao final.
func (a *App) Run() error {
	defer a.close()
	_, err := a.program.Run()
	return err
}

// close roda os finalizadores na ordem inversa do registro.
func (a *App) close() {
	for i := len(a.closers) - 1; i >= 0; i-- {
		_ = a.closers[i]()
	}
	a.closers = nil
}
