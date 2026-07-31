package confirmation_test

import (
	"reflect"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/confirmation"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	"github.com/mateuslh/lealing/internal/core/domain"
)

type confirmedMsg struct{}

func TestSomenteConfirmacaoExplicitaContinua(t *testing.T) {
	model := confirmation.New(
		tui.Deps{Theme: theme.Default()},
		domain.Tool{ID: "danger", Name: "Perigosa", Risk: domain.RiskDestructive},
		func() tea.Cmd { return func() tea.Msg { return confirmedMsg{} } },
	)
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	message := command()
	sequence := reflect.ValueOf(message)
	if sequence.Kind() != reflect.Slice || sequence.Len() != 2 {
		t.Fatalf("confirmação = %T, quero sequência back + ação", message)
	}
	first := sequence.Index(0).Interface().(tea.Cmd)()
	if _, ok := first.(tui.BackMsg); !ok {
		t.Fatalf("primeira mensagem = %T", first)
	}
	second := sequence.Index(1).Interface().(tea.Cmd)()
	if _, ok := second.(confirmedMsg); !ok {
		t.Fatalf("segunda mensagem = %T", second)
	}
}
