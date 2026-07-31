// Command lealing é o centro de comando de tools no terminal.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/mateuslh/lealing/internal/bootstrap"
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
		debug     = flag.Bool("debug", false, "log em arquivo e validação estrita do catálogo")
		ephemeral = flag.Bool("ephemeral", false, "não persiste favoritos nem estatísticas")
		showVer   = flag.Bool("version", false, "mostra a versão e sai")
		platforms = flag.Bool("platforms", false, "mostra em quais sistemas cada tool roda e sai")
		update    = flag.Bool("update", false, "atualiza o lealing pela linha de comando e sai")
		render    = flag.String("render", "", "imprime um frame estático no tamanho LxA (ex.: 140x42) e sai")
		keys      = flag.String("keys", "", "teclas aplicadas antes do -render (ex.: \"/git[down]\")")
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
