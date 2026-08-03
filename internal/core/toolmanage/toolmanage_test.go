package toolmanage_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/toolinstall"
	"github.com/mateuslh/lealing/internal/core/toolmanage"
)

type memoryState struct {
	state toolmanage.State
	err   error
}

func (m *memoryState) Load(context.Context) (toolmanage.State, error) { return m.state, m.err }
func (m *memoryState) Save(_ context.Context, state toolmanage.State) error {
	if m.err != nil {
		return m.err
	}
	m.state = state
	return nil
}

type fakeCatalog struct {
	tools   []domain.Tool
	reloads int
}

func (f *fakeCatalog) All(context.Context) ([]domain.Tool, error) { return f.tools, nil }
func (f *fakeCatalog) ByID(_ context.Context, id domain.ToolID) (domain.Tool, error) {
	for _, tool := range f.tools {
		if tool.ID == id {
			return tool, nil
		}
	}
	return domain.Tool{}, domain.WrapTool(id, domain.ErrToolNotFound)
}
func (*fakeCatalog) Categories(context.Context) ([]domain.Category, error) { return nil, nil }
func (f *fakeCatalog) Reload(context.Context) error {
	f.reloads++
	for index, tool := range f.tools {
		if tool.ID == "example-tool" {
			f.tools = append(f.tools[:index], f.tools[index+1:]...)
			break
		}
	}
	return nil
}

type fakeInstaller struct {
	installed []toolinstall.Installed
	removed   string
}

func (*fakeInstaller) InstallLocal(context.Context, toolinstall.InstallRequest) (toolinstall.Installation, error) {
	return toolinstall.Installation{}, nil
}
func (f *fakeInstaller) ListInstalled(context.Context) ([]toolinstall.Installed, error) {
	return f.installed, nil
}
func (*fakeInstaller) Rollback(context.Context, string) (toolinstall.Installation, error) {
	return toolinstall.Installation{}, nil
}
func (f *fakeInstaller) Remove(_ context.Context, id string) (toolinstall.Removal, error) {
	f.removed = id
	return toolinstall.Removal{ID: id, RecoveryDir: "/tools/.trash/" + id}, nil
}

func fixture() (*toolmanage.Service, *fakeCatalog, *fakeInstaller, *memoryState) {
	catalog := &fakeCatalog{tools: []domain.Tool{
		{Host: "fixture", ID: "example-tool", Name: "Example Tool", Kind: domain.KindProcess, Runtime: &domain.ExternalRuntime{}},
		{Host: "fixture", ID: "another-tool", Name: "Another Tool", Kind: domain.KindProcess, Runtime: &domain.ExternalRuntime{}},
	}}
	installer := &fakeInstaller{installed: []toolinstall.Installed{
		{ID: "example-tool", ActiveVersion: "1.1.0", PreviousVersion: "1.0.0"},
		{ID: "another-tool", ActiveVersion: "2.0.0"},
	}}
	state := &memoryState{}
	return toolmanage.NewService(catalog, state, installer, catalog), catalog, installer, state
}

func TestDesativacaoFiltraAllEByIDSemApagarDoGerenciamento(t *testing.T) {
	service, _, _, state := fixture()
	if err := service.SetEnabled(t.Context(), "another-tool", false); err != nil {
		t.Fatal(err)
	}
	active, err := service.All(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(active) != 1 || active[0].ID != "example-tool" {
		t.Fatalf("ativas = %+v", active)
	}
	if _, err := service.ByID(t.Context(), "another-tool"); !errors.Is(err, domain.ErrToolNotFound) {
		t.Fatalf("ByID = %v", err)
	}
	items, err := service.List(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	disabled := false
	for _, item := range items {
		if item.Tool.ID == "another-tool" {
			disabled = !item.Enabled
		}
	}
	if len(items) != 2 || !disabled {
		t.Fatalf("gerenciamento = %+v", items)
	}
	if len(state.state.Disabled) != 1 || state.state.Disabled[0] != (domain.ToolRef{Host: "fixture", ID: "another-tool"}) {
		t.Fatalf("estado = %+v", state.state)
	}
}

func TestReativacaoPublicaToolNovamente(t *testing.T) {
	service, _, _, _ := fixture()
	if err := service.SetEnabled(t.Context(), "another-tool", false); err != nil {
		t.Fatal(err)
	}
	if err := service.SetEnabled(t.Context(), "another-tool", true); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ByID(t.Context(), "another-tool"); err != nil {
		t.Fatalf("tool não voltou: %v", err)
	}
}

func TestRemoveExternaRecarregaCatalogoELimpaEstado(t *testing.T) {
	service, catalog, installer, state := fixture()
	if err := service.SetEnabled(t.Context(), "example-tool", false); err != nil {
		t.Fatal(err)
	}
	removed, err := service.Remove(t.Context(), "example-tool")
	if err != nil {
		t.Fatal(err)
	}
	if removed.RecoveryDir == "" || installer.removed != "example-tool" || catalog.reloads != 1 {
		t.Fatalf("remoção=%+v removed=%q reloads=%d", removed, installer.removed, catalog.reloads)
	}
	if len(state.state.Disabled) != 0 {
		t.Fatalf("estado residual = %+v", state.state)
	}
}

func TestMesmoIDEmOutroProviderNaoHerdaDesativacao(t *testing.T) {
	service, catalog, _, state := fixture()
	if err := service.SetEnabled(t.Context(), "example-tool", false); err != nil {
		t.Fatal(err)
	}
	catalog.tools[0].Host = "outro-provider"
	if _, err := service.ByID(t.Context(), "example-tool"); err != nil {
		t.Fatalf("outro provider herdou a desativação: %v", err)
	}
	if err := service.SetEnabled(t.Context(), "example-tool", false); err != nil {
		t.Fatal(err)
	}
	if len(state.state.Disabled) != 2 ||
		state.state.Disabled[0].Host != "fixture" ||
		state.state.Disabled[1].Host != "outro-provider" {
		t.Fatalf("estado não distinguiu providers = %+v", state.state)
	}
}

func TestEstadoSemProviderERecusado(t *testing.T) {
	service, _, _, state := fixture()
	state.state.Disabled = []domain.ToolRef{{ID: "example-tool"}}
	if _, err := service.ByID(t.Context(), "example-tool"); err == nil {
		t.Fatal("estado sem provider foi aceito")
	}
}

func TestFalhaAoPersistirNaoMudaCatalogoEmMemoria(t *testing.T) {
	service, _, _, state := fixture()
	state.err = errors.New("disco cheio")
	if err := service.SetEnabled(t.Context(), "another-tool", false); err == nil {
		t.Fatal("falha do store foi escondida")
	}
	state.err = nil
	if _, err := service.ByID(t.Context(), "another-tool"); err != nil {
		t.Fatalf("estado em memória mudou apesar da falha: %v", err)
	}
}
