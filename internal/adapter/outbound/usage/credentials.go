package usage

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ErrNoCredentials indica que não há sessão do Claude Code para consultar.
// É esperado — quem nunca autenticou não tem cota para mostrar — e por isso a
// tela trata como ausência de dado, não como falha.
var ErrNoCredentials = errors.New("nenhuma sessão do Claude Code encontrada")

// ErrSessionExpired indica sessão vencida. O Claude Code renova o token
// sozinho no próximo uso; renovar por fora exigiria reescrever a credencial
// dele no chaveiro, o que é pior que pedir para o usuário abrir a CLI.
var ErrSessionExpired = errors.New("sessão do Claude Code expirada — rode `claude` para renovar")

// keychainService é o item onde o Claude Code guarda a credencial no macOS.
const keychainService = "Claude Code-credentials"

// Credential é a sessão OAuth do Claude Code.
type Credential struct {
	AccessToken string
	ExpiresAt   time.Time
	Plan        string
}

// Expired informa se o token já venceu.
func (c Credential) Expired(now time.Time) bool {
	return !c.ExpiresAt.IsZero() && !c.ExpiresAt.After(now)
}

// CredentialSource é de onde a sessão é lida.
type CredentialSource interface {
	Credential(ctx context.Context) (Credential, error)
}

// LocalCredentials lê a sessão do chaveiro do macOS e, se não achar, do
// arquivo que as outras plataformas usam.
type LocalCredentials struct {
	// File é o caminho do fallback em arquivo.
	File string
	// Service permite apontar para outro item do chaveiro em teste.
	Service string
	// Keychain habilita a consulta ao `security` antes do arquivo. O
	// composition root só liga esta opção no macOS.
	Keychain bool
}

var _ CredentialSource = (*LocalCredentials)(nil)

// NewLocalCredentials monta a fonte com dependências explícitas.
func NewLocalCredentials(file string, keychain bool) *LocalCredentials {
	return &LocalCredentials{File: file, Service: keychainService, Keychain: keychain}
}

// Credential implementa CredentialSource.
func (l *LocalCredentials) Credential(ctx context.Context) (Credential, error) {
	if l.Keychain {
		if raw, err := l.fromKeychain(ctx); err == nil {
			return ParseCredential(raw)
		}
	}
	raw, err := os.ReadFile(l.File)
	if err != nil {
		return Credential{}, ErrNoCredentials
	}
	return ParseCredential(raw)
}

// fromKeychain pede o item ao `security`. O binário escreve o segredo em
// stdout, então a saída nunca pode ir para log — nem em caso de erro.
//
// Só existe no macOS: nas outras plataformas o Claude Code guarda a
// credencial no arquivo, e tentar o comando ali só gastaria o custo de um
// processo para falhar.
func (l *LocalCredentials) fromKeychain(ctx context.Context) ([]byte, error) {
	service := l.Service
	if service == "" {
		service = keychainService
	}
	out, err := exec.CommandContext(ctx, "/usr/bin/security",
		"find-generic-password", "-s", service, "-w").Output()
	if err != nil {
		return nil, ErrNoCredentials
	}
	return out, nil
}

// CodexCredential é a sessão OAuth que o Codex grava em ~/.codex/auth.json.
type CodexCredential struct {
	AccessToken string
	AccountID   string
	ExpiresAt   time.Time
}

// Expired informa se o token já venceu.
func (c CodexCredential) Expired(now time.Time) bool {
	return !c.ExpiresAt.IsZero() && !c.ExpiresAt.After(now)
}

// CodexCredentialSource é de onde a sessão do Codex é lida.
type CodexCredentialSource interface {
	CodexCredential(ctx context.Context) (CodexCredential, error)
}

// CodexFile lê a sessão do arquivo que a CLI mantém.
type CodexFile struct{ Path string }

var _ CodexCredentialSource = (*CodexFile)(nil)

// NewCodexFile monta a fonte no caminho recebido do composition root.
func NewCodexFile(path string) *CodexFile { return &CodexFile{Path: path} }

// CodexCredential implementa CodexCredentialSource.
func (c *CodexFile) CodexCredential(context.Context) (CodexCredential, error) {
	raw, err := os.ReadFile(c.Path)
	if err != nil {
		return CodexCredential{}, ErrNoCodexCredentials
	}
	return ParseCodexCredential(raw)
}

// ErrNoCodexCredentials indica que não há sessão do Codex para consultar.
var ErrNoCodexCredentials = errors.New("nenhuma sessão do Codex encontrada")

// ErrCodexSessionExpired pede a renovação, que a própria CLI faz ao rodar.
var ErrCodexSessionExpired = errors.New("sessão do Codex expirada — rode `codex` para renovar")

// ParseCodexCredential extrai a sessão do auth.json.
//
// O prazo de validade não vem em campo próprio: está dentro do JWT, e lê-lo
// evita uma ida à rede que só voltaria com 401.
func ParseCodexCredential(raw []byte) (CodexCredential, error) {
	var payload struct {
		Tokens struct {
			AccessToken string `json:"access_token"`
			AccountID   string `json:"account_id"`
		} `json:"tokens"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return CodexCredential{}, ErrNoCodexCredentials
	}
	if payload.Tokens.AccessToken == "" {
		return CodexCredential{}, ErrNoCodexCredentials
	}
	return CodexCredential{
		AccessToken: payload.Tokens.AccessToken,
		AccountID:   payload.Tokens.AccountID,
		ExpiresAt:   jwtExpiry(payload.Tokens.AccessToken),
	}, nil
}

// jwtExpiry lê o `exp` do corpo de um JWT sem validar a assinatura.
//
// Validar não faria sentido aqui: não temos a chave pública, e quem decide se
// o token vale é o servidor. O campo serve só para evitar a chamada quando já
// sabemos que ela falharia. Token ilegível devolve zero, que significa "sem
// prazo conhecido" — a chamada acontece e o 401 resolve.
func jwtExpiry(token string) time.Time {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return time.Time{}
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return time.Time{}
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(body, &claims); err != nil || claims.Exp <= 0 {
		return time.Time{}
	}
	return time.Unix(claims.Exp, 0)
}

// ParseCredential extrai a sessão do JSON gravado pelo Claude Code.
func ParseCredential(raw []byte) (Credential, error) {
	var payload struct {
		OAuth struct {
			AccessToken string `json:"accessToken"`
			// expiresAt vem em milissegundos desde a época.
			ExpiresAt        int64  `json:"expiresAt"`
			SubscriptionType string `json:"subscriptionType"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return Credential{}, ErrNoCredentials
	}
	if payload.OAuth.AccessToken == "" {
		return Credential{}, ErrNoCredentials
	}

	cred := Credential{
		AccessToken: payload.OAuth.AccessToken,
		Plan:        payload.OAuth.SubscriptionType,
	}
	if ms := payload.OAuth.ExpiresAt; ms > 0 {
		cred.ExpiresAt = time.UnixMilli(ms)
	}
	return cred, nil
}
