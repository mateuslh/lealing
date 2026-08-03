package machine_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/mateuslh/lealing/sdk/machine"
	"github.com/mateuslh/lealing/sdk/protocol"
)

func TestExecutorRecusaPermissaoAusente(t *testing.T) {
	executor := machine.NewEnvironment(protocol.Initialize{}).Executor()
	_, err := executor.Output(context.Background(), "comando-que-nao-deve-iniciar")
	if !errors.Is(err, machine.ErrPermissionDenied) {
		t.Fatalf("erro = %v", err)
	}
}

func TestExecutorPreservaArgumentosEContexto(t *testing.T) {
	executor := machine.NewEnvironment(protocol.Initialize{
		Permissions: protocol.Permissions{Subprocess: true},
	}).Executor().WithEnv("LEALING_MACHINE_HELPER=1")

	out, err := executor.OutputText(context.Background(), os.Args[0],
		"-test.run=TestMachineHelperProcess", "--", "um argumento", "$(não-é-shell)")
	if err != nil {
		t.Fatal(err)
	}
	if out != "um argumento|$(não-é-shell)" {
		t.Fatalf("saída = %q", out)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := executor.Output(ctx, os.Args[0], "-test.run=TestMachineHelperProcess"); err == nil {
		t.Error("contexto cancelado não interrompeu o processo")
	}
}

func TestMachineHelperProcess(t *testing.T) {
	if os.Getenv("LEALING_MACHINE_HELPER") != "1" {
		return
	}
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator >= 0 {
		_, _ = os.Stdout.WriteString(strings.Join(os.Args[separator+1:], "|"))
	}
	os.Exit(0)
}

func TestErrorText(t *testing.T) {
	err := errors.New("exit status 1")
	if got := machine.ErrorText([]byte("falhou\ndetalhe"), err); got != "falhou" {
		t.Fatalf("ErrorText = %q", got)
	}
	if got := machine.ErrorText(nil, err); got != err.Error() {
		t.Fatalf("fallback = %q", got)
	}
}
