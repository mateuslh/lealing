// Package outbound declara as portas de saída (driven) da aplicação.
//
// Serviços do core consomem estes contratos; adapters de saída os
// implementam. Nenhum adapter de entrada deve importar este pacote.
package outbound

import (
	"context"
	"time"

	"github.com/mateuslh/lealing/internal/core/domain"
)

// ToolProvider é uma fonte de tools. O registry agrega N providers, o que
// permite crescer para centenas de tools sem um arquivo monolítico.
type ToolProvider interface {
	// Name identifica o provider em logs e diagnósticos.
	Name() string
	// Provide devolve o lote de tools e categorias desta fonte.
	Provide(ctx context.Context) ([]domain.Tool, []domain.Category, error)
}

// ToolRepository é a visão consolidada e somente leitura do acervo.
type ToolRepository interface {
	// All devolve todas as tools registradas.
	All(ctx context.Context) ([]domain.Tool, error)
	// ByID resolve uma tool; devolve domain.ErrToolNotFound.
	ByID(ctx context.Context, id domain.ToolID) (domain.Tool, error)
	// Categories devolve as categorias registradas, já ordenadas.
	Categories(ctx context.Context) ([]domain.Category, error)
}

// ReloadableToolRepository recompõe providers depois de instalar, atualizar,
// remover ou fazer rollback de uma tool. A carga continua lazy no arranque;
// somente uma mutação explícita pede recarga.
type ReloadableToolRepository interface {
	ToolRepository
	Reload(ctx context.Context) error
}

// Searcher ranqueia tools contra um termo de busca.
//
// O adapter é deliberadamente textual: sinais de negócio como frequência,
// recência e favoritos são combinados pelo serviço do catálogo, sem criar
// uma dependência de volta do adapter para o caso de uso.
type Searcher interface {
	Rank(term string, candidates []domain.Tool) []domain.Match
}

// UsageStore persiste favoritos e estatísticas de execução.
type UsageStore interface {
	// Load traz todo o estado de uso, indexado por ToolID.
	Load(ctx context.Context) (map[domain.ToolID]domain.Usage, error)
	// Save grava o estado de uma tool.
	Save(ctx context.Context, u domain.Usage) error
}

// ToolRunner executa uma tool de um Kind específico.
type ToolRunner interface {
	// Supports informa se este runner atende ao Kind.
	Supports(kind domain.Kind) bool
	// Run executa a tool. O canal devolvido emite as transições de fase e é
	// fechado ao término.
	Run(ctx context.Context, t domain.Tool, args domain.Args) (<-chan domain.Session, error)
}

// RequirementChecker verifica as ferramentas externas declaradas no
// catálogo antes que uma tool seja iniciada.
type RequirementChecker interface {
	// Missing devolve somente os requisitos cujo executável não está no PATH.
	Missing(ctx context.Context, requirements []domain.Requirement) []domain.Requirement
}

// Clock abstrai o tempo, para que score, recência e duração sejam testáveis
// sem sleep.
type Clock interface {
	Now() time.Time
}

// ClockFunc adapta uma função a Clock.
type ClockFunc func() time.Time

// Now implementa Clock.
func (f ClockFunc) Now() time.Time { return f() }

// SystemClock é o relógio de produção.
var SystemClock = ClockFunc(time.Now)

// Logger é o contrato mínimo de log usado pelo core. Um adapter concreto
// (slog em arquivo) o implementa — a TUI ocupa o stdout, então log em
// terminal não é opção.
type Logger interface {
	Debug(msg string, kv ...any)
	Info(msg string, kv ...any)
	Warn(msg string, kv ...any)
	Error(msg string, kv ...any)
}
