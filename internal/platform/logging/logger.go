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

	"github.com/mateuslh/lealing/internal/core/port/outbound"
	"github.com/mateuslh/lealing/internal/platform/xdg"
)

// Slog adapta log/slog à porta outbound.Logger.
type Slog struct {
	l      *slog.Logger
	closer io.Closer
}

var _ outbound.Logger = (*Slog)(nil)

// NewFile abre (ou cria) o arquivo de log em modo append.
func NewFile(path string, level slog.Level) (*Slog, error) {
	if err := xdg.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	// Sob sudo o log nasceria com dono root e travaria o `-debug` seguinte,
	// que roda como o usuário e só sabe abrir em append.
	if err := xdg.Adopt(path); err != nil {
		f.Close()
		return nil, err
	}
	handler := slog.NewTextHandler(f, &slog.HandlerOptions{Level: level})
	return &Slog{l: slog.New(handler), closer: f}, nil
}

// NewDiscard devolve um logger que descarta tudo.
func NewDiscard() *Slog {
	return &Slog{l: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

// Debug implementa outbound.Logger.
func (s *Slog) Debug(msg string, kv ...any) { s.l.Debug(msg, kv...) }

// Info implementa outbound.Logger.
func (s *Slog) Info(msg string, kv ...any) { s.l.Info(msg, kv...) }

// Warn implementa outbound.Logger.
func (s *Slog) Warn(msg string, kv ...any) { s.l.Warn(msg, kv...) }

// Error implementa outbound.Logger.
func (s *Slog) Error(msg string, kv ...any) { s.l.Error(msg, kv...) }

// Close libera o arquivo subjacente, se houver.
func (s *Slog) Close() error {
	if s.closer == nil {
		return nil
	}
	return s.closer.Close()
}
