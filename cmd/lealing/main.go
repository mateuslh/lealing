// Command lealing é o centro de comando de tools no terminal.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/mateuslh/lealing/internal/bootstrap"
	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/marketplace"
	"github.com/mateuslh/lealing/internal/core/toolinstall"
	"github.com/mateuslh/lealing/internal/core/toolmanage"
	"github.com/mateuslh/lealing/internal/core/usersync"
)

// version é sobrescrita no build com -ldflags "-X main.version=…".
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "lealing:", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		debug       = flag.Bool("debug", false, "log em arquivo e validação estrita do catálogo")
		ephemeral   = flag.Bool("ephemeral", false, "não persiste favoritos nem estatísticas")
		showVer     = flag.Bool("version", false, "mostra a versão e sai")
		platforms   = flag.Bool("platforms", false, "mostra em quais sistemas cada tool roda e sai")
		update      = flag.Bool("update", false, "atualiza o lealing pela linha de comando e sai")
		listTools   = flag.Bool("tools", false, "lista todas as tools e seu estado de ativação")
		market      = flag.Bool("marketplace", false, "lista as tools disponíveis no marketplace e sai")
		marketURL   = flag.String("marketplace-url", bootstrap.DefaultMarketplaceURL, "URL HTTPS do índice padrão do marketplace")
		sources     = flag.Bool("sources", false, "lista os repositórios de tools cadastrados e sai")
		sourceAdd   = flag.String("source-add", "", "cadastra um repositório de tools: URL HTTPS do índice ou caminho absoluto local")
		sourceName  = flag.String("source-name", "", "nome do repositório usado com -source-add; vazio deriva do endereço")
		sourceDrop  = flag.String("source-remove", "", "remove um repositório de tools pelo nome")
		sourceOn    = flag.String("source-enable", "", "habilita um repositório de tools pelo nome")
		sourceOff   = flag.String("source-disable", "", "desabilita um repositório sem descartar o cadastro")
		install     = flag.String("tool-install", "", "instala pelo ID do marketplace ou por um diretório local")
		updateTool  = flag.String("tool-update", "", "atualiza pelo ID do marketplace ou por um diretório local")
		checksum    = flag.String("tool-checksum", "", "SHA-256 esperado para -tool-install")
		remove      = flag.String("tool-remove", "", "remove uma tool instalada, preservando-a para recuperação")
		enableTool  = flag.String("tool-enable", "", "ativa uma tool instalada")
		disableTool = flag.String("tool-disable", "", "desativa uma tool sem desinstalá-la")
		rollback    = flag.String("tool-rollback", "", "troca a tool pela versão anterior instalada")
		validate    = flag.String("tool-validate", "", "valida um manifest ou diretório de tool sem executar o binário")
		login       = flag.Bool("login", false, "conecta a conta do GitHub pelo device flow e sai")
		logout      = flag.Bool("logout", false, "desconecta a conta do GitHub desta máquina e sai")
		syncPush    = flag.Bool("sync-push", false, "envia as preferências desta máquina e sai")
		syncPull    = flag.Bool("sync-pull", false, "baixa as preferências do repositório e sai")
		syncStatus  = flag.Bool("sync", false, "mostra o estado da sincronização e sai")
		syncForce   = flag.Bool("force", false, "com -sync-push/-sync-pull, sobrescreve o outro lado")
		render      = flag.String("render", "", "imprime um frame estático no tamanho LxA (ex.: 140x42) e sai")
		keys        = flag.String("keys", "", "teclas aplicadas antes do -render (ex.: \"/git[down]\")")
	)
	flag.Parse()

	if *showVer {
		fmt.Println("lealing", version)
		return nil
	}

	if *platforms {
		matrix, err := bootstrap.SupportMatrix(context.Background())
		if err != nil {
			return err
		}
		fmt.Print(matrix)
		return nil
	}

	// A atualização é uma capacidade administrativa da engine: funciona
	// também numa máquina remota sem TUI ou em um cron.
	if *update {
		return bootstrap.SelfUpdate(context.Background(), version, os.Stdout)
	}

	syncCommands := 0
	for _, selected := range []bool{*login, *logout, *syncPush, *syncPull, *syncStatus} {
		if selected {
			syncCommands++
		}
	}
	if syncCommands > 1 {
		return fmt.Errorf("use apenas um comando de conta por vez")
	}
	if syncCommands == 1 {
		manager, err := bootstrap.SyncManager(version)
		if err != nil {
			return err
		}
		return runSyncCommand(context.Background(), manager, os.Stdout, syncCommand{
			login: *login, logout: *logout,
			push: *syncPush, pull: *syncPull, status: *syncStatus, force: *syncForce,
		})
	}
	if *syncForce {
		return fmt.Errorf("-force exige -sync-push ou -sync-pull")
	}

	toolCommands := 0
	for _, selected := range []bool{
		*listTools, *market, *install != "", *updateTool != "", *remove != "", *enableTool != "", *disableTool != "", *rollback != "", *validate != "",
		*sources, *sourceAdd != "", *sourceDrop != "", *sourceOn != "", *sourceOff != "",
	} {
		if selected {
			toolCommands++
		}
	}
	if toolCommands > 1 {
		return fmt.Errorf("use apenas um comando de gerenciamento de tools por vez")
	}
	if *sourceName != "" && *sourceAdd == "" {
		return fmt.Errorf("-source-name exige -source-add")
	}
	if toolCommands == 1 {
		if *validate != "" {
			id, toolVersion, err := bootstrap.ValidateToolManifest(*validate)
			if err != nil {
				return err
			}
			fmt.Printf("%s@%s: manifest válido\n", id, toolVersion)
			return nil
		}
		source := *install
		if source == "" {
			source = *updateTool
		}
		return runToolCommand(context.Background(), bootstrap.ToolManager(), bootstrap.ToolManagement(), bootstrap.MarketplaceManager(version, *marketURL), os.Stdout, toolCommand{
			list: *listTools, marketplace: *market, install: source, checksum: *checksum,
			remove: *remove, enable: *enableTool, disable: *disableTool, rollback: *rollback,
			sources: *sources, sourceAdd: *sourceAdd, sourceName: *sourceName,
			sourceRemove: *sourceDrop, sourceEnable: *sourceOn, sourceDisable: *sourceOff,
		})
	}
	if *checksum != "" {
		return fmt.Errorf("-tool-checksum exige -tool-install")
	}

	app, err := bootstrap.Wire(bootstrap.Options{
		Debug:          *debug,
		Ephemeral:      *ephemeral,
		Version:        version,
		MarketplaceURL: *marketURL,
	})
	if err != nil {
		return err
	}

	if *render != "" {
		w, h, err := parseSize(*render)
		if err != nil {
			return err
		}
		frame, err := app.RenderStatic(w, h, *keys)
		if err != nil {
			return err
		}
		fmt.Println(frame)
		return nil
	}

	return app.Run()
}

