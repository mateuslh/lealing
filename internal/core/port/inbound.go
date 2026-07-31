// Package port declara as fronteiras do hexágono.
//
// Portas de entrada (driving) são o que o mundo externo pede ao core; portas
// de saída (driven) são o que o core pede ao mundo externo. Ambas são
// declaradas aqui, do lado de dentro, para que a dependência sempre aponte
// para o domínio — adapters implementam ou consomem, nunca o contrário.
package port

import (
	"context"

	"github.com/mateuslh/lealing/internal/core/domain"
)

// Catalog é a porta de leitura do acervo de tools. É o que a TUI consulta
// para desenhar listas, painéis e a home.
type Catalog interface {
	// Browse aplica filtros, busca e ordenação, devolvendo uma página.
	Browse(ctx context.Context, q domain.Query) (domain.Page, error)
	// Lookup resolve uma tool pelo ID; devolve domain.ErrToolNotFound.
	Lookup(ctx context.Context, id domain.ToolID) (domain.Tool, error)
	// Categories devolve as categorias já ordenadas, com a contagem de tools
	// de cada uma resolvida.
	Categories(ctx context.Context) ([]CategoryView, error)
	// Highlights monta o conteúdo da home (favoritas, recentes, sugestões).
	Highlights(ctx context.Context, limit int) (domain.Highlights, error)
}

// CategoryView é uma categoria enriquecida com dados derivados. Fica na
// porta, e não no domínio, porque a contagem é uma projeção de leitura.
type CategoryView struct {
	domain.Category
	Count int
}

// Launcher é a porta de execução de tools.
type Launcher interface {
	// Launch inicia a execução. Retorna domain.ErrConfirmationRequired
	// quando a tool é destrutiva e opts.Confirmed é falso.
	Launch(ctx context.Context, id domain.ToolID, args domain.Args, opts LaunchOptions) (domain.Session, error)
	// Cancel interrompe uma sessão viva.
	Cancel(ctx context.Context, id domain.SessionID) error
	// Sessions lista as execuções conhecidas, mais recente primeiro.
	Sessions(ctx context.Context) ([]domain.Session, error)
}

// LaunchOptions carrega decisões tomadas pelo driving adapter.
type LaunchOptions struct {
	// Confirmed sinaliza que o usuário já passou pelo diálogo de risco.
	Confirmed bool
	// Detached roda sem tomar o terminal da TUI.
	Detached bool
}

// Preferences é a porta de mutação do estado do usuário.
type Preferences interface {
	// ToggleFavorite inverte o estado de favorito e devolve o novo valor.
	ToggleFavorite(ctx context.Context, id domain.ToolID) (bool, error)
	// Usage devolve as estatísticas de uma tool.
	Usage(ctx context.Context, id domain.ToolID) (domain.Usage, error)
}
