package marketplace

import (
	"context"
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	coremarket "github.com/mateuslh/lealing/internal/core/marketplace"
	"github.com/mateuslh/lealing/internal/core/toolinstall"
)

type fakeManager struct {
	tools        []coremarket.Listing
	listErr      error
	installation toolinstall.Installation
	installErr   error
	lists        int
	installedID  string
}

func (f *fakeManager) List(context.Context) ([]coremarket.Listing, error) {
	f.lists++
	return f.tools, f.listErr
}

func (f *fakeManager) Install(_ context.Context, id string) (toolinstall.Installation, error) {
	f.installedID = id
	return f.installation, f.installErr
}

func fixture() coremarket.Listing {
	return coremarket.Listing{Entry: coremarket.Entry{
		ID: "token-usage", Version: "1.0.0", Name: "Uso de Tokens",
		Summary: "Mostra consumo de tokens e custos estimados.", Publisher: "mateuslh",
		DistributionTier: coremarket.ChannelOfficial, Risk: "safe", Glyph: "✧",
	}}
}

func testDeps() tui.Deps { return tui.Deps{Theme: theme.Default()} }

func TestEstadosLoadingRunningEError(t *testing.T) {
	manager := &fakeManager{tools: []coremarket.Listing{fixture()}}
	model := New(testDeps(), manager)
	if !strings.Contains(model.View(tui.Frame{Width: 80, Height: 20}), "consultando") {
		t.Fatal("loading não foi renderizado")
	}
	message := model.Init()()
	if manager.lists != 1 {
		t.Fatalf("List = %d", manager.lists)
	}
	if next, command := model.Update(message); command != nil || !strings.Contains(next.View(tui.Frame{Width: 80, Height: 20}), "Uso de Tokens") {
		t.Fatal("estado running não foi renderizado")
	}

	broken := New(testDeps(), &fakeManager{listErr: errors.New("sem rede")})
	broken, _ = updateAsModel(t, broken, broken.Init()())
	if !strings.Contains(broken.View(tui.Frame{Width: 60, Height: 12}), "sem rede") {
		t.Fatal("erro de carga não foi renderizado")
	}
}

func TestEnterAbreConfirmacaoGlobalSemInstalarDentroDeUpdate(t *testing.T) {
	manager := &fakeManager{tools: []coremarket.Listing{fixture()}}
	model := New(testDeps(), manager)
	model, _ = updateAsModel(t, model, model.Init()())
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("enter não abriu confirmação")
	}
	message := command()
	if _, ok := message.(tui.NavigateMsg); !ok {
		t.Fatalf("mensagem = %T, quero NavigateMsg", message)
	}
	if manager.installedID != "" {
		t.Fatal("Update executou I/O de instalação")
	}
}

func TestInstallRodaEmComandoAssincrono(t *testing.T) {
	manager := &fakeManager{
		tools:        []coremarket.Listing{fixture()},
		installation: toolinstall.Installation{ID: "token-usage", Version: "1.0.0"},
	}
	model := New(testDeps(), manager)
	model.tools = manager.tools
	model.loading = false
	message := model.install("token-usage")()
	if manager.installedID != "token-usage" {
		t.Fatal("comando não chamou o caso de uso")
	}
	next, command := model.Update(message)
	view := next.View(tui.Frame{Width: 100, Height: 24})
	if command == nil || !strings.Contains(view, "token-usage@1.0.0 instalada") {
		t.Fatalf("sucesso não atualizou o estado e não pediu recarga: command=%v view=%q", command != nil, view)
	}
}

func TestHintsIncluemEsc(t *testing.T) {
	model := New(testDeps(), &fakeManager{})
	for _, hint := range model.Hints() {
		if strings.Contains(hint.Key, "esc") {
			return
		}
	}
	t.Fatal("hint esc ausente")
}

func updateAsModel(t *testing.T, model *Model, message tea.Msg) (*Model, tea.Cmd) {
	t.Helper()
	next, command := model.Update(message)
	updated, ok := next.(*Model)
	if !ok {
		t.Fatalf("model = %T", next)
	}
	return updated, command
}
