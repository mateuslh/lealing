// Package tui é o driving adapter do lealing: traduz teclas em chamadas às
// portas de entrada e o resultado de volta em pixels de terminal.
//
// A camada é organizada em três anéis: o chrome (topbar, statusbar, overlays)
// vive no App; a navegação vive no Router; e cada tela é uma unidade
// independente que só conhece o contrato Screen.
package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	"github.com/mateuslh/lealing/internal/core/domain"
)

// ScreenID identifica uma tela para navegação e telemetria.
type ScreenID string

// IDs das telas embutidas.
const (
	ScreenHome ScreenID = "home"
)

// Frame é o retângulo disponível para uma tela, já descontados topbar e
// statusbar. Passar isso explicitamente — em vez de a tela consultar um
// tamanho global — é o que permite compor telas dentro de painéis depois.
type Frame struct {
	Width  int
	Height int
}

// Hint é um atalho anunciado por uma tela para a barra de status.
type Hint struct {
	Key   string
	Label string
}

// Screen é o contrato de uma tela.
//
// Difere de tea.Model em dois pontos deliberados: Update devolve Screen (não
// tea.Model), o que elimina type assertions no router; e View recebe o Frame,
// o que impede uma tela de assumir que ocupa o terminal inteiro.
type Screen interface {
	ID() ScreenID
	// Title alimenta o breadcrumb da topbar.
	Title() string
	Init() tea.Cmd
	Update(msg tea.Msg) (Screen, tea.Cmd)
	View(f Frame) string
	// Hints são os atalhos específicos da tela; os globais são somados pelo
	// App. Ordem importa: os primeiros sobrevivem em telas estreitas.
	Hints() []Hint
}

// Deps é o conjunto de dependências entregue a cada tela na construção.
// Telas recebem portas, nunca implementações concretas — é o que mantém a
// TUI testável com um catálogo fake.
type Deps struct {
	Theme *theme.Theme
}

// ScreenFactory constrói a tela de uma tool sob demanda.
//
// Sob demanda, e não na inicialização, porque cada tela carrega estado e
// dispara I/O ao abrir: instanciar as centenas de telas de um catálogo cheio
// no arranque tornaria a abertura do lealing proporcional ao tamanho do
// acervo.
type ScreenFactory func() Screen

// Screens associa tools às telas que as implementam.
//
// É a ponte entre o catálogo (que só sabe que a tool existe) e a TUI (que
// sabe desenhá-la). Tools não nativas caem no Launcher; uma builtin sem
// factory é recusada por validateWiring antes de a TUI abrir.
type Screens map[domain.ToolID]ScreenFactory

// Open devolve a tela da tool, se houver uma registrada.
func (s Screens) Open(id domain.ToolID) (Screen, bool) {
	factory, ok := s[id]
	if !ok || factory == nil {
		return nil, false
	}
	return factory(), true
}

// Has informa se existe factory sem construir a tela.
//
// O composition root usa esta consulta para validar a ligação entre catálogo
// e TUI antes de abrir o loop; construir a tela ali dispararia estado e I/O
// cedo demais.
func (s Screens) Has(id domain.ToolID) bool {
	factory, ok := s[id]
	return ok && factory != nil
}

// NavigateMsg pede ao router para empilhar uma nova tela.
type NavigateMsg struct{ Screen Screen }

// BackMsg pede ao router para desempilhar a tela atual.
type BackMsg struct{}

// Navigate é o helper para emitir NavigateMsg de dentro de uma tela.
func Navigate(s Screen) tea.Cmd {
	return func() tea.Msg { return NavigateMsg{Screen: s} }
}

// Back é o helper para emitir BackMsg.
func Back() tea.Msg { return BackMsg{} }

// Router mantém a pilha de navegação.
//
// Pilha, e não um único ponteiro, porque o fluxo natural do lealing é
// entrar (categoria → tool → execução) e voltar preservando o estado de
// cada nível — recriar a tela anterior perderia scroll, filtro e seleção.
type Router struct {
	stack []Screen
}

// NewRouter cria o router com a tela raiz.
func NewRouter(root Screen) *Router {
	return &Router{stack: []Screen{root}}
}

// Current devolve a tela no topo da pilha.
func (r *Router) Current() Screen {
	if len(r.stack) == 0 {
		return nil
	}
	return r.stack[len(r.stack)-1]
}

// Push empilha uma tela e devolve o comando de inicialização dela.
func (r *Router) Push(s Screen) tea.Cmd {
	r.stack = append(r.stack, s)
	return s.Init()
}

// Pop desempilha, nunca esvaziando a pilha: a raiz é permanente.
func (r *Router) Pop() bool {
	if len(r.stack) <= 1 {
		return false
	}
	r.stack = r.stack[:len(r.stack)-1]
	return true
}

// Depth é a profundidade atual da navegação.
func (r *Router) Depth() int { return len(r.stack) }

// Trail devolve os títulos da pilha, da raiz ao topo, para o breadcrumb.
func (r *Router) Trail() []string {
	out := make([]string, len(r.stack))
	for i, s := range r.stack {
		out[i] = s.Title()
	}
	return out
}

// Update repassa a mensagem apenas à tela do topo. Telas ocultas não
// processam eventos — o que evita que uma lista fora de vista consuma teclas
// ou dispare requisições.
func (r *Router) Update(msg tea.Msg) tea.Cmd {
	cur := r.Current()
	if cur == nil {
		return nil
	}
	next, cmd := cur.Update(msg)
	r.stack[len(r.stack)-1] = next
	return cmd
}

// Broadcast repassa a mensagem a todas as telas da pilha. Reservado para
// eventos de layout (WindowSizeMsg), que telas ocultas precisam receber para
// não voltarem à vista com o tamanho errado.
func (r *Router) Broadcast(msg tea.Msg) tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(r.stack))
	for i, s := range r.stack {
		next, cmd := s.Update(msg)
		r.stack[i] = next
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}
