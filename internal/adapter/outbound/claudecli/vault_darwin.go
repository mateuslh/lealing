//go:build darwin

package claudecli

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"sync"
	"syscall"

	"github.com/mateuslh/lealing/internal/core/ccaccount"
)

// keychainService é o item onde o Claude Code guarda a credencial no macOS.
const keychainService = "Claude Code-credentials"

// Vault no macOS é o chaveiro, com o arquivo como plano B: instalações
// configuradas para não usar o Keychain guardam a credencial em
// ~/.claude/.credentials.json, e a troca precisa funcionar nos dois casos.
type Vault struct {
	keychain *keychain
	file     *FileVault

	mu     sync.Mutex
	origin string
}

var _ ccaccount.Vault = (*Vault)(nil)

// NewVault monta o cofre da plataforma com o fallback explícito.
func NewVault(credentialsPath string) ccaccount.Vault {
	return &Vault{
		keychain: &keychain{service: keychainService},
		file:     NewFileVault(credentialsPath),
	}
}

// Read implementa ccaccount.Vault.
func (v *Vault) Read(ctx context.Context) (json.RawMessage, error) {
	if raw, err := v.keychain.get(ctx, ""); err == nil {
		if cred, err := compact(raw); err == nil {
			v.setOrigin("chaveiro do macOS")
			return cred, nil
		}
	}
	cred, err := v.file.Read(ctx)
	if err != nil {
		return nil, ccaccount.ErrNoActiveSession
	}
	v.setOrigin(v.file.Path)
	return cred, nil
}

// Write implementa ccaccount.Vault.
//
// Escreve onde a sessão já mora. Gravar nos dois lugares deixaria uma cópia
// em texto puro no disco de quem escolheu o chaveiro — o oposto do que essa
// pessoa pediu.
func (v *Vault) Write(ctx context.Context, credential json.RawMessage) error {
	if _, err := v.keychain.get(ctx, ""); err == nil {
		return v.keychain.set(ctx, "", credential)
	}
	if _, err := os.Stat(v.file.Path); err == nil {
		return v.file.Write(ctx, credential)
	}
	// Sem sessão em lugar nenhum, o chaveiro é o padrão do macOS.
	return v.keychain.set(ctx, "", credential)
}

// Origin implementa ccaccount.Vault.
func (v *Vault) Origin() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.origin == "" {
		return "chaveiro do macOS"
	}
	return v.origin
}

func (v *Vault) setOrigin(o string) {
	v.mu.Lock()
	v.origin = o
	v.mu.Unlock()
}

// --- Chaveiro ----------------------------------------------------------

// keychain é o acesso a itens genéricos via o binário `security`.
//
// Usamos o binário, e não a API Security via cgo, porque a tool inteira é
// Go puro e o custo é um processo por operação — irrelevante para um gesto
// manual do usuário.
type keychain struct{ service string }

// securityCmd monta uma chamada ao `security` fora da sessão de terminal.
//
// Setsid não é detalhe: com um terminal controlador à mão, o `security`
// ignora a entrada padrão e vai direto ao /dev/tty pedir digitação — o
// prompt aparece por cima do frame da TUI e o programa espera para sempre
// por uma tecla que ninguém vai apertar. Vale para gravar (a senha do item)
// e para ler ou apagar com o chaveiro trancado (a senha do chaveiro). Sem
// sessão de terminal, esse caminho não existe: ou ele usa o que mandamos
// pela entrada padrão, ou falha rápido.
func securityCmd(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "/usr/bin/security", args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd
}

// account resolve a conta do item. O Claude Code grava sob o usuário do
// sistema; descobrir a partir do item existente evita chutar errado quando
// não é o caso.
func (k *keychain) account(ctx context.Context, fallback string) string {
	if fallback != "" {
		return fallback
	}
	out, err := securityCmd(ctx, "find-generic-password", "-s", k.service).CombinedOutput()
	if err == nil {
		if acct := parseKeychainAccount(string(out)); acct != "" {
			return acct
		}
	}
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return os.Getenv("USER")
}

// get devolve o segredo do item. A saída nunca pode ir para log — é o
// segredo em texto puro.
func (k *keychain) get(ctx context.Context, account string) ([]byte, error) {
	args := []string{"find-generic-password", "-s", k.service, "-w"}
	if account != "" {
		args = append(args, "-a", account)
	}
	out, err := securityCmd(ctx, args...).Output()
	if err != nil {
		return nil, ccaccount.ErrNoActiveSession
	}
	out = []byte(strings.TrimRight(string(out), "\r\n"))
	if len(out) == 0 {
		return nil, ccaccount.ErrNoActiveSession
	}
	return out, nil
}

