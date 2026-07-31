// Package logging implementa a porta Logger.
//
// Uma TUI ocupa o terminal inteiro: qualquer escrita em stdout/stderr
// corrompe o frame. Por isso o log vai sempre para arquivo.
package logging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/mateuslh/lealing/internal/core/port"
)

// Slog adapta log/slog à porta port.Logger.
type Slog struct {
	l      *slog.Logger
	closer io.Closer
}

var _ port.Logger = (*Slog)(nil)

// NewFile abre (ou cria) o arquivo de log em modo append.
func NewFile(path string, level slog.Level) (*Slog, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	handler := slog.NewTextHandler(f, &slog.HandlerOptions{Level: level})
	return &Slog{l: slog.New(handler), closer: f}, nil
}

// NewDiscard devolve um logger que descarta tudo.
func NewDiscard() *Slog {
	return &Slog{l: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

// Debug implementa port.Logger.
func (s *Slog) Debug(msg string, kv ...any) { s.l.Debug(msg, kv...) }

// Info implementa port.Logger.
func (s *Slog) Info(msg string, kv ...any) { s.l.Info(msg, kv...) }

// Warn implementa port.Logger.
func (s *Slog) Warn(msg string, kv ...any) { s.l.Warn(msg, kv...) }

// Error implementa port.Logger.
func (s *Slog) Error(msg string, kv ...any) { s.l.Error(msg, kv...) }

// Close libera o arquivo subjacente, se houver.
func (s *Slog) Close() error {
	if s.closer == nil {
		return nil
	}
	return s.closer.Close()
}
