// Package persistence implementa as portas de armazenamento em disco.
package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/port"
)

// UsageFile implementa port.UsageStore sobre um único arquivo JSON.
//
// A escrita é debounced e atômica: a TUI pode chamar Save a cada toggle de
// favorito sem transformar isso em uma tempestade de fsync, e um crash no
// meio da gravação nunca deixa o arquivo truncado (write em temp + rename).
type UsageFile struct {
	path     string
	debounce time.Duration

	mu      sync.Mutex
	data    map[domain.ToolID]domain.Usage
	loaded  bool
	dirty   bool
	timer   *time.Timer
	flushed chan struct{}
}

var _ port.UsageStore = (*UsageFile)(nil)

// NewUsageFile monta o store. Um debounce zero grava de forma síncrona, o
// que é o comportamento desejado em testes.
func NewUsageFile(path string, debounce time.Duration) *UsageFile {
	return &UsageFile{
		path:     path,
		debounce: debounce,
		data:     make(map[domain.ToolID]domain.Usage),
		flushed:  make(chan struct{}),
	}
}

// record é a forma serializada de domain.Usage. Manter um DTO separado
// impede que uma mudança no domínio quebre arquivos já gravados em disco.
type record struct {
	Runs     int       `json:"runs,omitempty"`
	LastRun  time.Time `json:"last_run,omitempty"`
	Favorite bool      `json:"favorite,omitempty"`
}

type fileFormat struct {
	Version int                      `json:"version"`
	Usage   map[domain.ToolID]record `json:"usage"`
}

const currentVersion = 1

// Load implementa port.UsageStore. Arquivo ausente não é erro: significa
// primeira execução.
func (s *UsageFile) Load(context.Context) (map[domain.ToolID]domain.Usage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.loaded {
		return s.snapshotLocked(), nil
	}

	raw, err := os.ReadFile(s.path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		s.loaded = true
		return s.snapshotLocked(), nil
	case err != nil:
		return nil, err
	}

	var ff fileFormat
	if err := json.Unmarshal(raw, &ff); err != nil {
		// Estado de uso é cache, não fonte da verdade: um arquivo corrompido
		// não pode impedir a TUI de abrir.
		s.loaded = true
		return s.snapshotLocked(), nil
	}

	for id, r := range ff.Usage {
		s.data[id] = domain.Usage{ToolID: id, Runs: r.Runs, LastRun: r.LastRun, Favorite: r.Favorite}
	}
	s.loaded = true
	return s.snapshotLocked(), nil
}

// Save implementa port.UsageStore, agendando a gravação.
func (s *UsageFile) Save(_ context.Context, u domain.Usage) error {
	s.mu.Lock()
	s.data[u.ToolID] = u
	s.dirty = true

	if s.debounce <= 0 {
		defer s.mu.Unlock()
		return s.writeLocked()
	}

	if s.timer == nil {
		s.timer = time.AfterFunc(s.debounce, s.flush)
	} else {
		s.timer.Reset(s.debounce)
	}
	s.mu.Unlock()
	return nil
}

// Close força a gravação pendente. Deve ser chamado no shutdown.
func (s *UsageFile) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if !s.dirty {
		return nil
	}
	return s.writeLocked()
}

func (s *UsageFile) flush() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.timer = nil
	if s.dirty {
		_ = s.writeLocked()
	}
}

// writeLocked grava via arquivo temporário + rename atômico.
func (s *UsageFile) writeLocked() error {
	ff := fileFormat{Version: currentVersion, Usage: make(map[domain.ToolID]record, len(s.data))}
	for id, u := range s.data {
		if u.Runs == 0 && !u.Favorite {
			continue // não persiste linha vazia
		}
		ff.Usage[id] = record{Runs: u.Runs, LastRun: u.LastRun, Favorite: u.Favorite}
	}

	raw, err := json.MarshalIndent(ff, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".usage-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op quando o rename já aconteceu

	if _, err := tmp.Write(raw); err != nil {
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
	if err := os.Rename(tmpName, s.path); err != nil {
		return err
	}

	s.dirty = false
	return nil
}

func (s *UsageFile) snapshotLocked() map[domain.ToolID]domain.Usage {
	out := make(map[domain.ToolID]domain.Usage, len(s.data))
	for id, u := range s.data {
		out[id] = u
	}
	return out
}

// MemoryUsage é um port.UsageStore volátil, útil em testes e no modo
// efêmero (--no-persist).
type MemoryUsage struct {
	mu   sync.RWMutex
	data map[domain.ToolID]domain.Usage
}

var _ port.UsageStore = (*MemoryUsage)(nil)

// NewMemoryUsage monta o store em memória.
func NewMemoryUsage() *MemoryUsage {
	return &MemoryUsage{data: make(map[domain.ToolID]domain.Usage)}
}

// Load implementa port.UsageStore.
func (m *MemoryUsage) Load(context.Context) (map[domain.ToolID]domain.Usage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[domain.ToolID]domain.Usage, len(m.data))
	for k, v := range m.data {
		out[k] = v
	}
	return out, nil
}

// Save implementa port.UsageStore.
func (m *MemoryUsage) Save(_ context.Context, u domain.Usage) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[u.ToolID] = u
	return nil
}
