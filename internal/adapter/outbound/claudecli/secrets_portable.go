//go:build !darwin

package claudecli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// fileSecrets guarda os perfis em um arquivo só do dono.
//
// Windows e Linux não têm um cofre que a CLI use, e é ali que ela própria
// deixa a credencial ativa em texto puro. A proteção real é a permissão do
// arquivo; o base64 só evita que um token apareça inteiro num grep casual —
// não é cifra e não pretende ser.
type fileSecrets struct {
	path string
	mu   sync.Mutex
}

var _ secretStore = (*fileSecrets)(nil)

// newSecretStore devolve o cofre da plataforma, ao lado do índice.
func newSecretStore(dir string) secretStore {
	return &fileSecrets{path: filepath.Join(dir, "claude-accounts.secret.json")}
}

// ErrSecretNotFound indica perfil sem credencial guardada.
var ErrSecretNotFound = errors.New("credencial do perfil não encontrada")

func (s *fileSecrets) Get(_ context.Context, key string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	box, err := s.read()
	if err != nil {
		return nil, err
	}
	encoded, ok := box[key]
	if !ok {
		return nil, ErrSecretNotFound
	}
	return base64.StdEncoding.DecodeString(encoded)
}

func (s *fileSecrets) Set(_ context.Context, key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	box, err := s.read()
	if err != nil {
		return err
	}
	box[key] = base64.StdEncoding.EncodeToString(value)
	return s.write(box)
}

func (s *fileSecrets) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	box, err := s.read()
	if err != nil {
		return err
	}
	delete(box, key)
	return s.write(box)
}

func (s *fileSecrets) read() (map[string]string, error) {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, err
	}
	box := map[string]string{}
	if err := json.Unmarshal(raw, &box); err != nil {
		return nil, err
	}
	return box, nil
}

func (s *fileSecrets) write(box map[string]string) error {
	out, err := json.MarshalIndent(box, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	return writeAtomic(s.path, out, 0o600)
}