type toolCommand struct {
	list, marketplace bool
	install, checksum string
	remove, rollback  string
	enable, disable   string

	sources               bool
	sourceAdd, sourceName string
	sourceRemove          string
	sourceEnable          string
	sourceDisable         string
}

func runToolCommand(
	ctx context.Context,
	installer toolinstall.Manager,
	tools toolmanage.Manager,
	market marketplace.Manager,
	output io.Writer,
	command toolCommand,
) error {
	switch {
	case command.list:
		installed, err := tools.List(ctx)
		if err != nil {
			return err
		}
		if len(installed) == 0 {
			_, err = fmt.Fprintln(output, "nenhuma tool instalada")
			return err
		}
		for _, tool := range installed {
			state := "ativa"
			if !tool.Enabled {
				state = "desativada"
			}
			previous := ""
			if tool.PreviousVersion != "" {
				previous = " (anterior " + tool.PreviousVersion + ")"
			}
			if _, err := fmt.Fprintf(output, "%s\t%s\t%s\t%s%s\n",
				tool.Tool.ID, state, "externa", tool.ActiveVersion, previous); err != nil {
				return err
			}
		}
		return nil

	case command.marketplace:
		catalog, err := market.Catalog(ctx)
		if err != nil {
			return err
		}
		// As origens que falharam saem antes da lista e marcadas: um índice
		// fora do ar explica uma tool ausente, e silenciar isso faria o
		// usuário procurar o problema na engine.
		for _, status := range catalog.Sources {
			if status.Err != nil {
				if _, err := fmt.Fprintf(output, "aviso: origem %s indisponível: %v\n", status.Name, status.Err); err != nil {
					return err
				}
			}
		}
		if len(catalog.Tools) == 0 {
			_, err = fmt.Fprintln(output, "nenhuma tool compatível disponível no marketplace")
			return err
		}
		for _, tool := range catalog.Tools {
			state := "disponível"
			switch {
			case tool.UpdateAvailable:
				state = "atualização de " + tool.InstalledVersion
			case tool.InstalledVersion != "":
				state = "instalada"
			}
			if _, err := fmt.Fprintf(output, "%s\t%s\t%s\t%s\t%s\n",
				tool.Ref(), tool.Version, tool.DistributionTier, state, tool.Summary); err != nil {
				return err
			}
		}
		return nil

	case command.sources:
		origins, err := market.Sources(ctx)
		if err != nil {
			return err
		}
		for _, origin := range origins {
			state := "habilitada"
			if !origin.Enabled {
				state = "desabilitada"
			}
			scope := "própria"
			if origin.Builtin {
				scope = "embutida"
			}
			if _, err := fmt.Fprintf(output, "%s\t%s\t%s\t%s\t%s\n",
				origin.Name, origin.Kind, state, scope, origin.Ref); err != nil {
				return err
			}
		}
		return nil

	case command.sourceAdd != "":
		origin, err := marketplace.NewOrigin(command.sourceName, "", command.sourceAdd)
		if err != nil {
			return err
		}
		if err := market.AddSource(ctx, origin); err != nil {
			return err
		}
		_, err = fmt.Fprintf(output, "origem %s cadastrada (%s %s)\n", origin.Name, origin.Kind, origin.Ref)
		return err

	case command.sourceRemove != "":
		if err := market.RemoveSource(ctx, command.sourceRemove); err != nil {
			return err
		}
		_, err := fmt.Fprintf(output, "origem %s removida; as tools já instaladas continuam funcionando\n", command.sourceRemove)
		return err

	case command.sourceEnable != "", command.sourceDisable != "":
		name, enabled := command.sourceEnable, true
		if name == "" {
			name, enabled = command.sourceDisable, false
		}
		if err := market.SetSourceEnabled(ctx, name, enabled); err != nil {
			return err
		}
		state := "habilitada"
		if !enabled {
			state = "desabilitada"
		}
		_, err := fmt.Fprintf(output, "origem %s %s\n", name, state)
		return err

	case command.install != "":
		info, statErr := os.Stat(command.install)
		if statErr != nil && !os.IsNotExist(statErr) {
			return statErr
		}
		if os.IsNotExist(statErr) && command.checksum == "" {
			installed, err := market.Install(ctx, command.install)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(output, "%s@%s instalada do marketplace em %s (sha256 %s)\n",
				installed.ID, installed.Version, installed.Path, installed.SHA256)
			return err
		}
		if statErr == nil && !info.IsDir() {
			return fmt.Errorf("pacote local precisa ser um diretório: %s", command.install)
		}
		installed, err := installer.InstallLocal(ctx, toolinstall.InstallRequest{
			SourceDir: command.install, ExpectedSHA256: command.checksum,
		})
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(output, "%s@%s instalada em %s (sha256 %s)\n",
			installed.ID, installed.Version, installed.Path, installed.SHA256)
		return err

	case command.rollback != "":
		installed, err := installer.Rollback(ctx, command.rollback)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(output, "%s voltou para %s; %s ficou disponível para refazer a troca\n",
			installed.ID, installed.Version, installed.PreviousVersion)
		return err

	case command.remove != "":
		removed, err := tools.Remove(ctx, domain.ToolID(command.remove))
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(output, "%s removida; recuperação em %s\n", removed.ID, removed.RecoveryDir)
		return err

	case command.enable != "", command.disable != "":
		id, enabled := command.enable, true
		if id == "" {
			id, enabled = command.disable, false
		}
		if err := tools.SetEnabled(ctx, domain.ToolID(id), enabled); err != nil {
			return err
		}
		state := "ativada"
		if !enabled {
			state = "desativada"
		}
		_, err := fmt.Fprintf(output, "%s %s\n", id, state)
		return err
	}
	return nil
}

