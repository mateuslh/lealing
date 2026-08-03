package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/component"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
)

// Contratos opcionais que uma tela pode implementar para alimentar o chrome.
// São opcionais por design: uma tela nova funciona sem implementar nenhum,
// e ganha topbar e statusbar mais ricas conforme adota cada um.
type (
	// statusProvider alimenta o canto direito da barra de status.
	statusProvider interface {
		Status() (string, lipgloss.TerminalColor)
	}
	// metaProvider alimenta os números da topbar.
	metaProvider interface{ Meta() []string }
	// badgeProvider alimenta o selo de destaque da topbar — um aviso pontual
	// que precisa de cor própria, e não do tom neutro que Meta sempre usa.
	badgeProvider interface{ Badge() string }
	// inputCapturer avisa que o teclado está preso em um campo de texto.
	inputCapturer interface{ Capturing() bool }
	// refresher pede para recarregar seus dados ao voltar ao topo da pilha.
	// Telas ocultas não recebem mensagens, então uma tela sem este contrato
	// reaparece com os dados de antes de empilhar — e a home voltaria de uma
	// tool sem ela em "recentes".
	refresher interface{ Refresh() tea.Cmd }
)

// Alturas fixas do chrome: cada faixa ocupa uma linha de conteúdo mais uma
// de régua.
const (
	topbarHeight    = 2
	statusbarHeight = 2
)

// App é o tea.Model raiz. Ele não conhece nenhuma tela concreta: só o
// contrato Screen e o Router. Adicionar a centésima tela ao lealing não
// muda uma linha deste arquivo.
type App struct {
	router *Router
	theme  *theme.Theme

	width, height int
	showHelp      bool
	quitting      bool
	exitMessage   string
}

var _ tea.Model = (*App)(nil)

// NewApp monta a aplicação sobre a tela raiz.
func NewApp(th *theme.Theme, root Screen) *App {
	return &App{router: NewRouter(root), theme: th}
}

// Init implementa tea.Model.
func (a *App) Init() tea.Cmd {
	return tea.Batch(a.router.Current().Init(), tea.EnterAltScreen)
}

// Update implementa tea.Model.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		// Todas as telas da pilha precisam do novo tamanho, não só a visível.
		return a, a.router.Broadcast(a.frameSize(msg))

	case NavigateMsg:
		cmd := a.router.Push(msg.Screen)
		return a, tea.Batch(cmd, a.resizeCurrent())

	case BackMsg:
		if popped, closeCmd := a.router.Pop(); popped {
			return a, tea.Batch(closeCmd, a.refreshCurrent())
		}
		return a, nil

	case ExitMsg:
		a.quitting = true
		a.exitMessage = msg.Message
		return a, tea.Sequence(a.router.CloseAll(), tea.Quit)

	case tea.KeyMsg:
		if cmd, handled := a.handleGlobalKey(msg); handled {
			return a, cmd
		}
	}

	return a, a.router.Update(msg)
}

// ExitMessage devolve o aviso que deve permanecer no terminal depois que a
// tela alternativa for restaurada. Saídas normais não produzem mensagem.
func (a *App) ExitMessage() string { return a.exitMessage }

// handleGlobalKey trata os atalhos que valem em qualquer tela.
//
// Só ctrl+c é incondicional. Os demais cedem quando a tela está capturando
// texto: digitar "q" em um campo de busca não pode fechar o programa.
func (a *App) handleGlobalKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	if msg.String() == "ctrl+c" {
		a.quitting = true
		return tea.Sequence(a.router.CloseAll(), tea.Quit), true
	}

	if a.capturing() {
		return nil, false
	}

	switch msg.String() {
	case "q":
		a.quitting = true
		return tea.Sequence(a.router.CloseAll(), tea.Quit), true

	case "?":
		a.showHelp = !a.showHelp
		return nil, true

	case "esc":
		if a.showHelp {
			a.showHelp = false
			return nil, true
		}
		if popped, closeCmd := a.router.Pop(); popped {
			return tea.Batch(closeCmd, a.refreshCurrent()), true
		}
	}

	return nil, false
}

// refreshCurrent pede à tela que reapareceu que recarregue seus dados.
func (a *App) refreshCurrent() tea.Cmd {
	if r, ok := a.router.Current().(refresher); ok {
		return r.Refresh()
	}
	return nil
}

// capturing consulta a tela atual sobre a captura de teclado.
func (a *App) capturing() bool {
	c, ok := a.router.Current().(inputCapturer)
	return ok && c.Capturing()
}

// frameSize traduz o tamanho da janela no tamanho útil de uma tela.
func (a *App) frameSize(msg tea.WindowSizeMsg) tea.WindowSizeMsg {
	msg.Height = max(msg.Height-topbarHeight-statusbarHeight, 1)
	return msg
}

