// Package claudecli fala com os arquivos e cofres que a CLI do Claude Code
// mantém na máquina: a credencial OAuth e o ~/.claude.json.
//
// O que é específico de cada sistema — chaveiro no macOS, arquivo no Windows
// e no Linux — está isolado nos arquivos com build tag; tudo aqui é comum às
// três plataformas.
package claudecli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/mateuslh/lealing/internal/core/ccaccount"
	"github.com/mateuslh/lealing/internal/platform/xdg"
)

// Caminhos que a CLI usa, relativos ao diretório do usuário.
const (
	configName      = ".claude.json"
	credentialsName = ".credentials.json"
	claudeDir       = ".claude"
)

// ConfigPath devolve o arquivo de configuração sob a home recebida.
func ConfigPath(home string) string { return filepath.Join(home, configName) }

// CredentialsPath devolve o arquivo de credencial sob a home recebida.
func CredentialsPath(home string) string {
	return filepath.Join(home, claudeDir, credentialsName)
}

// --- Credencial em arquivo ---------------------------------------------

// FileVault lê e escreve a credencial no arquivo que a CLI usa no Windows e
// no Linux. No macOS ele é o plano B, para quem tem a CLI configurada para
// não usar o chaveiro.
type FileVault struct{ Path string }

var _ ccaccount.Vault = (*FileVault)(nil)

// NewFileVault monta o cofre de arquivo no caminho explícito.
func NewFileVault(path string) *FileVault { return &FileVault{Path: path} }

// Read implementa ccaccount.Vault.
func (v *FileVault) Read(context.Context) (json.RawMessage, error) {
	raw, err := os.ReadFile(v.Path)
	if err != nil {
		return nil, ccaccount.ErrNoActiveSession
	}
	return compact(raw)
}

// Write implementa ccaccount.Vault.
func (v *FileVault) Write(_ context.Context, credential json.RawMessage) error {
	if err := xdg.MkdirAll(filepath.Dir(v.Path), 0o700); err != nil {
		return err
	}
	return writeAtomic(v.Path, credential, 0o600)
}

// Origin implementa ccaccount.Vault.
func (v *FileVault) Origin() string { return v.Path }

// --- ~/.claude.json ----------------------------------------------------

// ConfigFile é a identidade da conta dentro do arquivo de configuração da
// CLI.
//
// O arquivo tem dezenas de chaves — projetos, histórico, contadores — e só
// duas nos interessam. Por isso ele é lido como um mapa cru e reescrito
// inteiro: qualquer campo que não conhecemos atravessa a troca intacto.
type ConfigFile struct {
	Path string
	// BackupPath recebe uma cópia do arquivo antes de cada escrita. Vazio
	// desliga o backup.
	BackupPath string
}

var _ ccaccount.Config = (*ConfigFile)(nil)

// NewConfigFile monta o adapter com caminhos explícitos.
func NewConfigFile(path, backupPath string) *ConfigFile {
	return &ConfigFile{Path: path, BackupPath: backupPath}
}

// accountKey e userKey são as chaves que carregam a identidade.
const (
	accountKey = "oauthAccount"
	userKey    = "userID"
)

// accountScopedCaches são chaves cujo conteúdo pertence à conta anterior:
// cotas, modelos liberados, elegibilidade de plano. Deixá-las no lugar faz a
// CLI abrir mostrando os limites da conta errada até a próxima atualização.
// Todas são caches — a CLI as reconstrói sozinha.
var accountScopedCaches = []string{
	"cachedUsageUtilization",
	"cachedExtraUsageDisabledReason",
	"modelAccessCache",
	"orgModelDefaultCache",
	"additionalModelCostsCache",
	"additionalModelOptionsCache",
	"passesEligibilityCache",
	"passesLastSeenRemaining",
	"clientDataCache",
	"clientDataCacheSlots",
}

// Account implementa ccaccount.Config.
func (c *ConfigFile) Account(context.Context) (json.RawMessage, string, error) {
	doc, err := c.read()
	if err != nil {
		return nil, "", err
	}
	var userID string
	if raw, ok := doc[userKey]; ok {
		_ = json.Unmarshal(raw, &userID)
	}
	return doc[accountKey], userID, nil
}

// SetAccount implementa ccaccount.Config.
func (c *ConfigFile) SetAccount(_ context.Context, account json.RawMessage, userID string) error {
	doc, err := c.read()
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if doc == nil {
		doc = map[string]json.RawMessage{}
	}

	doc[accountKey] = account
	if userID != "" {
		id, err := json.Marshal(userID)
		if err != nil {
			return err
		}
		doc[userKey] = id
	}
	for _, key := range accountScopedCaches {
		delete(doc, key)
	}

	// Dois espaços de indentação: é o formato que a própria CLI grava, e
	// manter igual evita um diff artificial na próxima vez que ela salvar.
	// O escape de HTML é desligado pelo mesmo motivo: `json.Marshal` troca
	// `<`, `>` e `&` por sequências \u — legítimo, mas reescreveria campos
	// que não temos nada a ver com eles.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return err
	}
	out := buf.Bytes()

	if c.BackupPath != "" {
		// O backup é do arquivo como estava, não do que vamos gravar: é o
		// que permite voltar atrás se a troca der errado.
		if raw, err := os.ReadFile(c.Path); err == nil {
			if err := xdg.MkdirAll(filepath.Dir(c.BackupPath), 0o700); err == nil {
				_ = writeAtomic(c.BackupPath, raw, 0o600)
			}
		}
	}
	return writeAtomic(c.Path, out, 0o600)
}

// read carrega o arquivo como mapa de chaves cruas.
func (c *ConfigFile) read() (map[string]json.RawMessage, error) {
	raw, err := os.ReadFile(c.Path)
	if err != nil {
		return nil, err
	}
	doc := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	return doc, nil
}

// --- Utilitários -------------------------------------------------------

// compact valida o JSON e o reduz a uma linha. Uma linha importa: no macOS a
// credencial entra no chaveiro pela entrada padrão do `security`, que lê até
// a quebra de linha.
func compact(raw []byte) (json.RawMessage, error) {
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return nil, err
	}
	return json.RawMessage(buf.Bytes()), nil
}

// writeAtomic grava em um temporário no mesmo diretório e renomeia: um
// crash no meio da escrita nunca deixa o ~/.claude.json truncado, que é o
// tipo de estrago que custa uma reconfiguração inteira ao usuário.
func writeAtomic(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".lealing-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no caminho feliz o rename já levou o arquivo

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return err
	}
	// Um `sudo lealing` que troca de conta não pode deixar o ~/.claude.json
	// com dono root: a CLI do Claude roda como o usuário e pararia de abrir.
	if err := xdg.Adopt(tmpName); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
