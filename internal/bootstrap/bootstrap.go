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

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/home"
	marketplacescreen "github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/marketplace"
	settingsscreen "github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/settings"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	"github.com/mateuslh/lealing/internal/adapter/outbound/persistence"
	"github.com/mateuslh/lealing/internal/adapter/outbound/pluginprocess"
	"github.com/mateuslh/lealing/internal/adapter/outbound/requirements"
	"github.com/mateuslh/lealing/internal/adapter/outbound/search"
	"github.com/mateuslh/lealing/internal/adapter/outbound/toolstate"
	"github.com/mateuslh/lealing/internal/core/hostaction"
	"github.com/mateuslh/lealing/internal/core/interactive"
	"github.com/mateuslh/lealing/internal/core/port/outbound"
	"github.com/mateuslh/lealing/internal/core/service"
	"github.com/mateuslh/lealing/internal/core/settings"
	"github.com/mateuslh/lealing/internal/core/toolmanage"
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

	// A configuração é lida antes de tudo: ela decide o índice do
	// marketplace, o app do GitHub e o que a home consulta ao abrir. Um erro
	// de leitura não impede a engine de subir — os padrões valem e a tela de
	// configuração mostra a falha —, mas é registrado no log.
	config, configErr := newSettings(directories, opts.Version)

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
	if configErr != nil {
		log.Warn("configuração não pôde ser lida; usando os padrões", "erro", configErr)
	}

	// --- Adapters de saída ---
	// O catálogo é o mesmo em toda plataforma; o filtro é que decide o que
	// esta máquina enxerga. Manter a declaração única evita que a lista de
	// tools se bifurque por sistema operacional.
	repo := newToolRepository(directories, platform, opts.Debug, log)

	var usageStore outbound.UsageStore
	if opts.Ephemeral {
		usageStore = persistence.NewMemoryUsage()
	} else {
		// O debounce de meio segundo agrupa rajadas de favoritos em uma
		// única escrita, sem que o usuário perceba atraso.
		file := persistence.NewUsageFile(filepath.Join(directories.Data, UsageFileName), usageDebounce)
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
	toolManager := newToolManager(directories.Tools)
	toolManagement := toolmanage.NewService(
		repo,
		toolstate.New(filepath.Join(directories.Config, ToolStateFileName)),
		toolManager,
		repo,
	)
	catalogSvc := service.NewCatalog(toolManagement, search.NewFuzzy(), usageStore, clock)
	prerequisites := service.NewPrerequisites(toolManagement, requirements.NewPathChecker())
	// A flag continua vencendo a configuração: quem passou -marketplace-url
	// está testando outro registry agora, e o valor gravado é a preferência
	// de sempre.
	indexURL := opts.MarketplaceURL
	if indexURL == "" || indexURL == DefaultMarketplaceURL {
		indexURL = config.String(settings.KeyMarketplaceIndex)
	}
	marketplaceSvc := newMarketplaceManager(
		opts.Version, indexURL, toolManager, repo, directories,
	)

	var toolRunners []outbound.ToolRunner
	launcher := service.NewLauncher(toolManagement, catalogSvc, clock, log, toolRunners...)

	// A engine não registra factories por tool. Toda interface instalável
	// entra pela tela genérica screen-v1.
	th := theme.Default()
	deps := tui.Deps{Theme: th}

	userHome, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	native := adaptersFor(platform)
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
	interactiveTools := interactive.NewService(toolManagement, processRuntime, interactive.ServiceConfig{
		EngineVersion: opts.Version,
		Platform:      currentToolTarget().OS,
		Architecture:  currentToolTarget().Arch,
		DataRoot:      filepath.Join(directories.Data, "tool-data"),
		CacheRoot:     filepath.Join(directories.Cache, "tools"),
		UserHome:      userHome,
		Capabilities:  capabilities,
	})
	hostActions := hostaction.NewService(native.host)

	if err := validateWiring(context.Background(), repo, toolRunners); err != nil {
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
		Interactive:   interactiveTools,
		HostActions:   hostActions,
		Marketplace:   marketplaceSvc,
		GreetingName: func() string {
			return config.String(settings.KeyGreetingName)
		},
		MarketplaceOnHome: func() bool {
			return config.Bool(settings.KeyMarketplaceOnHome)
		},
		// A loja não entra em `screens`: ela não é uma tool do catálogo, e sim
		// a porta por onde as tools chegam. A home a abre pela vitrine.
		MarketplaceScreen: func() tui.Screen {
			return marketplacescreen.New(deps, marketplaceSvc, toolManagement)
		},
		SettingsScreen: func() tui.Screen { return settingsscreen.New(deps, config) },
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
