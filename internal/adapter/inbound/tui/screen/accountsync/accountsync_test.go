package accountsync

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	"github.com/mateuslh/lealing/internal/core/usersync"
)

type fakeManager struct {
	status usersync.Status
	code   usersync.DeviceCode

	pushErr error
	pullErr error

	pushForces []bool
	pullForces []bool
	sections   map[usersync.Section]bool
	loggedOut  bool
	wait       bool
}

type fakeActions struct {
	clipboard string
	browser   string
}

func (f *fakeActions) WriteClipboard(_ context.Context, text string) error {
	f.clipboard = text
	return nil
}

func (f *fakeActions) OpenBrowser(_ context.Context, target string) error {
	f.browser = target
	return nil
}

func (f *fakeManager) Status(context.Context) (usersync.Status, error) { return f.status, nil }
func (f *fakeManager) StartLogin(context.Context) (usersync.DeviceCode, error) {
	return f.code, nil
}
func (f *fakeManager) CompleteLogin(ctx context.Context, _ usersync.DeviceCode) (usersync.Identity, error) {
	if f.wait {
		<-ctx.Done()
		return usersync.Identity{}, ctx.Err()
	}
	f.status.Connected = true
	f.status.Identity = usersync.Identity{Login: "alguem", Name: "Alguém"}
	return f.status.Identity, nil
}
func (f *fakeManager) Logout(context.Context) error {
	f.loggedOut = true
	f.status.Connected = false
	return nil
}
func (f *fakeManager) Push(_ context.Context, force bool) (usersync.Result, error) {
	f.pushForces = append(f.pushForces, force)
	if f.pushErr != nil && !force {
		return usersync.Result{}, f.pushErr
	}
	return usersync.Result{State: f.status.Local}, nil
}
func (f *fakeManager) Pull(_ context.Context, force bool) (usersync.Result, error) {
	f.pullForces = append(f.pullForces, force)
	if f.pullErr != nil && !force {
		return usersync.Result{}, f.pullErr
	}
	return usersync.Result{Applied: usersync.Applied{usersync.SectionUsage: 1}}, nil
}
func (f *fakeManager) SetSection(_ context.Context, section usersync.Section, enabled bool) error {
	if f.sections == nil {
		f.sections = map[usersync.Section]bool{}
	}
	f.sections[section] = enabled
	return nil
}

func connectedStatus() usersync.Status {
	return usersync.Status{
		Connected: true, Identity: usersync.Identity{Login: "alguem", Name: "Alguém"},
		Repository: "lealing-state", Selection: usersync.DefaultSelection(),
		LastSync: time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC),
		Local: usersync.State{
			Usage:   []usersync.ToolUsage{{Host: "lealing", ID: "example-tool"}},
			Sources: []usersync.MarketplaceSource{{Name: "example", Kind: "local", Ref: "/tmp/example"}},
			Tools:   []usersync.InstalledTool{{Host: "lealing", ID: "another-tool", Version: "1.0.0"}},
		},
		Remote: usersync.State{Usage: []usersync.ToolUsage{{Host: "lealing", ID: "example-tool"}}},
	}
}

func testDeps() tui.Deps { return tui.Deps{Theme: theme.Default()} }

func loadedModel(t *testing.T, manager *fakeManager) *Model {
	t.Helper()
	model := New(testDeps(), manager, nil)
	next, _ := model.Update(model.Init()())
	return next.(*Model)
}

func loadedModelWithActions(t *testing.T, manager *fakeManager, actions *fakeActions) *Model {
	t.Helper()
	model := New(testDeps(), manager, actions)
	next, _ := model.Update(model.Init()())
	return next.(*Model)
}

func updateModel(t *testing.T, model *Model, message tea.Msg) (*Model, tea.Cmd) {
	t.Helper()
	next, command := model.Update(message)
	updated, ok := next.(*Model)
	if !ok {
		t.Fatalf("model = %T", next)
	}
	return updated, command
}

