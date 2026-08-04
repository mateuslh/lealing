package toolinstall_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mateuslh/lealing/internal/core/toolinstall"
)

func TestPermissionsAddedDetectaSoOQueEEstritamenteNovo(t *testing.T) {
	tests := []struct {
		name string
		old  toolinstall.ToolPermissions
		new  toolinstall.ToolPermissions
		want toolinstall.ToolPermissions
	}{
		{
			name: "sem instalação prévia conta tudo como novo",
			old:  toolinstall.ToolPermissions{},
			new:  toolinstall.ToolPermissions{ReadPaths: []string{"~/proj"}, Network: true},
			want: toolinstall.ToolPermissions{ReadPaths: []string{"~/proj"}, Network: true},
		},
		{
			name: "mesmas permissões não é escalada",
			old:  toolinstall.ToolPermissions{ReadPaths: []string{"~/proj"}, Network: true, WorkingDir: "read"},
			new:  toolinstall.ToolPermissions{ReadPaths: []string{"~/proj"}, Network: true, WorkingDir: "read"},
			want: toolinstall.ToolPermissions{},
		},
		{
			name: "caminho de leitura novo é escalada",
			old:  toolinstall.ToolPermissions{ReadPaths: []string{"~/a"}},
			new:  toolinstall.ToolPermissions{ReadPaths: []string{"~/a", "~/b"}},
			want: toolinstall.ToolPermissions{ReadPaths: []string{"~/b"}},
		},
		{
			name: "rede false para true é escalada",
			old:  toolinstall.ToolPermissions{Network: false},
			new:  toolinstall.ToolPermissions{Network: true},
			want: toolinstall.ToolPermissions{Network: true},
		},
		{
			name: "subprocess false para true é escalada",
			old:  toolinstall.ToolPermissions{Subprocess: false},
			new:  toolinstall.ToolPermissions{Subprocess: true},
			want: toolinstall.ToolPermissions{Subprocess: true},
		},
		{
			name: "workingDir vazio para read é escalada",
			old:  toolinstall.ToolPermissions{WorkingDir: ""},
			new:  toolinstall.ToolPermissions{WorkingDir: "read"},
			want: toolinstall.ToolPermissions{WorkingDir: "read"},
		},
		{
			name: "workingDir read para write é escalada",
			old:  toolinstall.ToolPermissions{WorkingDir: "read"},
			new:  toolinstall.ToolPermissions{WorkingDir: "write"},
			want: toolinstall.ToolPermissions{WorkingDir: "write"},
		},
		{
			name: "remover um caminho não é escalada",
			old:  toolinstall.ToolPermissions{ReadPaths: []string{"~/a", "~/b"}},
			new:  toolinstall.ToolPermissions{ReadPaths: []string{"~/a"}},
			want: toolinstall.ToolPermissions{},
		},
		{
			name: "workingDir write para read não é escalada",
			old:  toolinstall.ToolPermissions{WorkingDir: "write"},
			new:  toolinstall.ToolPermissions{WorkingDir: "read"},
			want: toolinstall.ToolPermissions{},
		},
		{
			name: "desligar rede não é escalada",
			old:  toolinstall.ToolPermissions{Network: true},
			new:  toolinstall.ToolPermissions{Network: false},
			want: toolinstall.ToolPermissions{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toolinstall.PermissionsAdded(tt.old, tt.new)
			if !sameToolPermissions(got, tt.want) {
				t.Fatalf("PermissionsAdded(%+v, %+v) = %+v, want %+v", tt.old, tt.new, got, tt.want)
			}
			if got.Empty() != tt.want.Empty() {
				t.Fatalf("Empty() = %v, want %v", got.Empty(), tt.want.Empty())
			}
		})
	}
}

func sameToolPermissions(a, b toolinstall.ToolPermissions) bool {
	return sameStrings(a.ReadPaths, b.ReadPaths) && sameStrings(a.WritePaths, b.WritePaths) &&
		a.Network == b.Network && a.Subprocess == b.Subprocess && a.WorkingDir == b.WorkingDir
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// fakeStore permite observar se Install/Reconcile chegaram a mutar o estado.
type fakeStore struct {
	activePermissions map[string]toolinstall.ToolPermissions
	installed         map[string]bool
	activeErr         error

	installCalls   int
	reconcileCalls int
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		activePermissions: map[string]toolinstall.ToolPermissions{},
		installed:         map[string]bool{},
	}
}

func (f *fakeStore) Install(_ context.Context, request toolinstall.InstallRequest) (toolinstall.Installation, error) {
	f.installCalls++
	f.installed[request.ExpectedID] = true
	return toolinstall.Installation{ID: request.ExpectedID, Version: request.ExpectedVersion}, nil
}

func (f *fakeStore) List(context.Context) ([]toolinstall.Installed, error) { return nil, nil }

func (f *fakeStore) Rollback(context.Context, string) (toolinstall.Installation, error) {
	return toolinstall.Installation{}, errors.New("não usado neste teste")
}

func (f *fakeStore) Remove(context.Context, string) (toolinstall.Removal, error) {
	return toolinstall.Removal{}, errors.New("não usado neste teste")
}

func (f *fakeStore) ActivePermissions(_ context.Context, id string) (toolinstall.ToolPermissions, bool, error) {
	if f.activeErr != nil {
		return toolinstall.ToolPermissions{}, false, f.activeErr
	}
	permissions, installed := f.activePermissions[id]
	return permissions, installed, nil
}

