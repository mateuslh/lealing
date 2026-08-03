package update

import (
	"context"
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	"github.com/mateuslh/lealing/internal/core/selfupdate"
)

type fakeManager struct {
	status    selfupdate.Status
	checkErr  error
	outcome   selfupdate.Outcome
	applyErr  error
	applyCall int
	checkCall int
}

func (f *fakeManager) Check(context.Context) (selfupdate.Status, error) {
	f.checkCall++
	return f.status, f.checkErr
}

func (f *fakeManager) Apply(context.Context, selfupdate.Status) (selfupdate.Outcome, error) {
	f.applyCall++
	return f.outcome, f.applyErr
}

func deps() tui.Deps { return tui.Deps{Theme: theme.Default()} }

func outdated() selfupdate.Status {
	return selfupdate.Status{
		Install: selfupdate.Install{Mode: selfupdate.ModeRelease, BinaryPath: "/opt/lealing/lealing", Writable: true},
		Current: selfupdate.ParseVersion("v1.0.0"),
		Latest:  selfupdate.Release{Tag: "v1.1.0"},
		State:   selfupdate.StateOutdated,
	}
}

func upToDate() selfupdate.Status {
	return selfupdate.Status{
		Install: selfupdate.Install{Mode: selfupdate.ModeRelease, BinaryPath: "/opt/lealing/lealing", Writable: true},
		Current: selfupdate.ParseVersion("v1.1.0"),
		Latest:  selfupdate.Release{Tag: "v1.1.0"},
		State:   selfupdate.StateUpToDate,
	}
}

// opened traz a tela já com o resultado da verificação inicial aplicado, como
// ela chega depois de Init() rodar de verdade.
func opened(t *testing.T, manager *fakeManager) *Model {
	t.Helper()
	model := New(deps(), manager, "/home/alguem", nil)
	next, _ := update(t, model, model.Init()())
	return next
}

func TestVerificaAoAbrir(t *testing.T) {
	manager := &fakeManager{status: outdated()}
	model := opened(t, manager)

	if manager.checkCall != 1 {
		t.Fatalf("Check chamado %d vezes", manager.checkCall)
	}
	if model.phase != phaseReady {
		t.Fatalf("fase = %v", model.phase)
	}
	if model.status.State != selfupdate.StateOutdated {
		t.Fatalf("estado = %v", model.status.State)
	}
}

func TestSemGerenciadorEntregaErro(t *testing.T) {
	model := New(deps(), nil, "/home/alguem", nil)
	next, _ := update(t, model, model.Init()())

	if !errors.Is(next.err, errNotConfigured) {
		t.Fatalf("err = %v", next.err)
	}
}

func TestUAtualizaQuandoPodeAplicar(t *testing.T) {
	manager := &fakeManager{status: outdated(), outcome: selfupdate.Outcome{To: "v1.1.0", Restart: true}}
	model := opened(t, manager)

	model, command := update(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	if model.phase != phaseApplying {
		t.Fatalf("fase = %v", model.phase)
	}
	if command == nil {
		t.Fatal("u não disparou a aplicação")
	}

	model, command = update(t, model, command())
	if manager.applyCall != 1 {
		t.Fatalf("Apply chamado %d vezes", manager.applyCall)
	}
	if model.phase != phaseDone || model.outcome.To != "v1.1.0" {
		t.Fatalf("fase = %v, outcome = %+v", model.phase, model.outcome)
	}
	if command == nil {
		t.Fatal("atualização concluída não pediu para fechar o lealing")
	}
	message := command()
	exit, ok := message.(tui.ExitMsg)
	if !ok {
		t.Fatalf("comando final devolveu %T, queria tui.ExitMsg", message)
	}
	if want := "lealing atualizado para v1.1.0. Você já pode abrir novamente para usar a versão nova."; exit.Message != want {
		t.Fatalf("mensagem final = %q, queria %q", exit.Message, want)
	}
}

func TestFalhaAoAtualizarMantemOTerminalAberto(t *testing.T) {
	manager := &fakeManager{status: outdated(), applyErr: errors.New("sem rede")}
	model := opened(t, manager)

	model, command := update(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	model, command = update(t, model, command())

	if command != nil {
		t.Fatal("falha na atualização pediu para fechar o lealing")
	}
	if model.phase != phaseDone || model.err == nil {
		t.Fatalf("fase = %v, err = %v", model.phase, model.err)
	}
}

func TestUIgnoradoQuandoNaoPodeAplicar(t *testing.T) {
	manager := &fakeManager{status: upToDate()}
	model := opened(t, manager)

	model, command := update(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	if command != nil {
		t.Fatal("u disparou aplicação sem atualização disponível")
	}
	if model.phase != phaseReady {
		t.Fatalf("fase = %v", model.phase)
	}
}

func TestTeclasIgnoradasDuranteAplicacao(t *testing.T) {
	manager := &fakeManager{status: outdated()}
	model := opened(t, manager)
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})

	model, command := update(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if command != nil || model.phase != phaseApplying {
		t.Fatalf("uma tecla durante a aplicação mudou o estado: fase = %v", model.phase)
	}
}

func TestRReiniciaAVerificacao(t *testing.T) {
	manager := &fakeManager{status: outdated()}
	model := opened(t, manager)
	model.phase, model.err = phaseDone, errors.New("falhou antes")

	model, command := update(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if model.phase != phaseChecking || model.err != nil {
		t.Fatalf("fase = %v, err = %v", model.phase, model.err)
	}
	if command == nil {
		t.Fatal("r não disparou nova verificação")
	}
	command()
	if manager.checkCall != 2 {
		t.Fatalf("Check chamado %d vezes", manager.checkCall)
	}
}

func TestHintsMostramAtualizarSoQuandoPossivel(t *testing.T) {
	hasHint := func(model *Model, key string) bool {
		for _, hint := range model.Hints() {
			if hint.Key == key {
				return true
			}
		}
		return false
	}

	if !hasHint(opened(t, &fakeManager{status: outdated()}), "u") {
		t.Fatal("hint de atualizar ausente com atualização disponível")
	}
	if hasHint(opened(t, &fakeManager{status: upToDate()}), "u") {
		t.Fatal("hint de atualizar presente sem atualização disponível")
	}
}

func TestStatusRefleteAFase(t *testing.T) {
	model := opened(t, &fakeManager{status: outdated()})
	if status, _ := model.Status(); status != selfupdate.StateOutdated.Label() {
		t.Fatalf("status = %q", status)
	}

	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	if status, _ := model.Status(); status != "atualizando…" {
		t.Fatalf("status durante aplicação = %q", status)
	}
}

func update(t *testing.T, model *Model, message tea.Msg) (*Model, tea.Cmd) {
	t.Helper()
	next, command := model.Update(message)
	updated, ok := next.(*Model)
	if !ok {
		t.Fatalf("model = %T", next)
	}
	return updated, command
}
