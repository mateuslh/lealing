package usersync

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	core "github.com/mateuslh/lealing/internal/core/usersync"
)

type fakeManager struct {
	status     core.Status
	statusErr  error
	code       core.DeviceCode
	identity   core.Identity
	loginErr   error
	result     core.Result
	syncErr    error
	pushes     int
	pulls      int
	forced     bool
	logouts    int
	toggled    core.Section
	toggledTo  bool
	waitCalled bool
}

func (f *fakeManager) Status(context.Context) (core.Status, error) { return f.status, f.statusErr }
func (f *fakeManager) StartLogin(context.Context) (core.DeviceCode, error) {
	return f.code, nil
}
func (f *fakeManager) CompleteLogin(context.Context, core.DeviceCode) (core.Identity, error) {
	f.waitCalled = true
	return f.identity, f.loginErr
}
func (f *fakeManager) Logout(context.Context) error { f.logouts++; return nil }
func (f *fakeManager) Push(_ context.Context, force bool) (core.Result, error) {
	f.pushes, f.forced = f.pushes+1, force
	return f.result, f.syncErr
}
func (f *fakeManager) Pull(_ context.Context, force bool) (core.Result, error) {
	f.pulls, f.forced = f.pulls+1, force
	return f.result, f.syncErr
}
func (f *fakeManager) SetSection(_ context.Context, section core.Section, enabled bool) error {
	f.toggled, f.toggledTo = section, enabled
	return nil
}

func deps() tui.Deps { return tui.Deps{Theme: theme.Default()} }

func fixedNow() time.Time { return time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC) }

func connected() core.Status {
	return core.Status{
		Connected:  true,
		Identity:   core.Identity{Login: "alguem"},
		Repository: core.DefaultRepository,
		Selection:  core.DefaultSelection(),
		Local: core.State{
			Usage: []core.ToolUsage{{ID: "git-dev-radar", Runs: 4, Favorite: true}},
		},
		Remote: core.State{
			Device:    "outro-mac",
			UpdatedAt: fixedNow().Add(-2 * time.Hour),
			Usage:     []core.ToolUsage{{ID: "power-control", Runs: 1}},
		},
	}
}

func loaded(t *testing.T, manager *fakeManager) *Model {
	t.Helper()
	model := New(deps(), manager, nil, fixedNow)
	next, _ := model.Update(model.load()())
	return next.(*Model)
}

func TestSessaoEfemeraExplicaEmVezDeMostrarPainelVazio(t *testing.T) {
	model := New(deps(), nil, nil, fixedNow)
	next, _ := model.Update(model.load()())
	view := next.View(tui.Frame{Width: 90, Height: 24})

	if !strings.Contains(view, "desligada") || !strings.Contains(view, "ephemeral") {
		t.Fatalf("a tela não explicou a ausência do recurso:\n%s", view)
	}
}

func TestDesconectadoConviteAoLogin(t *testing.T) {
	model := loaded(t, &fakeManager{})
	view := model.View(tui.Frame{Width: 90, Height: 24})
	if !strings.Contains(view, "Conecte sua conta do GitHub") {
		t.Fatalf("convite ausente:\n%s", view)
	}
	if !strings.Contains(view, core.DefaultRepository) {
		t.Fatal("a tela não disse onde as preferências vão parar")
	}
}

func TestLoginMostraOCodigoEEsperaAprovacao(t *testing.T) {
	manager := &fakeManager{
		code: core.DeviceCode{
			UserCode: "ABCD-1234", VerificationURL: "https://github.com/login/device",
			ExpiresAt: fixedNow().Add(15 * time.Minute),
		},
		identity: core.Identity{Login: "alguem"},
	}
	model := loaded(t, manager)

	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("Enter não iniciou o login")
	}
	model, _ = update(t, model, command())

	view := model.View(tui.Frame{Width: 90, Height: 26})
	if !strings.Contains(view, "ABCD-1234") {
		t.Fatalf("o código não apareceu:\n%s", view)
	}
	if !strings.Contains(view, "github.com/login/device") {
		t.Fatal("a página de autorização não foi informada")
	}
	if model.phase != phaseAwaiting {
		t.Fatalf("fase = %v", model.phase)
	}
}

