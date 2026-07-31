// Package windows implementa as portas que precisam falar com o Windows:
// CIM/WMI para leitura e powercfg para escrita.
//
// O pacote não tem build tag de propósito. Ele compila em qualquer sistema —
// o que roda só no Windows é o processo que ele dispara, não o código Go — e
// é isso que permite exercitar os parsers, que são a parte frágil, na mesma
// suíte que roda no Mac e na CI.
package windows

import (
	"context"
	"os/exec"
	"strings"
)

// powershellArgs são as opções fixas de toda invocação.
//
// -NoProfile evita que o perfil do usuário escreva na saída (um Write-Host
// em $PROFILE quebraria o JSON), e -NonInteractive garante que nada fique
// esperando resposta com a TUI ocupando o terminal.
var powershellArgs = []string{
	"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command",
}

// powershell executa um script e devolve stdout já aparado.
//
// stderr é descartado: estas leituras são best-effort e quem chama trata a
// ausência de valor, não a mensagem.
func powershell(ctx context.Context, script string) (string, error) {
	args := append(append([]string{}, powershellArgs...), script)
	out, err := exec.CommandContext(ctx, "powershell.exe", args...).Output()
	return strings.TrimSpace(string(out)), err
}

// run executa um binário do sistema capturando stdout e stderr juntos, para
// os casos em que a mensagem de erro é o dado que interessa.
func run(ctx context.Context, name string, args ...string) (string, error) {
	out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// firstLine reduz a saída de erro à primeira linha, que é a informativa.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}
