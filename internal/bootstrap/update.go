package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/mateuslh/lealing/internal/adapter/outbound/selfupdate"
	coreselfupdate "github.com/mateuslh/lealing/internal/core/selfupdate"
)

// Coordenadas de distribuição do projeto.
//
// Vivem no composition root e em nenhum outro lugar: são configuração de
// *onde este binário é publicado*, não domínio. O nome do artefato precisa
// casar com o name_template do .goreleaser.yaml — os dois juntos são o
// contrato entre quem publica e quem se atualiza.
const (
	repoOwner   = "mateuslh"
	repoName    = "lealing"
	modulePath  = "github.com/mateuslh/lealing"
	binaryName  = "lealing"
	mainPackage = "./cmd/lealing"
)

// Updater monta o serviço de atualização.
//
// É exportado porque serve a dois consumidores: a tela da tool e o modo
// `lealing -update`, que atualiza sem abrir a TUI — o caminho de quem
// instalou o binário numa máquina remota e está num terminal sem interface.
func Updater(version string) *coreselfupdate.Service {
	repo := selfupdate.Repo{Owner: repoOwner, Name: repoName}
	return coreselfupdate.NewService(version,
		selfupdate.NewLocator(modulePath),
		selfupdate.NewGitHub(repo),
		selfupdate.NewApplier(repo, binaryName, mainPackage),
	)
}

// SelfUpdate verifica e, se houver o que aplicar, atualiza — escrevendo o
// andamento em out. É o modo não interativo da tool "Atualizar o lealing".
func SelfUpdate(ctx context.Context, version string, out io.Writer) error {
	svc := Updater(version)

	st, err := svc.Check(ctx)
	if err != nil {
		return err
	}

	fmt.Fprintf(out, "instalada: %s (%s)\n", st.Current, st.Install.Mode.Label())
	fmt.Fprintf(out, "publicada: %s — %s\n", orDash(st.Latest.Tag), st.State.Label())

	if !st.CanApply() {
		if st.Install.Mode == coreselfupdate.ModeUnknown {
			fmt.Fprintln(out, "nada a fazer: não reconheci como este binário foi instalado")
			return nil
		}
		fmt.Fprintln(out, "nada a fazer")
		return nil
	}

	outcome, err := svc.Apply(ctx, st)
	if err != nil {
		if errors.Is(err, coreselfupdate.ErrChecksum) {
			return fmt.Errorf("%w — o arquivo baixado foi descartado e nada foi instalado", err)
		}
		return err
	}

	fmt.Fprintf(out, "atualizado: %s → %s\n", orDash(outcome.From), orDash(outcome.To))
	if outcome.Detail != "" {
		fmt.Fprintln(out, outcome.Detail)
	}
	return nil
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
