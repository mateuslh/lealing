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
