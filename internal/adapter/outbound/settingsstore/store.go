// Package settingsstore guarda os ajustes que o usuário mudou, em JSON.
//
// Só o que difere do padrão vai para o disco. Gravar a configuração inteira
// congelaria decisões que a engine deve poder revisar numa atualização: um
// padrão melhorado nunca chegaria a quem já abriu a tela uma vez.
package settingsstore

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/mateuslh/lealing/internal/core/settings"
	"github.com/mateuslh/lealing/internal/platform/xdg"
)

// Version marca o formato do arquivo.
const Version = 1

const sizeLimit = 1 << 20

type document struct {
	Version int               `json:"version"`
	Values  map[string]string `json:"values"`
}

type Store struct {
	path  string
	mutex sync.Mutex
}

var _ settings.Store = (*Store)(nil)

func New(path string) *Store { return &Store{path: path} }

func (s *Store) Load() (map[string]string, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	raw, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		// Nenhum ajuste ainda: tudo vale o padrão.
		return map[string]string{}, nil
	}
	if err != nil {
		return map[string]string{}, err
	}
	if len(raw) > sizeLimit {
		return map[string]string{}, fmt.Errorf("%s excede %d bytes", s.path, sizeLimit)
	}

	var parsed document
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return map[string]string{}, fmt.Errorf("ajustes em %s: %w", s.path, err)
	}
	if parsed.Version > Version {
		return map[string]string{}, fmt.Errorf(
			"%s foi escrito por uma versão mais nova do lealing (formato %d)", s.path, parsed.Version)
	}
	if parsed.Values == nil {
		parsed.Values = map[string]string{}
	}
	return parsed.Values, nil
}

func (s *Store) Save(values map[string]string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	raw, err := json.MarshalIndent(document{Version: Version, Values: values}, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')

	directory := filepath.Dir(s.path)
	if err := xdg.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".settings-*.tmp")
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
