package usersync_test

import (
	"context"
	"testing"

	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/marketplace"
	"github.com/mateuslh/lealing/internal/core/toolinstall"
	"github.com/mateuslh/lealing/internal/core/usersync"
)

type localUsageStore struct {
	data map[domain.ToolID]domain.Usage
}

func (s *localUsageStore) Load(context.Context) (map[domain.ToolID]domain.Usage, error) {
	result := make(map[domain.ToolID]domain.Usage, len(s.data))
	for id, usage := range s.data {
		result[id] = usage
	}
	return result, nil
}

func (s *localUsageStore) Save(_ context.Context, usage domain.Usage) error {
	if s.data == nil {
		s.data = map[domain.ToolID]domain.Usage{}
	}
	s.data[usage.ToolID] = usage
	return nil
}

type localInstalled struct{ tools []toolinstall.Installed }

func (*localInstalled) InstallLocal(context.Context, toolinstall.InstallRequest) (toolinstall.Installation, error) {
	return toolinstall.Installation{}, nil
}
func (s *localInstalled) ListInstalled(context.Context) ([]toolinstall.Installed, error) {
	return append([]toolinstall.Installed(nil), s.tools...), nil
}
func (*localInstalled) Rollback(context.Context, string) (toolinstall.Installation, error) {
	return toolinstall.Installation{}, nil
}
func (*localInstalled) Remove(context.Context, string) (toolinstall.Removal, error) {
	return toolinstall.Removal{}, nil
}

type localSourceStore struct{ state marketplace.SourceState }

func (s *localSourceStore) Load(context.Context) (marketplace.SourceState, error) {
	return s.state, nil
}

func (s *localSourceStore) Save(_ context.Context, state marketplace.SourceState) error {
	s.state = state
	return nil
}

type localReconciler struct {
	request marketplace.StateReconcileRequest
	result  marketplace.StateReconciliation
	err     error
}

func (r *localReconciler) ReconcileState(_ context.Context, request marketplace.StateReconcileRequest) (marketplace.StateReconciliation, error) {
	r.request = request
	return r.result, r.err
}

type localCatalog struct{ tools map[domain.ToolID]domain.Tool }

func (c localCatalog) All(context.Context) ([]domain.Tool, error) {
	result := make([]domain.Tool, 0, len(c.tools))
	for _, tool := range c.tools {
		result = append(result, tool)
	}
	return result, nil
}
func (c localCatalog) ByID(_ context.Context, id domain.ToolID) (domain.Tool, error) {
	tool, ok := c.tools[id]
	if !ok {
		return domain.Tool{}, domain.WrapTool(id, domain.ErrToolNotFound)
	}
	return tool, nil
}
func (localCatalog) Categories(context.Context) ([]domain.Category, error) { return nil, nil }

func TestLocalStateResolveHostLegadoEPreservaProvenienciaInstalada(t *testing.T) {
	usage := &localUsageStore{data: map[domain.ToolID]domain.Usage{
		"example-tool": {ToolID: "example-tool", Runs: 2, Favorite: true},
	}}
	installed := &localInstalled{tools: []toolinstall.Installed{{
		ID: "example-tool", ActiveVersion: "1.2.3",
	}}}
	catalog := localCatalog{tools: map[domain.ToolID]domain.Tool{
		"example-tool": {Host: "lealing", ID: "example-tool"},
	}}

	state, err := usersync.NewLocalState(usage, nil, installed, catalog, nil, nil).Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Usage) != 1 || state.Usage[0].Host != "lealing" {
		t.Fatalf("uso coletado = %+v", state.Usage)
	}
	if len(state.Tools) != 1 || state.Tools[0].Host != "lealing" {
		t.Fatalf("tools coletadas = %+v", state.Tools)
	}
}

func TestLocalStateNaoAplicaPreferenciaEmToolHomonima(t *testing.T) {
	usage := &localUsageStore{}
	catalog := localCatalog{tools: map[domain.ToolID]domain.Tool{
		"example-tool": {Host: "outro-host", ID: "example-tool"},
	}}
	local := usersync.NewLocalState(usage, nil, nil, catalog, nil, nil)
	state := usersync.State{Usage: []usersync.ToolUsage{{
		Host: "lealing", ID: "example-tool", Runs: 9, Favorite: true,
	}}}
	selection := usersync.Selection{usersync.SectionUsage: true}

	applied, err := local.Apply(context.Background(), state, selection, false)
	if err != nil {
		t.Fatal(err)
	}
	if applied[usersync.SectionUsage] != 0 || len(usage.data) != 0 {
		t.Fatalf("preferência atravessou host: applied=%v usage=%v", applied, usage.data)
	}
}

func TestLocalStateGuardaPreferenciaDeToolAindaNaoInstalada(t *testing.T) {
	usage := &localUsageStore{}
	local := usersync.NewLocalState(usage, nil, nil, localCatalog{}, nil, nil)
	state := usersync.State{Usage: []usersync.ToolUsage{{
		Host: "lealing", ID: "example-tool", Runs: 4, Favorite: true,
	}}}

	if _, err := local.Apply(context.Background(), state,
		usersync.Selection{usersync.SectionUsage: true}, false); err != nil {
		t.Fatal(err)
	}
	got := usage.data["example-tool"]
	if got.Host != "lealing" || !got.Favorite || got.Runs != 4 {
		t.Fatalf("uso guardado = %+v", got)
	}
}

