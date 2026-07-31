package tui

import (
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
