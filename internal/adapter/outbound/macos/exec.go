// Package macos implementa as portas que precisam falar com o sistema
// operacional: sysctl, pmset e o diálogo de autenticação do macOS.
package macos

import (
	"context"
	"os/exec"
	"strings"
)

// run executa um comando e devolve a saída padrão já aparada.
//
// stderr é descartado de propósito: estas leituras são best-effort e o
// chamador trata a ausência de valor, não a mensagem de erro.
func run(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).Output()
	return strings.TrimSpace(string(out)), err
}

// runCombined executa capturando stdout e stderr juntos, para os casos em
// que a mensagem de erro é o dado que interessa.
func runCombined(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// sysctl lê uma chave do sysctl. Devolve string vazia quando indisponível.
func sysctl(ctx context.Context, key string) string {
	out, err := run(ctx, "/usr/sbin/sysctl", "-n", key)
	if err != nil {
		return ""
	}
	return out
}

// firstNonEmpty devolve o primeiro valor preenchido.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
