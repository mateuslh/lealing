package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
)

type capturingScreen struct{ keys []string }

func (*capturingScreen) ID() ScreenID  { return "capturing" }
func (*capturingScreen) Title() string { return "capturing" }
func (*capturingScreen) Init() tea.Cmd { return nil }
func (s *capturingScreen) Update(message tea.Msg) (Screen, tea.Cmd) {
	if key, ok := message.(tea.KeyMsg); ok {
		s.keys = append(s.keys, key.String())
	}
	return s, nil
}
func (*capturingScreen) View(Frame) string { return "" }
func (*capturingScreen) Hints() []Hint     { return []Hint{{Key: "esc", Label: "voltar"}} }
func (*capturingScreen) Capturing() bool   { return true }

type sizingScreen struct {
	id    ScreenID
	sizes []tea.WindowSizeMsg
}

func (s *sizingScreen) ID() ScreenID  { return s.id }
func (s *sizingScreen) Title() string { return string(s.id) }
func (*sizingScreen) Init() tea.Cmd   { return nil }
func (s *sizingScreen) Update(message tea.Msg) (Screen, tea.Cmd) {
	if size, ok := message.(tea.WindowSizeMsg); ok {
		s.sizes = append(s.sizes, size)
	}
	return s, nil
}
func (*sizingScreen) View(Frame) string { return "" }
func (*sizingScreen) Hints() []Hint     { return nil }

func TestCapturingImpedeTextoDeFecharAplicacao(t *testing.T) {
	screen := &capturingScreen{}
	app := NewApp(theme.Default(), screen)
	_, command := app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if command != nil || app.quitting {
		t.Fatal("q fechou a aplicação enquanto a tela capturava texto")
	}
	if len(screen.keys) != 1 || screen.keys[0] != "q" {
		t.Fatalf("tecla não chegou ao campo: %v", screen.keys)
	}
}

func TestExitMsgFechaAplicacaoEPreservaMensagem(t *testing.T) {
	app := NewApp(theme.Default(), &capturingScreen{})
	message := "lealing atualizado. Você já pode abrir novamente."

	_, command := app.Update(ExitMsg{Message: message})

	if !app.quitting || command == nil {
		t.Fatal("ExitMsg não iniciou o encerramento da aplicação")
	}
	if got := app.ExitMessage(); got != message {
		t.Fatalf("mensagem final = %q, queria %q", got, message)
	}
	if view := app.View(); strings.TrimSpace(view) != "" {
		t.Fatalf("aplicação continuou renderizando durante a saída: %q", view)
	}
}

func TestEntrarESairDeTelaNaoReduzFrameDaHome(t *testing.T) {
	const width, height = 150, 42
	want := tea.WindowSizeMsg{Width: width, Height: height - topbarHeight - statusbarHeight}
	root := &sizingScreen{id: ScreenHome}
	app := NewApp(theme.Default(), root)
	_, _ = app.Update(tea.WindowSizeMsg{Width: width, Height: height})

	for cycle := range 3 {
		tool := &sizingScreen{id: ScreenID("tool/example")}
		model, command := app.Update(NavigateMsg{Screen: tool})
		app = settle(model, []tea.Cmd{command}, 2).(*App)

		if app.width != width || app.height != height {
			t.Fatalf("ciclo %d alterou a janela para %dx%d", cycle+1, app.width, app.height)
		}
		if len(tool.sizes) != 1 || tool.sizes[0] != want {
			t.Fatalf("ciclo %d entregou frame %+v à tool; quero %+v", cycle+1, tool.sizes, want)
		}

		model, command = app.Update(BackMsg{})
		app = settle(model, []tea.Cmd{command}, 2).(*App)
	}

	if got := root.sizes[len(root.sizes)-1]; got != want {
		t.Fatalf("home voltou com frame %+v; quero %+v", got, want)
	}
}
