package machine

import (
	"context"
	"os"
	"os/exec"
	"strings"
)

// Executor inicia subprocessos sem shell e somente quando a concessão foi
// negociada. O valor pode ser copiado e configurado por adapter.
type Executor struct {
	allowed bool
	dir     string
	env     []string
}

// Executor devolve um executor ligado às permissões deste ambiente.
func (e Environment) Executor() Executor {
	return Executor{allowed: e.Permissions.Subprocess}
}

// WithDir devolve uma cópia que inicia comandos em dir.
func (e Executor) WithDir(dir string) Executor {
	e.dir = dir
	return e
}

// WithEnv acrescenta variáveis no formato CHAVE=valor ao ambiente herdado.
// As últimas ocorrências vencem, como em os/exec.
func (e Executor) WithEnv(values ...string) Executor {
	e.env = append(append([]string(nil), e.env...), values...)
	return e
}

// Command monta um exec.Cmd cancelável. Nome e argumentos continuam tokens
// separados; a SDK nunca interpola uma linha de shell.
func (e Executor) Command(ctx context.Context, name string, args ...string) (*exec.Cmd, error) {
	if !e.allowed {
		return nil, &PermissionError{Operation: "de subprocesso"}
	}
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = e.dir
	if len(e.env) > 0 {
		cmd.Env = append(os.Environ(), e.env...)
	}
	return cmd, nil
}

// Output executa e captura stdout.
func (e Executor) Output(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd, err := e.Command(ctx, name, args...)
	if err != nil {
		return nil, err
	}
	return cmd.Output()
}

// CombinedOutput executa e captura stdout e stderr na ordem produzida.
func (e Executor) CombinedOutput(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd, err := e.Command(ctx, name, args...)
	if err != nil {
		return nil, err
	}
	return cmd.CombinedOutput()
}

// OutputText é Output com espaços externos removidos.
func (e Executor) OutputText(ctx context.Context, name string, args ...string) (string, error) {
	out, err := e.Output(ctx, name, args...)
	return strings.TrimSpace(string(out)), err
}

// CombinedText é CombinedOutput com espaços externos removidos.
func (e Executor) CombinedText(ctx context.Context, name string, args ...string) (string, error) {
	out, err := e.CombinedOutput(ctx, name, args...)
	return strings.TrimSpace(string(out)), err
}

// ErrorText reduz a saída de um comando à primeira linha útil e usa err como
// fallback. Isso mantém mensagens de adapters curtas sem perder o exit error.
func ErrorText(output []byte, err error) string {
	text := strings.TrimSpace(string(output))
	if text != "" {
		line, _, _ := strings.Cut(text, "\n")
		return strings.TrimSpace(line)
	}
	if err != nil {
		return err.Error()
	}
	return ""
}
