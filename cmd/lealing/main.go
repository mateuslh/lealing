// Command lealing é o centro de comando de tools no terminal.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/mateuslh/lealing/internal/bootstrap"
	"github.com/mateuslh/lealing/internal/core/toolinstall"
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
		debug      = flag.Bool("debug", false, "log em arquivo e validação estrita do catálogo")
		ephemeral  = flag.Bool("ephemeral", false, "não persiste favoritos nem estatísticas")
		showVer    = flag.Bool("version", false, "mostra a versão e sai")
		platforms  = flag.Bool("platforms", false, "mostra em quais sistemas cada tool roda e sai")
		update     = flag.Bool("update", false, "atualiza o lealing pela linha de comando e sai")
		listTools  = flag.Bool("tools", false, "lista as tools externas instaladas e sai")
		install    = flag.String("tool-install", "", "instala ou atualiza uma tool a partir de um diretório local")
		updateTool = flag.String("tool-update", "", "atualiza uma tool a partir de um diretório local")
		checksum   = flag.String("tool-checksum", "", "SHA-256 esperado para -tool-install")
		remove     = flag.String("tool-remove", "", "remove uma tool instalada, preservando-a para recuperação")
		rollback   = flag.String("tool-rollback", "", "troca a tool pela versão anterior instalada")
		validate   = flag.String("tool-validate", "", "valida um manifest ou diretório de tool sem executar o binário")
		render     = flag.String("render", "", "imprime um frame estático no tamanho LxA (ex.: 140x42) e sai")
		keys       = flag.String("keys", "", "teclas aplicadas antes do -render (ex.: \"/git[down]\")")
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

	// A mesma tool "Atualizar o lealing", sem a TUI: é o caminho de quem
	// instalou o binário numa máquina remota e está num terminal sem
	// interface — ou de um cron que quer manter a ferramenta em dia.
	if *update {
		return bootstrap.SelfUpdate(context.Background(), version, os.Stdout)
	}

	toolCommands := 0
	for _, selected := range []bool{*listTools, *install != "", *updateTool != "", *remove != "", *rollback != "", *validate != ""} {
		if selected {
			toolCommands++
		}
	}
	if toolCommands > 1 {
		return fmt.Errorf("use apenas um comando de gerenciamento de tools por vez")
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
		return runToolCommand(context.Background(), bootstrap.ToolManager(), os.Stdout, toolCommand{
			list: *listTools, install: source, checksum: *checksum,
			remove: *remove, rollback: *rollback,
		})
	}
	if *checksum != "" {
		return fmt.Errorf("-tool-checksum exige -tool-install")
	}

	app, err := bootstrap.Wire(bootstrap.Options{
		Debug:     *debug,
		Ephemeral: *ephemeral,
		Version:   version,
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
	list              bool
	install, checksum string
	remove, rollback  string
}

func runToolCommand(ctx context.Context, manager toolinstall.Manager, output io.Writer, command toolCommand) error {
	switch {
	case command.list:
		installed, err := manager.ListInstalled(ctx)
		if err != nil {
			return err
		}
		if len(installed) == 0 {
			_, err = fmt.Fprintln(output, "nenhuma tool externa instalada")
			return err
		}
		for _, tool := range installed {
			previous := ""
			if tool.PreviousVersion != "" {
				previous = " (anterior " + tool.PreviousVersion + ")"
			}
			if _, err := fmt.Fprintf(output, "%s\t%s%s\n", tool.ID, tool.ActiveVersion, previous); err != nil {
				return err
			}
		}
		return nil

	case command.install != "":
		installed, err := manager.InstallLocal(ctx, toolinstall.InstallRequest{
			SourceDir: command.install, ExpectedSHA256: command.checksum,
		})
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(output, "%s@%s instalada em %s (sha256 %s)\n",
			installed.ID, installed.Version, installed.Path, installed.SHA256)
		return err

	case command.rollback != "":
		installed, err := manager.Rollback(ctx, command.rollback)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(output, "%s voltou para %s; %s ficou disponível para refazer a troca\n",
			installed.ID, installed.Version, installed.PreviousVersion)
		return err

	case command.remove != "":
		removed, err := manager.Remove(ctx, command.remove)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(output, "%s removida; recuperação em %s\n", removed.ID, removed.RecoveryDir)
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