func TestContaDesconectadaConduzDeviceFlowAteOStatusConectado(t *testing.T) {
	actions := &fakeActions{}
	manager := &fakeManager{status: usersync.Status{
		Local: usersync.State{Usage: []usersync.ToolUsage{{Host: "lealing", ID: "example-tool"}}},
	}, code: usersync.DeviceCode{
		UserCode: "ABCD-1234", VerificationURL: "https://github.com/login/device",
		ExpiresAt: time.Now().Add(time.Minute),
	}}
	model := loadedModelWithActions(t, manager, actions)
	if view := model.View(tui.Frame{Width: 100, Height: 30}); !strings.Contains(view, "Nenhuma conta") {
		t.Fatalf("estado desconectado ausente:\n%s", view)
	}

	model, command := updateModel(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	model, command = updateModel(t, model, command())
	batchMessage := command()
	batch, ok := batchMessage.(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("início da espera = %T com %d comandos", batchMessage, len(batch))
	}
	if actions.clipboard != "" {
		t.Fatal("clipboard foi alterado fora de tea.Cmd")
	}
	model, _ = updateModel(t, model, batch[0]())
	view := model.View(tui.Frame{Width: 100, Height: 30})
	if !strings.Contains(view, "ABCD-1234") || !strings.Contains(view, "github.com/login/device") ||
		!strings.Contains(view, "Pressione o") || !strings.Contains(view, "código copiado") {
		t.Fatalf("device flow incompleto:\n%s", view)
	}
	if actions.clipboard != "ABCD-1234" {
		t.Fatalf("clipboard = %q", actions.clipboard)
	}

	model, openCommand := updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if actions.browser != "" {
		t.Fatal("navegador foi aberto fora de tea.Cmd")
	}
	model, _ = updateModel(t, model, openCommand())
	if actions.browser != "https://github.com/login/device" {
		t.Fatalf("navegador = %q", actions.browser)
	}

	model, command = updateModel(t, model, batch[1]())
	if command == nil {
		t.Fatal("login concluído não recarregou o status")
	}
	model, _ = updateModel(t, model, command())
	if view := model.View(tui.Frame{Width: 100, Height: 30}); !strings.Contains(view, "@alguem") {
		t.Fatalf("conta conectada ausente:\n%s", view)
	}
}

func TestSecoesSaoAlternadasPelaPortaDeEntrada(t *testing.T) {
	manager := &fakeManager{status: connectedStatus()}
	model := loadedModel(t, manager)

	model, command := updateModel(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("alternar seção não gerou comando")
	}
	model, _ = updateModel(t, model, command())
	if manager.sections[usersync.SectionUsage] {
		t.Fatal("seção ligada não foi desligada")
	}
	if model.status.Selection.Enabled(usersync.SectionUsage) {
		t.Fatal("retrato da tela não acompanhou a gravação")
	}
}

func TestPushExigeConfirmacaoEConflitoExigeForcaExplicita(t *testing.T) {
	manager := &fakeManager{status: connectedStatus(), pushErr: usersync.ErrConflict}
	model := loadedModel(t, manager)
	model.cursor = len(usersync.AllSections)

	model, command := updateModel(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if command != nil || model.confirmation == nil || len(manager.pushForces) != 0 {
		t.Fatal("push começou antes da confirmação")
	}
	model, command = updateModel(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	model, _ = updateModel(t, model, command())
	if model.confirmation == nil || !model.confirmation.force {
		t.Fatal("conflito não abriu confirmação de sobrescrita")
	}

	model, command = updateModel(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	model, command = updateModel(t, model, command())
	if len(manager.pushForces) != 2 || manager.pushForces[0] || !manager.pushForces[1] {
		t.Fatalf("tentativas de push = %v", manager.pushForces)
	}
	if command == nil {
		t.Fatal("push concluído não recarregou o status")
	}
}

func TestPullELogoutMostramConfirmacaoAntesDeAlterarEstado(t *testing.T) {
	manager := &fakeManager{status: connectedStatus()}
	model := loadedModel(t, manager)

	model.cursor = len(usersync.AllSections) + 1
	model, command := updateModel(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if command != nil || model.confirmation == nil || len(manager.pullForces) != 0 {
		t.Fatal("pull começou antes da confirmação")
	}
	model, _ = updateModel(t, model, tea.KeyMsg{Type: tea.KeyEsc})
	if model.confirmation != nil {
		t.Fatal("esc não cancelou a confirmação")
	}

	model.cursor = len(usersync.AllSections) + 2
	model, _ = updateModel(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if manager.loggedOut || model.confirmation == nil {
		t.Fatal("logout não aguardou confirmação")
	}
}

func TestCloseCancelaPollingDoLogin(t *testing.T) {
	manager := &fakeManager{wait: true, code: usersync.DeviceCode{
		UserCode: "ABCD", ExpiresAt: time.Now().Add(time.Minute),
	}}
	model := loadedModel(t, manager)
	model, command := updateModel(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	model, command = updateModel(t, model, command())
	batch, ok := command().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("início da espera = %T", command())
	}
	waitCommand := batch[1]

	done := make(chan tea.Msg, 1)
	go func() { done <- waitCommand() }()
	if closeCommand := model.Close(); closeCommand != nil {
		closeCommand()
	}
	select {
	case message := <-done:
		finished := message.(loginFinishedMsg)
		if !errors.Is(finished.err, context.Canceled) {
			t.Fatalf("polling terminou com %v", finished.err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close não interrompeu o polling")
	}
}

func TestFalhaRemotaNaoApagaEstadoLocalDaTela(t *testing.T) {
	status := connectedStatus()
	status.RemoteErr = errors.New("sem rede")
	model := loadedModel(t, &fakeManager{status: status})
	view := model.View(tui.Frame{Width: 100, Height: 30})
	if !strings.Contains(view, "sem rede") || !strings.Contains(view, "1 aqui") {
		t.Fatalf("degradação remota escondeu estado útil:\n%s", view)
	}
}

func TestLayoutAmploComparaLadosEExplicaProcedencia(t *testing.T) {
	status := connectedStatus()
	status.Remote.Device = "notebook-de-casa"
	status.Remote.Engine = "1.4.0"
	status.Remote.UpdatedAt = time.Date(2026, 8, 1, 21, 30, 0, 0, time.UTC)
	model := loadedModel(t, &fakeManager{status: status})
	view := model.View(tui.Frame{Width: 150, Height: 42})

	for _, expected := range []string{
		"CONTA GITHUB", "ESTA MÁQUINA", "GITHUB", "VOLUME SINCRONIZÁVEL",
		"O QUE SINCRONIZA", "OPERAÇÕES", "notebook-de-casa", "1.4.0",
		"lealing/example-tool", "binários de tools nunca são enviados",
	} {
		if !strings.Contains(view, expected) {
			t.Errorf("%q ausente no layout amplo:\n%s", expected, view)
		}
	}
}
