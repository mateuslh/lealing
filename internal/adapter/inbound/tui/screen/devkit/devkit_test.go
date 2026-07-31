package devkit

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	core "github.com/mateuslh/lealing/internal/core/devkit"
)

type recordingRunner struct {
	request core.Request
}

func (r *recordingRunner) Run(_ context.Context, request core.Request) (core.Result, error) {
	r.request = request
	return core.Result{Title: "feito", Body: "resultado"}, nil
}

func TestTabTrocaModoEEnterExecutaPelaPorta(t *testing.T) {
	definition, ok := core.DefinitionFor(core.ToolJSON)
	if !ok {
		t.Fatal("definição JSON ausente")
	}
	runner := &recordingRunner{}
	model := New(tui.Deps{Theme: theme.Default()}, runner, definition)

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'{', '}'}})
	model = next.(*Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = next.(*Model)
	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(*Model)
	if cmd == nil {
		t.Fatal("enter não devolveu comando")
	}
	next, _ = model.Update(cmd())
	model = next.(*Model)

	if runner.request.Tool != core.ToolJSON || runner.request.Mode != "minify" || runner.request.Input != "{}" {
		t.Errorf("request = %+v", runner.request)
	}
	if model.result.Title != "feito" {
		t.Errorf("resultado não chegou à tela: %+v", model.result)
	}
}

func TestEscVoltaMesmoComCampoCapturando(t *testing.T) {
	definition, _ := core.DefinitionFor(core.ToolCIDR)
	model := New(tui.Deps{Theme: theme.Default()}, &recordingRunner{}, definition)
	if !model.Capturing() {
		t.Fatal("campo não anunciou captura")
	}
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd == nil {
		t.Fatal("esc não devolveu comando")
	}
	msg := cmd()
	if _, ok := msg.(tui.BackMsg); !ok {
		t.Errorf("esc devolveu %T, quero BackMsg", msg)
	}
}

func TestExecucaoCongelaEntradaEQuebraTokensLongos(t *testing.T) {
	definition, _ := core.DefinitionFor(core.ToolChecksum)
	model := New(tui.Deps{Theme: theme.Default()}, &recordingRunner{}, definition)
	model.input.SetValue("abc")

	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = next.(*Model)
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	model = next.(*Model)
	if model.input.Value() != "abc" {
		t.Errorf("entrada mudou durante execução: %q", model.input.Value())
	}
	if cmd == nil {
		t.Fatal("execução sem comando")
	}

	wrapped := fitText("0123456789abcdef", 4)
	for _, line := range []string{"0123", "4567", "89ab", "cdef"} {
		if !containsLine(wrapped, line) {
			t.Errorf("token não foi quebrado em %q:\n%s", line, wrapped)
		}
	}
	if got := displayText("ok\x1b[31m\tfim"); got != "ok[31m fim" {
		t.Errorf("displayText = %q", got)
	}
}

func containsLine(text, want string) bool {
	for _, line := range strings.Split(text, "\n") {
		if line == want {
			return true
		}
	}
	return false
}
