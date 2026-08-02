// Package usersyncstore guarda, nesta máquina, o que a sincronização precisa
// lembrar: a credencial (no cofre da plataforma) e os ajustes (em JSON).
//
// Os dois são separados de propósito. O token é segredo e vai para o
// chaveiro; os ajustes são configuração comum, precisam ser legíveis e
// editáveis por quem quiser, e não podem arrastar o segredo junto num
// backup de dotfiles.
package usersyncstore

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/mateuslh/lealing/internal/core/usersync"
	"github.com/mateuslh/lealing/internal/platform/secrets"
	"github.com/mateuslh/lealing/internal/platform/xdg"
)

// credentialKey é a conta usada no cofre. Fixa porque só há uma conta do
// GitHub conectada por vez.
const credentialKey = "github"

// Tokens guarda a credencial no cofre da plataforma.
type Tokens struct{ store secrets.Store }

var _ usersync.TokenStore = (*Tokens)(nil)

func NewTokens(store secrets.Store) *Tokens { return &Tokens{store: store} }

func (t *Tokens) Load(ctx context.Context) (usersync.Credential, error) {
	raw, err := t.store.Get(ctx, credentialKey)
	if errors.Is(err, secrets.ErrNotFound) {
		// Ausência é o estado de quem nunca entrou: o serviço traduz isso em
		// "não conectado", não em falha.
		return usersync.Credential{}, nil
	}
	if err != nil {
		return usersync.Credential{}, err
	}
	var credential usersync.Credential
	if err := json.Unmarshal(raw, &credential); err != nil {
		// Um cofre com lixo dentro é indistinguível de não ter credencial: em
		// ambos o caminho é entrar de novo.
		return usersync.Credential{}, nil
	}
	return credential, nil
}

func (t *Tokens) Save(ctx context.Context, credential usersync.Credential) error {
	raw, err := json.Marshal(credential)
	if err != nil {
		return err
	}
	return t.store.Set(ctx, credentialKey, raw)
}

func (t *Tokens) Delete(ctx context.Context) error {
	return t.store.Delete(ctx, credentialKey)
}

// Version marca o formato do arquivo de ajustes.
const Version = 1

const sizeLimit = 1 << 20

type document struct {
	Version int `json:"version"`
	usersync.Settings
}

// Settings persiste os ajustes em JSON, com escrita atômica.
type Settings struct {
	path  string
	mutex sync.Mutex
}

var _ usersync.SettingsStore = (*Settings)(nil)

func NewSettings(path string) *Settings { return &Settings{path: path} }

func (s *Settings) Load(context.Context) (usersync.Settings, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	raw, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return usersync.Settings{}, nil
	}
	if err != nil {
		return usersync.Settings{}, err
	}
	if len(raw) > sizeLimit {
		return usersync.Settings{}, errors.New(s.path + " é grande demais para ajustes")
	}
	var parsed document
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return usersync.Settings{}, err
	}
	if parsed.Version > Version {
		return usersync.Settings{}, errors.New(
			s.path + " foi escrito por uma versão mais nova do lealing")
	}
	return parsed.Settings, nil
}

func (s *Settings) Save(_ context.Context, settings usersync.Settings) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	raw, err := json.MarshalIndent(document{Version: Version, Settings: settings}, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')

	directory := filepath.Dir(s.path)
	if err := xdg.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".sync-*.tmp")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)

	if _, err := temporary.Write(raw); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := xdg.Adopt(name); err != nil {
		return err
	}
	return os.Rename(name, s.path)
}
