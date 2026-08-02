//go:build darwin

package secrets

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

// keychain guarda cada segredo como um item genérico, com a chave na conta.
type keychain struct{ service string }

var _ Store = (*keychain)(nil)

func newStore(service, _ string) Store { return &keychain{service: service} }

// securityCmd monta uma chamada ao `security` fora da sessão de terminal.
//
// Setsid não é detalhe: com um terminal controlador à mão, o `security`
// ignora a entrada padrão e vai direto ao /dev/tty pedir digitação — o
// prompt aparece por cima do frame da TUI e o programa espera para sempre
// por uma tecla que ninguém vai apertar. Sem sessão de terminal esse caminho
// não existe: ou ele usa o que mandamos, ou falha rápido.
func securityCmd(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "/usr/bin/security", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd
}

func (k *keychain) Get(ctx context.Context, key string) ([]byte, error) {
	if err := checkKey(key); err != nil {
		return nil, err
	}
	// A saída nunca pode ir para log: é o segredo em texto puro.
	out, err := securityCmd(ctx, "find-generic-password", "-s", k.service, "-a", key, "-w").Output()
	if err != nil {
		return nil, ErrNotFound
	}
	value := bytes.TrimRight(out, "\r\n")
	if len(value) == 0 {
		return nil, ErrNotFound
	}
	return value, nil
}

// Set grava o segredo, atualizando o item se ele já existir.
//
// O segredo vai em hexadecimal (-X), e não pela entrada padrão, porque o
// caminho interativo do `security` passa por um buffer de senha de 128
// bytes: um token com refresh passa disso e chegaria cortado ao meio — uma
// credencial que parece salva e não serve para nada.
//
// O preço é a linha de comando, que `ps` mostra enquanto o processo vive.
// Não há terceira via: o `security` só aceita o segredo por argumento ou
// pelo buffer truncado, e a API nativa exigiria cgo, que custaria a
// compilação cruzada do binário inteiro.
func (k *keychain) Set(ctx context.Context, key string, value []byte) error {
	if err := checkKey(key); err != nil {
		return err
	}
	out, err := securityCmd(ctx, "add-generic-password", "-U",
		"-s", k.service, "-a", key, "-X", hex.EncodeToString(value)).CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return errKeychainStuck
		}
		return keychainError(out, err)
	}
	// Chaveiro trancado é o caminho que pediria digitação e, sem tty, ele
	// termina sem gravar. O texto do prompt na saída é o que denuncia.
	if strings.Contains(string(out), "password to unlock") {
		return errKeychainLocked
	}

	// Reler é barato e é a única forma de saber que o item ficou íntegro.
	stored, err := k.Get(ctx, key)
	if err != nil {
		return errKeychainUnverified
	}
	if !bytes.Equal(stored, value) {
		return errKeychainTruncated
	}
	return nil
}

func (k *keychain) Delete(ctx context.Context, key string) error {
	if err := checkKey(key); err != nil {
		return err
	}
	out, err := securityCmd(ctx, "delete-generic-password", "-s", k.service, "-a", key).CombinedOutput()
	if err != nil && !strings.Contains(string(out), "could not be found") {
		return keychainError(out, err)
	}
	return nil
}

// Os erros abaixo pedem a ação que resolve, em vez de repetir a mensagem do
// `security`, que fala de item quando o problema é o chaveiro.
var (
	errKeychainLocked = errors.New(
		"o chaveiro está trancado — rode `security unlock-keychain` e tente de novo")
	errKeychainStuck = errors.New(
		"o chaveiro não respondeu — se houver um diálogo de autorização aberto, responda e tente de novo")
	errKeychainUnverified = errors.New("gravou no chaveiro mas não foi possível reler para conferir")
	errKeychainTruncated  = errors.New("o chaveiro devolveu um valor diferente do gravado — nada foi salvo")
)

func keychainError(out []byte, err error) error {
	if msg := strings.TrimSpace(string(out)); msg != "" {
		return fmt.Errorf("chaveiro: %s", msg)
	}
	return err
}