// parseSize interpreta o argumento de -render no formato LARGURAxALTURA.
func parseSize(s string) (int, int, error) {
	var w, h int
	if _, err := fmt.Sscanf(s, "%dx%d", &w, &h); err != nil {
		return 0, 0, fmt.Errorf("tamanho inválido %q: use LARGURAxALTURA", s)
	}
	if w < 20 || h < 6 {
		return 0, 0, fmt.Errorf("tamanho %q é pequeno demais", s)
	}
	return w, h, nil
}

type syncCommand struct {
	login, logout      bool
	push, pull, status bool
	force              bool
}

// runSyncCommand é o mesmo caso de uso da tela, sem TUI: serve a máquinas
// remotas e a quem quer sincronizar por cron.
func runSyncCommand(
	ctx context.Context,
	manager usersync.Manager,
	output io.Writer,
	command syncCommand,
) error {
	switch {
	case command.status:
		status, err := manager.Status(ctx)
		if err != nil {
			return err
		}
		if !status.Connected {
			_, err = fmt.Fprintln(output, "nenhuma conta conectada; rode `lealing -login`")
			return err
		}
		if _, err := fmt.Fprintf(output, "conta\t%s\nrepositório\t%s\n",
			status.Identity.Login, status.Repository); err != nil {
			return err
		}
		for _, section := range usersync.AllSections {
			state := "desligada"
			if status.Selection.Enabled(section) {
				state = "ligada"
			}
			if _, err := fmt.Fprintf(output, "%s\t%s\t%d aqui\t%d lá\n",
				section, state, status.Local.Summary()[section], status.Remote.Summary()[section]); err != nil {
				return err
			}
		}
		switch {
		case status.RemoteErr != nil:
			_, err = fmt.Fprintf(output, "aviso: o estado remoto não pôde ser lido: %v\n", status.RemoteErr)
		case status.Diverged:
			_, err = fmt.Fprintln(output, "aviso: o remoto mudou desde a última sincronização desta máquina")
		}
		return err

	case command.login:
		code, err := manager.StartLogin(ctx)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(output, "abra %s e informe o código: %s\n",
			code.VerificationURL, code.UserCode); err != nil {
			return err
		}
		identity, err := manager.CompleteLogin(ctx, code)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(output, "conectado como %s\n", identity.Login)
		return err

	case command.logout:
		if err := manager.Logout(ctx); err != nil {
			return err
		}
		_, err := fmt.Fprintln(output,
			"conta desconectada desta máquina; revogue o acesso em github.com/settings/applications para encerrar o token")
		return err

	case command.push, command.pull:
		result, err := syncTransfer(ctx, manager, command)
		if errors.Is(err, usersync.ErrConflict) {
			return fmt.Errorf(
				"o outro lado mudou desde a última sincronização; repita com -force para sobrescrever")
		}
		if err != nil {
			return err
		}
		if command.push {
			summary := result.State.Summary()
			_, err = fmt.Fprintf(output, "enviado: %d usos, %d origens, %d tools\n",
				summary[usersync.SectionUsage], summary[usersync.SectionSources],
				summary[usersync.SectionTools])
			return err
		}
		_, err = fmt.Fprintf(output, "aplicado: %d usos, %d origens\n",
			result.Applied[usersync.SectionUsage], result.Applied[usersync.SectionSources])
		return err
	}
	return nil
}

func syncTransfer(ctx context.Context, manager usersync.Manager, command syncCommand) (usersync.Result, error) {
	if command.push {
		return manager.Push(ctx, command.force)
	}
	return manager.Pull(ctx, command.force)
}