// set grava o segredo, atualizando o item se ele já existir.
//
// O segredo vai em hexadecimal (-X), e não pela entrada padrão, porque o
// caminho interativo do `security` passa por um buffer de senha de 128
// bytes: uma credencial do Claude Code tem quase cinco vezes isso e chegaria
// cortada ao meio — um perfil que parece salvo e não serve para nada.
//
// O preço é a linha de comando, que `ps` mostra a qualquer usuário da
// máquina enquanto o processo vive. Não há terceira via: o `security` só
// aceita o segredo por argumento ou pelo buffer truncado, e a API nativa
// exigiria cgo, que custaria a compilação cruzada da tool inteira.
func (k *keychain) set(ctx context.Context, account string, secret []byte) error {
	acct := k.account(ctx, account)
	out, err := securityCmd(ctx, "add-generic-password", "-U",
		"-s", k.service, "-a", acct, "-X", hex.EncodeToString(secret)).CombinedOutput()
	if err != nil {
		// O contexto vencido aqui quase sempre significa que o `security`
		// ficou esperando alguém digitar. Dizer isso é mais útil que repetir
		// "signal: killed".
		if ctx.Err() != nil {
			return errKeychainStuck
		}
		return keychainError(out, err)
	}
	// Chaveiro trancado é o caminho que pediria digitação, e sem tty ele
	// termina sem gravar. O texto do prompt na saída é o que denuncia.
	if strings.Contains(string(out), "password to unlock") {
		return errKeychainLocked
	}

	// Reler é barato e é a única forma de saber que o item ficou íntegro.
	// Foi exatamente um segredo cortado — gravado sem erro nenhum — que
	// produziu o primeiro perfil inútil desta tool.
	stored, err := k.get(ctx, acct)
	if err != nil {
		return errKeychainUnverified
	}
	if !bytes.Equal(stored, secret) {
		return errKeychainTruncated
	}
	return nil
}

// errKeychainLocked pede a ação que resolve, em vez de repetir o erro do
// `security`, que fala de item e não de chaveiro.
var errKeychainLocked = errors.New(
	"o chaveiro está trancado — rode `security unlock-keychain` e tente de novo")

// errKeychainStuck é o tempo esgotado esperando o chaveiro responder.
var errKeychainStuck = errors.New(
	"o chaveiro não respondeu — se houver um diálogo de autorização aberto, responda e tente de novo")

// Os dois abaixo são a releitura falhando. Preferimos recusar a operação a
// deixar no chaveiro um item que só se revelaria quebrado na hora de trocar
// de conta.
var (
	errKeychainUnverified = errors.New("gravou no chaveiro mas não foi possível reler para conferir")
	errKeychainTruncated  = errors.New("o chaveiro devolveu um valor diferente do gravado — nada foi salvo em condições de uso")
)

// delete remove o item.
func (k *keychain) delete(ctx context.Context, account string) error {
	args := []string{"delete-generic-password", "-s", k.service}
	if account != "" {
		args = append(args, "-a", account)
	}
	out, err := securityCmd(ctx, args...).CombinedOutput()
	if err != nil && !strings.Contains(string(out), "could not be found") {
		return keychainError(out, err)
	}
	return nil
}

// keychainError traduz a saída do `security`, que é mais informativa que o
// código de saída — mas nunca contém segredo nestes comandos.
func keychainError(out []byte, err error) error {
	msg := strings.TrimSpace(string(out))
	if msg == "" {
		return err
	}
	return &keychainFailure{msg: msg}
}

type keychainFailure struct{ msg string }

func (e *keychainFailure) Error() string { return "chaveiro: " + e.msg }

// ParseKeychainAccount extrai o atributo "acct" da descrição de um item.
// É exportada porque é a parte com bug em potencial e o teste a exercita com
// uma saída fixa, sem tocar no chaveiro da máquina.
func ParseKeychainAccount(dump string) string { return parseKeychainAccount(dump) }

func parseKeychainAccount(dump string) string {
	const marker = `"acct"<blob>="`
	for line := range strings.SplitSeq(dump, "\n") {
		idx := strings.Index(line, marker)
		if idx < 0 {
			continue
		}
		rest := line[idx+len(marker):]
		if end := strings.LastIndex(rest, `"`); end >= 0 {
			return rest[:end]
		}
	}
	return ""
}
