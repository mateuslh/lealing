package usersync_test

import (
	"context"
	"testing"

	"github.com/mateuslh/lealing/internal/core/domain"
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

	state, err := usersync.NewLocalState(usage, nil, installed, catalog).Collect(context.Background())
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
	local := usersync.NewLocalState(usage, nil, nil, catalog)
	state := usersync.State{Usage: []usersync.ToolUsage{{
		Host: "lealing", ID: "example-tool", Runs: 9, Favorite: true,
	}}}
	selection := usersync.Selection{usersync.SectionUsage: true}

	applied, err := local.Apply(context.Background(), state, selection)
	if err != nil {
		t.Fatal(err)
	}
	if applied[usersync.SectionUsage] != 0 || len(usage.data) != 0 {
		t.Fatalf("preferência atravessou host: applied=%v usage=%v", applied, usage.data)
	}
}

func TestLocalStateGuardaPreferenciaDeToolAindaNaoInstalada(t *testing.T) {
	usage := &localUsageStore{}
	local := usersync.NewLocalState(usage, nil, nil, localCatalog{})
	state := usersync.State{Usage: []usersync.ToolUsage{{
		Host: "lealing", ID: "example-tool", Runs: 4, Favorite: true,
	}}}

	if _, err := local.Apply(context.Background(), state,
		usersync.Selection{usersync.SectionUsage: true}); err != nil {
		t.Fatal(err)
	}
	got := usage.data["example-tool"]
	if got.Host != "lealing" || !got.Favorite || got.Runs != 4 {
		t.Fatalf("uso guardado = %+v", got)
	}
}