func TestConectadoMostraOsDoisLadosEAsSecoes(t *testing.T) {
	model := loaded(t, &fakeManager{status: connected()})
	view := model.View(tui.Frame{Width: 110, Height: 26})

	for _, want := range []string{"alguem", core.DefaultRepository, "outro-mac", "há 2h"} {
		if !strings.Contains(view, want) {
			t.Errorf("a tela não mostrou %q:\n%s", want, view)
		}
	}
	for _, section := range core.AllSections {
		if !strings.Contains(view, section.Label()) {
			t.Errorf("seção %q ausente da tela", section.Label())
		}
	}
}

func TestEspacoLigaEDesligaSecao(t *testing.T) {
	manager := &fakeManager{status: connected()}
	model := loaded(t, manager)

	_, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if command == nil {
		t.Fatal("espaço não alternou a seção")
	}
	command()
	if manager.toggled != core.SectionUsage || manager.toggledTo {
		t.Fatalf("alternou %q para %v", manager.toggled, manager.toggledTo)
	}
}

func TestEnviarEBaixarChamamOCasoDeUso(t *testing.T) {
	manager := &fakeManager{status: connected()}
	model := loaded(t, manager)

	_, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if command == nil {
		t.Fatal("s não enviou")
	}
	command()
	if manager.pushes != 1 || manager.forced {
		t.Fatalf("push = %d, force = %v", manager.pushes, manager.forced)
	}

	model = loaded(t, manager)
	_, command = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	command()
	if manager.pulls != 1 {
		t.Fatalf("pull = %d", manager.pulls)
	}
}

// Divergência não pode virar erro nem sobrescrita: vira pergunta.
func TestConflitoViraPerguntaEForcaSoDepoisDoSim(t *testing.T) {
	manager := &fakeManager{status: connected(), syncErr: core.ErrConflict}
	model := loaded(t, manager)

	_, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	model, _ = update(t, model, command())

	if model.confirm == nil {
		t.Fatal("o conflito não gerou confirmação")
	}
	if model.err != nil {
		t.Fatalf("o conflito virou erro: %v", model.err)
	}
	view := model.View(tui.Frame{Width: 100, Height: 26})
	if !strings.Contains(view, "sobrescrever") {
		t.Fatalf("a confirmação não explicou o que está em jogo:\n%s", view)
	}

	// Recusar não escreve nada.
	next, _ := update(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	if next.confirm != nil || manager.forced {
		t.Fatal("recusar mesmo assim sobrescreveu")
	}

	// Aceitar repete a operação com force.
	manager.syncErr = nil
	model.confirm = &pending{push: true}
	_, command = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	command()
	if !manager.forced {
		t.Fatal("o sim não forçou a escrita")
	}
}

func TestFalhaDeRedeNoStatusApareceSemDerrubarATela(t *testing.T) {
	status := connected()
	status.RemoteErr = errors.New("sem rede")
	model := loaded(t, &fakeManager{status: status})

	view := model.View(tui.Frame{Width: 110, Height: 26})
	if !strings.Contains(view, "sem rede") {
		t.Fatalf("a falha remota não apareceu:\n%s", view)
	}
	if !strings.Contains(view, "alguem") {
		t.Fatal("o restante do painel sumiu junto com a rede")
	}
}

func TestDesconectarChamaLogout(t *testing.T) {
	manager := &fakeManager{status: connected()}
	model := loaded(t, manager)

	_, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	if command == nil {
		t.Fatal("x não desconectou")
	}
	command()
	if manager.logouts != 1 {
		t.Fatalf("logouts = %d", manager.logouts)
	}
}

func TestHintsIncluemEsc(t *testing.T) {
	model := loaded(t, &fakeManager{status: connected()})
	for _, hint := range model.Hints() {
		if strings.Contains(hint.Key, "esc") {
			return
		}
	}
	t.Fatal("hint esc ausente")
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