func TestLocalStatePublicaOrigensEmbutidasEReferenciaCadaToolAoHostCorreto(t *testing.T) {
	sources := &localSourceStore{state: marketplace.SourceState{Custom: []marketplace.Origin{{
		Name: "partner", Kind: marketplace.OriginRemote,
		Ref: "https://example.test/partner/index.json", Enabled: true,
	}}}}
	installed := &localInstalled{tools: []toolinstall.Installed{
		{Host: "partner", ID: "example-tool", ActiveVersion: "1.0.0"},
		{Host: "lealing", ID: "another-tool", ActiveVersion: "1.1.0"},
	}}
	builtins := []marketplace.Origin{{
		Name: "lealing", Label: "índice padrão", Kind: marketplace.OriginRemote,
		Ref: "https://example.test/lealing/index.json", Trusted: true, Builtin: true, Enabled: true,
	}}

	state, err := usersync.NewLocalState(nil, sources, installed, nil, builtins, nil).
		Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Sources) != 2 || state.Sources[0].Name != "lealing" && state.Sources[1].Name != "lealing" {
		t.Fatalf("origem lealing não foi publicada: %+v", state.Sources)
	}
	hosts := map[string]string{}
	for _, tool := range state.Tools {
		hosts[tool.ID] = tool.Host
	}
	if hosts["example-tool"] != "partner" || hosts["another-tool"] != "lealing" {
		t.Fatalf("hosts das tools = %+v", hosts)
	}
}

func TestLocalStateNaoPersisteOrigemEmbutidaRecebidaComoCustom(t *testing.T) {
	sources := &localSourceStore{}
	builtins := []marketplace.Origin{{
		Name: "lealing", Kind: marketplace.OriginRemote,
		Ref: "https://example.test/canonical/index.json", Trusted: true, Builtin: true, Enabled: true,
	}}
	local := usersync.NewLocalState(nil, sources, nil, nil, builtins, nil)
	state := usersync.State{Sources: []usersync.MarketplaceSource{
		{Name: "lealing", Kind: "remote", Ref: "https://example.test/forged/index.json", Enabled: false},
		{Name: "partner", Kind: "remote", Ref: "https://example.test/partner/index.json", Enabled: true},
	}}

	applied, err := local.Apply(context.Background(), state,
		usersync.Selection{usersync.SectionSources: true}, false)
	if err != nil {
		t.Fatal(err)
	}
	if applied[usersync.SectionSources] != 2 {
		t.Fatalf("origens aplicadas = %v", applied)
	}
	if len(sources.state.Custom) != 1 || sources.state.Custom[0].Name != "partner" {
		t.Fatalf("origens personalizadas = %+v", sources.state.Custom)
	}
	if len(sources.state.DisabledBuiltins) != 1 || sources.state.DisabledBuiltins[0] != "lealing" {
		t.Fatalf("origens embutidas desativadas = %+v", sources.state.DisabledBuiltins)
	}
}

func TestLocalStateAplicaOrigensEToolsComoUmaIntencaoExata(t *testing.T) {
	sources := &localSourceStore{}
	reconciler := &localReconciler{result: marketplace.StateReconciliation{Sources: 1, Tools: 2}}
	local := usersync.NewLocalState(nil, sources, nil, nil, nil, reconciler)
	state := usersync.State{
		Sources: []usersync.MarketplaceSource{{
			Name: "parceiro", Kind: "remote", Ref: "https://parceiro.test/index.json", Enabled: true,
		}},
		Tools: []usersync.InstalledTool{
			{Host: "parceiro", ID: "example-tool", Version: "1.2.3"},
			{Host: "parceiro", ID: "another-tool", Version: "2.0.0"},
		},
	}

	applied, err := local.Apply(context.Background(), state, usersync.Selection{
		usersync.SectionSources: true, usersync.SectionTools: true,
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if reconciler.request.Sources == nil || len(reconciler.request.Sources.Custom) != 1 || !reconciler.request.ExactTools {
		t.Fatalf("intenção recebida = %+v", reconciler.request)
	}
	if len(reconciler.request.Tools) != 2 || reconciler.request.Tools[0].Version != "1.2.3" {
		t.Fatalf("tools desejadas = %+v", reconciler.request.Tools)
	}
	if applied[usersync.SectionSources] != 1 || applied[usersync.SectionTools] != 2 {
		t.Fatalf("aplicado = %+v", applied)
	}
}

func TestCollectNaoPublicaInstalacaoLocalSemOrigemReproduzivel(t *testing.T) {
	sources := &localSourceStore{}
	installed := &localInstalled{tools: []toolinstall.Installed{{
		Host: "local", ID: "example-tool", ActiveVersion: "1.0.0",
	}}}

	state, err := usersync.NewLocalState(nil, sources, installed, nil, nil, nil).
		Collect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Tools) != 0 {
		t.Fatalf("instalação irreproduzível foi publicada: %+v", state.Tools)
	}
}
