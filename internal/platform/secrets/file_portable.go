//go:build !darwin

package secrets

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/mateuslh/lealing/internal/platform/xdg"
)

// fileStore guarda os segredos em um arquivo só do dono.
//
// Windows e Linux não oferecem um cofre que um binário sem instalação consiga
// usar de forma portátil. A proteção real aqui é a permissão do arquivo; o
// base64 apenas evita que um token apareça inteiro num grep casual — não é
// cifra e não pretende ser. Quem precisa de mais deve manter o disco cifrado.
type fileStore struct {
	path string
	mu   sync.Mutex
}

var _ Store = (*fileStore)(nil)

func newStore(service, dir string) Store {
	return &fileStore{path: filepath.Join(dir, service+".secret.json")}
}

func (s *fileStore) Get(_ context.Context, key string) ([]byte, error) {
	if err := checkKey(key); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	box, err := s.read()
	if err != nil {
		return nil, err
	}
	encoded, ok := box[key]
	if !ok {
		return nil, ErrNotFound
	}
	return base64.StdEncoding.DecodeString(encoded)
}

func (s *fileStore) Set(_ context.Context, key string, value []byte) error {
	if err := checkKey(key); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	box, err := s.read()
	if err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	if box == nil {
		box = map[string]string{}
	}
	box[key] = base64.StdEncoding.EncodeToString(value)
	return s.write(box)
}

func (s *fileStore) Delete(_ context.Context, key string) error {
	if err := checkKey(key); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	box, err := s.read()
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	delete(box, key)
	return s.write(box)
}

func (s *fileStore) read() (map[string]string, error) {
	raw, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var box map[string]string
	if err := json.Unmarshal(raw, &box); err != nil {
		return nil, err
	}
	return box, nil
}

// write troca o arquivo por um temporário já com a permissão restrita, para
// que não exista instante nenhum em que o segredo esteja legível por outros.
func (s *fileStore) write(box map[string]string) error {
	raw, err := json.Marshal(box)
	if err != nil {
		return err
	}
	if err := xdg.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(s.path), ".secret-*.tmp")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)

	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
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
