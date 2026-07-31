//go:build darwin

package claudecli

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"sync"

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

// NewVault monta o cofre da plataforma.
func NewVault() ccaccount.Vault {
	return &Vault{keychain: &keychain{service: keychainService}, file: NewFileVault()}
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

// account resolve a conta do item. O Claude Code grava sob o usuário do
// sistema; descobrir a partir do item existente evita chutar errado quando
// não é o caso.
func (k *keychain) account(ctx context.Context, fallback string) string {
	if fallback != "" {
		return fallback
	}
	out, err := exec.CommandContext(ctx, "/usr/bin/security",
		"find-generic-password", "-s", k.service).CombinedOutput()
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
	out, err := exec.CommandContext(ctx, "/usr/bin/security", args...).Output()
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
// O segredo vai pela entrada padrão, não em argumento: `ps` mostra a linha
// de comando de qualquer processo para qualquer usuário da máquina. O
// `security` pede a senha duas vezes (digitar e confirmar), daí as duas
// linhas iguais.
func (k *keychain) set(ctx context.Context, account string, secret []byte) error {
	acct := k.account(ctx, account)
	cmd := exec.CommandContext(ctx, "/usr/bin/security",
		"add-generic-password", "-U", "-s", k.service, "-a", acct, "-w")
	cmd.Stdin = strings.NewReader(string(secret) + "\n" + string(secret) + "\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		return keychainError(out, err)
	}
	return nil
}

// delete remove o item.
func (k *keychain) delete(ctx context.Context, account string) error {
	args := []string{"delete-generic-password", "-s", k.service}
	if account != "" {
		args = append(args, "-a", account)
	}
	out, err := exec.CommandContext(ctx, "/usr/bin/security", args...).CombinedOutput()
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
