// Package marketplacesources persiste as origens de tools escolhidas pelo
// usuário. O arquivo é a única coisa que separa uma instalação do lealing de
// outra: o restante do marketplace é recomposto a cada consulta.
package marketplacesources

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/mateuslh/lealing/internal/core/marketplace"
	"github.com/mateuslh/lealing/internal/platform/xdg"
)

// Version marca o formato do arquivo. Ler um arquivo de uma versão futura é
// recusado em vez de interpretado pela metade: uma origem entendida errado é
// uma origem de software instalada errado.
const Version = 1

// sizeLimit é generoso para uma lista de origens e ainda pequeno o bastante
// para nunca justificar leitura em streaming.
const sizeLimit = 1 << 20

type document struct {
	Version          int                  `json:"version"`
	Custom           []marketplace.Origin `json:"custom"`
	DisabledBuiltins []string             `json:"disabledBuiltins,omitempty"`
}

// Store guarda o estado em um JSON no diretório de configuração.
type Store struct {
	path string
	// mutex serializa leitura e escrita: a TUI carrega a lista enquanto o
	// usuário adiciona uma origem em outro comando da mesma sessão.
	mutex sync.Mutex
}

var _ marketplace.SourceStore = (*Store)(nil)

func New(path string) *Store { return &Store{path: path} }

func (s *Store) Load(context.Context) (marketplace.SourceState, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	raw, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		// Nenhuma personalização ainda: o marketplace vale o que o
		// composition root embutiu.
		return marketplace.SourceState{}, nil
	}
	if err != nil {
		return marketplace.SourceState{}, err
	}
	if len(raw) > sizeLimit {
		return marketplace.SourceState{}, fmt.Errorf("%s excede %d bytes", s.path, sizeLimit)
	}

	var parsed document
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return marketplace.SourceState{}, fmt.Errorf("origens do marketplace em %s: %w", s.path, err)
	}
	if parsed.Version > Version {
		return marketplace.SourceState{}, fmt.Errorf(
			"%s foi escrito por uma versão mais nova do lealing (formato %d)", s.path, parsed.Version)
	}

	custom := make([]marketplace.Origin, 0, len(parsed.Custom))
	for _, origin := range parsed.Custom {
		// Uma entrada corrompida some da lista em vez de derrubar a leitura:
		// perder uma origem editada à mão é recuperável, ficar sem
		// marketplace nenhum não.
		if origin.Validate() == nil {
			custom = append(custom, origin)
		}
	}
	return marketplace.SourceState{Custom: custom, DisabledBuiltins: parsed.DisabledBuiltins}, nil
}

func (s *Store) Save(_ context.Context, state marketplace.SourceState) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	raw, err := json.MarshalIndent(document{
		Version:          Version,
		Custom:           state.Custom,
		DisabledBuiltins: state.DisabledBuiltins,
	}, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')

	directory := filepath.Dir(s.path)
	if err := xdg.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".sources-*.tmp")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name) // no-op depois que o rename levou o arquivo embora

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