// resizeCurrent entrega o tamanho útil à tela recém-empilhada.
//
// A mensagem é processada diretamente pelo Router: devolvê-la como tea.Msg
// faria o App confundir um frame já sem o chrome com um novo tamanho do
// terminal e descontar as barras outra vez a cada navegação.
func (a *App) resizeCurrent() tea.Cmd {
	if a.width == 0 {
		return nil
	}
	size := a.frameSize(tea.WindowSizeMsg{Width: a.width, Height: a.height})
	return a.router.Update(size)
}

// View implementa tea.Model.
func (a *App) View() string {
	if a.quitting {
		// Uma última linha limpa evita que o alt-screen deixe resíduo.
		return ""
	}
	if a.width == 0 || a.height == 0 {
		return ""
	}

	current := a.router.Current()
	frame := Frame{
		Width:  a.width,
		Height: max(a.height-topbarHeight-statusbarHeight, 1),
	}

	var meta []string
	if p, ok := current.(metaProvider); ok {
		meta = p.Meta()
	}
	var badge string
	if p, ok := current.(badgeProvider); ok {
		badge = p.Badge()
	}
	top := component.Topbar{Trail: a.router.Trail(), Meta: meta, Badge: badge}.Render(a.theme, a.width)

	body := current.View(frame)
	if a.showHelp {
		body = a.viewHelp(frame)
	}
	body = lipgloss.NewStyle().Width(a.width).Height(frame.Height).MaxHeight(frame.Height).Render(body)

	hints := a.hints(current)
	status, tone := "", lipgloss.TerminalColor(nil)
	if p, ok := current.(statusProvider); ok && !a.showHelp {
		status, tone = p.Status()
	}
	bottom := component.Statusbar{Hints: hints, Right: status, Tone: tone}.Render(a.theme, a.width)

	return lipgloss.JoinVertical(lipgloss.Left, top, body, bottom)
}

// hints combina os atalhos da tela com os globais.
func (a *App) hints(s Screen) []component.Hint {
	screenHints := s.Hints()
	if a.showHelp {
		screenHints = nil
	}

	out := make([]component.Hint, 0, len(screenHints)+2)
	for _, h := range screenHints {
		out = append(out, component.Hint{Key: h.Key, Label: h.Label})
	}
	// Com a tela capturando texto, "?" e "q" viram caracteres digitados.
	// Anunciá-los ali seria mentir sobre o que a tecla faz.
	if !a.capturing() {
		out = append(out,
			component.Hint{Key: "?", Label: "ajuda"},
			component.Hint{Key: "q", Label: "sair"},
		)
	}
	return out
}

// viewHelp desenha o painel de ajuda, que substitui o conteúdo da tela.
func (a *App) viewHelp(f Frame) string {
	th := a.theme

	sections := []struct {
		title string
		rows  [][2]string
	}{
		{"navegação", [][2]string{
			{"↑ ↓ / j k", "mover a seleção"},
			{"← → / h l", "trocar de painel"},
			{"tab / ⇧tab", "ciclar entre painéis"},
			{"g / G", "início / fim da lista"},
			{"esc", "voltar um nível"},
		}},
		{"ações", [][2]string{
			{"↵", "executar a tool selecionada"},
			{"f", "favoritar / desfavoritar"},
			{"r", "recarregar o catálogo"},
		}},
		{"busca", [][2]string{
			{"/ ou ctrl+k", "abrir a busca"},
			{"kind:process", "filtrar por modo de execução"},
			{"cat:cloud", "filtrar por categoria"},
			{"is:fav", "somente favoritas"},
		}},
	}

	keyW := 0
	for _, s := range sections {
		for _, r := range s.rows {
			keyW = max(keyW, lipgloss.Width(r[0]))
		}
	}

	var lines []string
	for i, s := range sections {
		if i > 0 {
			lines = append(lines, "") // respiro entre seções
		}
		lines = append(lines, th.Strong.Render(strings.ToUpper(s.title)), "")
		for _, r := range s.rows {
			key := th.KeyCap.Render(r[0] + strings.Repeat(" ", keyW-lipgloss.Width(r[0])))
			lines = append(lines, key+"  "+th.Dim.Render(r[1]))
		}
	}
	content := strings.Join(lines, "\n")

	panel := component.Panel{
		Title:   "atalhos",
		Glyph:   "⌨",
		Accent:  th.Primary,
		Focused: true,
		Footer:  "? fecha",
		Width:   min(f.Width-8, 60),
		Height:  min(lipgloss.Height(content)+2, f.Height-2),
	}.Render(th, content)

	return lipgloss.Place(f.Width, f.Height, lipgloss.Center, lipgloss.Center, panel)
}