func (f *fakeStore) Reconcile(_ context.Context, requests []toolinstall.InstallRequest) (toolinstall.Reconciliation, error) {
	f.reconcileCalls++
	for _, request := range requests {
		f.installed[request.ExpectedID] = true
	}
	return toolinstall.Reconciliation{Changed: len(requests)}, nil
}

func (f *fakeStore) Restore(context.Context, toolinstall.Reconciliation) error { return nil }

var _ toolinstall.Store = (*fakeStore)(nil)
var _ toolinstall.ReconcileStore = (*fakeStore)(nil)

func expectation(read []string, network bool) *toolinstall.ManifestExpectation {
	return &toolinstall.ManifestExpectation{FilesystemRead: read, Network: network}
}

func TestInstallLocalSemEscaladaProssegueNormalmente(t *testing.T) {
	store := newFakeStore()
	service := toolinstall.NewService(store)

	_, err := service.InstallLocal(context.Background(), toolinstall.InstallRequest{
		ExpectedID: "demo", ExpectedVersion: "1.0.0", ExpectedManifest: expectation(nil, false),
	})
	if err != nil {
		t.Fatalf("InstallLocal sem instalação prévia devolveu erro: %v", err)
	}
	if store.installCalls != 1 {
		t.Fatalf("Install chamado %d vezes, queria 1", store.installCalls)
	}
}

func TestInstallLocalComEscaladaSemAceiteNaoMuta(t *testing.T) {
	store := newFakeStore()
	store.activePermissions["demo"] = toolinstall.ToolPermissions{}
	service := toolinstall.NewService(store)

	_, err := service.InstallLocal(context.Background(), toolinstall.InstallRequest{
		ExpectedID: "demo", ExpectedVersion: "2.0.0", ExpectedManifest: expectation(nil, true),
	})
	var escalation *toolinstall.PermissionEscalationError
	if !errors.As(err, &escalation) {
		t.Fatalf("InstallLocal = %v, queria *PermissionEscalationError", err)
	}
	if len(escalation.Escalations) != 1 || escalation.Escalations[0].ID != "demo" || !escalation.Escalations[0].Added.Network {
		t.Fatalf("escalada = %+v", escalation.Escalations)
	}
	if store.installCalls != 0 {
		t.Fatalf("Install foi chamado mesmo sem aprovação: %d chamadas", store.installCalls)
	}
}

func TestInstallLocalComEscaladaEAceiteProssegue(t *testing.T) {
	store := newFakeStore()
	store.activePermissions["demo"] = toolinstall.ToolPermissions{}
	service := toolinstall.NewService(store)

	_, err := service.InstallLocal(context.Background(), toolinstall.InstallRequest{
		ExpectedID: "demo", ExpectedVersion: "2.0.0", ExpectedManifest: expectation(nil, true),
		PermissionsAccepted: true,
	})
	if err != nil {
		t.Fatalf("InstallLocal com aceite devolveu erro: %v", err)
	}
	if store.installCalls != 1 {
		t.Fatalf("Install chamado %d vezes, queria 1", store.installCalls)
	}
}

func TestReconcileComEscaladaEmUmaToolNaoAplicaNenhuma(t *testing.T) {
	store := newFakeStore()
	store.activePermissions["a"] = toolinstall.ToolPermissions{}
	store.activePermissions["b"] = toolinstall.ToolPermissions{Network: true}
	service := toolinstall.NewService(store)

	_, err := service.Reconcile(context.Background(), []toolinstall.InstallRequest{
		{Host: "local", ExpectedID: "a", ExpectedVersion: "1.0.0", ExpectedManifest: expectation(nil, true)},
		{Host: "local", ExpectedID: "b", ExpectedVersion: "1.0.0", ExpectedManifest: expectation(nil, true)},
	})
	var escalation *toolinstall.PermissionEscalationError
	if !errors.As(err, &escalation) {
		t.Fatalf("Reconcile = %v, queria *PermissionEscalationError", err)
	}
	if len(escalation.Escalations) != 1 || escalation.Escalations[0].ID != "a" {
		t.Fatalf("escalada agregada = %+v, queria só 'a'", escalation.Escalations)
	}
	if store.reconcileCalls != 0 {
		t.Fatalf("Reconcile do store foi chamado mesmo com escalada pendente: %d chamadas", store.reconcileCalls)
	}
}

func TestActivePermissionsIlegivelNaoViraAusenciaDePermissao(t *testing.T) {
	store := newFakeStore()
	store.activeErr = errors.New("manifest corrompido")
	service := toolinstall.NewService(store)

	_, err := service.InstallLocal(context.Background(), toolinstall.InstallRequest{
		ExpectedID: "demo", ExpectedVersion: "2.0.0", ExpectedManifest: expectation(nil, true),
	})
	if err == nil {
		t.Fatal("InstallLocal deveria falhar quando as permissões ativas não puderam ser lidas")
	}
	var escalation *toolinstall.PermissionEscalationError
	if errors.As(err, &escalation) {
		t.Fatal("um erro de leitura não pode virar PermissionEscalationError: isso esconderia a falha real")
	}
	if store.installCalls != 0 {
		t.Fatalf("Install foi chamado mesmo com falha ao ler permissões ativas: %d chamadas", store.installCalls)
	}
}
