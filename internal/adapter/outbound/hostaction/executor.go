// Package hostaction executa integrações do host com comandos escolhidos no
// composition root. Nenhuma entrada da tool vira linha de shell.
package hostaction

import (
	"context"
	"errors"
	"os/exec"
	"strings"

	corehost "github.com/mateuslh/lealing/internal/core/hostaction"
)

type Command struct {
	Executable string
	Args       []string
}

type Executor struct {
	Clipboard *Command
	Browser   *Command
}

var _ corehost.Executor = (*Executor)(nil)

func New(clipboard, browser *Command) *Executor {
	return &Executor{Clipboard: clipboard, Browser: browser}
}

func (e *Executor) WriteClipboard(ctx context.Context, text string) error {
	if e.Clipboard == nil || e.Clipboard.Executable == "" {
		return errors.New("clipboard não suportado nesta plataforma")
	}
	command := exec.CommandContext(ctx, e.Clipboard.Executable, e.Clipboard.Args...)
	command.Stdin = strings.NewReader(text)
	return command.Run()
}

func (e *Executor) OpenBrowser(ctx context.Context, target string) error {
	if e.Browser == nil || e.Browser.Executable == "" {
		return errors.New("browser não suportado nesta plataforma")
	}
	args := append(append([]string(nil), e.Browser.Args...), target)
	return exec.CommandContext(ctx, e.Browser.Executable, args...).Run()
}
