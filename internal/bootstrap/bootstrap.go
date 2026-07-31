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
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	accountsscreen "github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/ccaccount"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/home"
	powerscreen "github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/power"
	sysinfoscreen "github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/sysinfo"
	tokensscreen "github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/tokens"
	updatescreen "github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/update"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	"github.com/mateuslh/lealing/internal/adapter/outbound/claudecli"
	"github.com/mateuslh/lealing/internal/adapter/outbound/persistence"
	"github.com/mateuslh/lealing/internal/adapter/outbound/registry"
	"github.com/mateuslh/lealing/internal/adapter/outbound/runner"
	"github.com/mateuslh/lealing/internal/adapter/outbound/search"
	"github.com/mateuslh/lealing/internal/adapter/outbound/usage"
	"github.com/mateuslh/lealing/internal/catalog"
	"github.com/mateuslh/lealing/internal/core/ccaccount"
	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/port"
	"github.com/mateuslh/lealing/internal/core/service"
	coretokens "github.com/mateuslh/lealing/internal/core/tokens"
	"github.com/mateuslh/lealing/internal/platform/logging"
	"github.com/mateuslh/lealing/internal/platform/xdg"
)

// Options são as escolhas que o binário oferece na linha de comando.
type Options struct {
	// Ephemeral desliga a persistência de favoritos e estatísticas.
	Ephemeral bool
	// Debug eleva o nível de log e liga a validação estrita do registry.
	Debug bool
	// Version aparece na topbar.
	Version string
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
// depois os adapters de saída, depois os serviços do core e, por último, o
// adapter de entrada — que é o único que enxerga tudo.
func Wire(opts Options) (*App, error) {
	app := &App{}

	// --- Infraestrutura ---
	log := port.Logger(logging.NewDiscard())
	if opts.Debug {
		level := slog.LevelDebug
		fileLog, err := logging.NewFile(filepath.Join(xdg.StateDir(), "lealing.log"), level)
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
	platform := domain.CurrentPlatform()
	repo := registry.New(catalog.Providers(),
		registry.WithLogger(log),
		registry.WithStrict(opts.Debug),
		registry.WithPlatform(platform),
	)

	var usageStore port.UsageStore
	if opts.Ephemeral {
		usageStore = persistence.NewMemoryUsage()
	} else {
		// O debounce de meio segundo agrupa rajadas de favoritos em uma
		// única escrita, sem que o usuário perceba atraso.
		file := persistence.NewUsageFile(filepath.Join(xdg.DataDir(), "usage.json"), 500*time.Millisecond)
		app.closers = append(app.closers, file.Close)
		usageStore = file
	}

	clock := port.SystemClock

	// --- Core ---
	// O buscador precisa consultar o uso para reponderar, e o catálogo
	// precisa do buscador: a dependência circular é quebrada passando uma
	// closure que só resolve depois de ambos existirem.
	var catalogSvc *service.CatalogService
	searcher := search.NewFuzzy(clock, func(id domain.ToolID) domain.Usage {
		if catalogSvc == nil {
			return domain.Usage{}
		}
		// Neste ponto o cache de uso já foi carregado pelo Browse que
		// chamou o Rank, então esta leitura não toca em I/O.
		u, _ := catalogSvc.Usage(context.Background(), id)
		return u
	})
	catalogSvc = service.NewCatalog(repo, searcher, usageStore, clock)

	launcher := service.NewLauncher(repo, catalogSvc, clock, log,
		runner.NewBuiltin(),
		runner.NewPlaceholder(log),
	)

	// --- Telas das tools nativas ---
	// Cada entrada liga uma tool do catálogo à tela que a implementa. As
	// dependências de sistema (pmset, sysctl, logs das CLIs) entram aqui e
	// em nenhum outro lugar: as telas só conhecem as portas.
	th := theme.Default()
	deps := tui.Deps{Theme: th}

	native := adaptersFor(platform, clock.Now)
	usageService := coretokens.NewService(clock.Now, usage.NewClaudeCode(), usage.NewCodex())

	// O cofre e o índice das contas do Claude Code: o backup do ~/.claude.json
	// e o índice dos perfis vivem no diretório de dados do lealing; as
	// credenciais, no cofre que cada sistema oferece.
	accounts := ccaccount.NewManager(
		claudecli.NewVault(),
		claudecli.NewConfigFile(filepath.Join(xdg.DataDir(), "claude-json.backup")),
		claudecli.NewStore(filepath.Join(xdg.DataDir(), "claude-accounts.json")),
		clock.Now,
	)

	// As tools de tokens e de contas leem o que as CLIs escrevem igual em
	// qualquer sistema, então não passam por nativeAdapters. As outras duas só
	// entram se a plataforma tiver quem as atenda — sem isso, a tool abriria
	// uma tela com um adapter nil.
	screens := tui.Screens{
		"token-usage":     func() tui.Screen { return tokensscreen.New(deps, usageService, clock.Now) },
		"claude-accounts": func() tui.Screen { return accountsscreen.New(deps, accounts, clock.Now) },
		"self-update":     func() tui.Screen { return updatescreen.New(deps, Updater(opts.Version)) },
	}
	if native.inspector != nil {
		screens["system-info"] = func() tui.Screen { return sysinfoscreen.New(deps, native.inspector) }
	}
	if native.power != nil {
		screens["power-control"] = func() tui.Screen { return powerscreen.New(deps, native.power) }
	}

	// --- Adapter de entrada ---
	root := home.New(home.Config{
		Deps:     deps,
		Catalog:  catalogSvc,
		Prefs:    catalogSvc,
		Launcher: launcher,
		Clock:    clock,
		Screens:  screens,
	})

	app.ui = tui.NewApp(th, root)
	app.program = tea.NewProgram(app.ui,
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
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
}
