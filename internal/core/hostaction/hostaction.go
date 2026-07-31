// Package hostaction aplica políticas às ações que uma tool pede à engine.
package hostaction

import (
	"context"
	"errors"
	"net/url"
	"strings"
)

type Executor interface {
	WriteClipboard(ctx context.Context, text string) error
	OpenBrowser(ctx context.Context, target string) error
}

type Actions interface {
	WriteClipboard(ctx context.Context, text string) error
	OpenBrowser(ctx context.Context, target string) error
}

type Service struct{ executor Executor }

var _ Actions = (*Service)(nil)

func NewService(executor Executor) *Service { return &Service{executor: executor} }

func (s *Service) WriteClipboard(ctx context.Context, text string) error {
	if len(text) > 1<<20 {
		return errors.New("conteúdo do clipboard excede 1 MiB")
	}
	if s.executor == nil {
		return errors.New("clipboard não disponível")
	}
	return s.executor.WriteClipboard(ctx, text)
}

func (s *Service) OpenBrowser(ctx context.Context, target string) error {
	if len(target) > 8<<10 {
		return errors.New("URL grande demais")
	}
	parsed, err := url.Parse(target)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return errors.New("somente URLs http/https são permitidas")
	}
	if strings.ContainsAny(target, "\r\n\x00") {
		return errors.New("URL contém controle inválido")
	}
	if s.executor == nil {
		return errors.New("browser não disponível")
	}
	return s.executor.OpenBrowser(ctx, target)
}
