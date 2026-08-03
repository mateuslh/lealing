package tui

import (
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// mouseScreen é uma tela fake que declara (ou não) precisar de mouse, para
// exercitar o toggle de captura que o Router faz em Push/Pop.
type mouseScreen struct{ wants bool }

func (*mouseScreen) ID() ScreenID                       { return "mouse" }
func (*mouseScreen) Title() string                      { return "mouse" }
func (*mouseScreen) Init() tea.Cmd                      { return nil }
func (s *mouseScreen) Update(tea.Msg) (Screen, tea.Cmd) { return s, nil }
func (*mouseScreen) View(Frame) string                  { return "" }
func (*mouseScreen) Hints() []Hint                      { return nil }
func (s *mouseScreen) WantsMouse() bool                 { return s.wants }

var _ screenMouseUser = (*mouseScreen)(nil)

// collectMsgs roda um Cmd e desmonta BatchMsg recursivamente, do mesmo jeito
// que o runtime do Bubble Tea faria antes de entregar cada mensagem a Update.
func collectMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, child := range batch {
			out = append(out, collectMsgs(child)...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

func hasMsgType(msgs []tea.Msg, sample tea.Msg) bool {
	for _, msg := range msgs {
		if reflect.TypeOf(msg) == reflect.TypeOf(sample) {
			return true
		}
	}
	return false
}

func TestRouterLigaMouseSoParaTelaQueDeclara(t *testing.T) {
	router := NewRouter(&capturingScreen{})

	cmd := router.Push(&mouseScreen{wants: false})
	if hasMsgType(collectMsgs(cmd), tea.EnableMouseCellMotion()) {
		t.Fatal("tela sem WantsMouse não deveria ligar a captura de mouse")
	}

	cmd = router.Push(&mouseScreen{wants: true})
	if !hasMsgType(collectMsgs(cmd), tea.EnableMouseCellMotion()) {
		t.Fatal("tela com WantsMouse deveria ligar a captura de mouse")
	}

	_, popCmd := router.Pop()
	if !hasMsgType(collectMsgs(popCmd), tea.DisableMouse()) {
		t.Fatal("sair de uma tela com WantsMouse para uma que não quer deveria desligar a captura")
	}

	_, popCmd = router.Pop()
	if hasMsgType(collectMsgs(popCmd), tea.DisableMouse()) {
		t.Fatal("sair de uma tela sem WantsMouse não deveria emitir DisableMouse de novo")
	}
}
