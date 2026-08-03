package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/marketplace"
	"github.com/mateuslh/lealing/internal/core/toolinstall"
	"github.com/mateuslh/lealing/internal/core/toolmanage"
)

type fakeToolManager struct {
	installed []toolinstall.Installed
	install   toolinstall.Installation
	removal   toolinstall.Removal
	request   toolinstall.InstallRequest
}

type fakeMarketplace struct {
	list     []marketplace.Listing
	statuses []marketplace.SourceStatus
	origins  []marketplace.Origin
	install  toolinstall.Installation
	id       string
	added    marketplace.Origin
	removed  string
	toggled  string
}

type fakeToolManagement struct {
	items     []toolmanage.Item
	removed   domain.ToolID
	toggled   domain.ToolID
	toggledTo bool
}

func (f *fakeToolManagement) List(context.Context) ([]toolmanage.Item, error) { return f.items, nil }
func (f *fakeToolManagement) SetEnabled(_ context.Context, id domain.ToolID, enabled bool) error {
	f.toggled, f.toggledTo = id, enabled
	return nil
}
func (f *fakeToolManagement) Remove(_ context.Context, id domain.ToolID) (toolinstall.Removal, error) {
	f.removed = id
	return toolinstall.Removal{ID: string(id), RecoveryDir: "/tools/.trash/" + string(id)}, nil
}

func (f *fakeMarketplace) Catalog(context.Context) (marketplace.Catalog, error) {
	return marketplace.Catalog{Tools: f.list, Sources: f.statuses}, nil
}
func (f *fakeMarketplace) List(context.Context) ([]marketplace.Listing, error) { return f.list, nil }
func (f *fakeMarketplace) Install(_ context.Context, id string) (toolinstall.Installation, error) {
	f.id = id
	return f.install, nil
}
func (f *fakeMarketplace) Sources(context.Context) ([]marketplace.Origin, error) {
	return f.origins, nil
}
func (f *fakeMarketplace) AddSource(_ context.Context, origin marketplace.Origin) error {
	f.added = origin
	return nil
}
func (f *fakeMarketplace) RemoveSource(_ context.Context, name string) error {
	f.removed = name
	return nil
}
func (f *fakeMarketplace) SetSourceEnabled(_ context.Context, name string, _ bool) error {
	f.toggled = name
	return nil
}

func (f *fakeToolManager) InstallLocal(_ context.Context, request toolinstall.InstallRequest) (toolinstall.Installation, error) {
	f.request = request
	return f.install, nil
}
func (f *fakeToolManager) ListInstalled(context.Context) ([]toolinstall.Installed, error) {
	return f.installed, nil
}
func (f *fakeToolManager) Rollback(context.Context, string) (toolinstall.Installation, error) {
	return f.install, nil
}
func (f *fakeToolManager) Remove(context.Context, string) (toolinstall.Removal, error) {
	return f.removal, nil
}

func TestComandosDeToolUsamPortaDeEntrada(t *testing.T) {
	localPackage := t.TempDir()
	manager := &fakeToolManager{
		install: toolinstall.Installation{ID: "example-tool", Version: "1.1.0", Path: "/tools/example-tool/1.1.0", SHA256: "abc"},
		removal: toolinstall.Removal{ID: "example-tool", RecoveryDir: "/tools/.trash/example-tool"},
	}
	tools := &fakeToolManagement{items: []toolmanage.Item{{
		Tool:    domain.Tool{ID: "example-tool", Name: "Example Tool", Kind: domain.KindProcess},
		Enabled: true, Installed: true, ActiveVersion: "1.1.0", PreviousVersion: "1.0.0",
	}}}
	market := &fakeMarketplace{list: []marketplace.Listing{{Entry: marketplace.Entry{
		ID: "example-tool", Version: "1.1.0", Summary: "Demonstra uma extensão.", DistributionTier: marketplace.ChannelOfficial,
	}}}}
	for _, command := range []toolCommand{
		{list: true},
		{marketplace: true},
		{install: localPackage, checksum: strings.Repeat("a", 64)},
		{rollback: "example-tool"},
		{remove: "example-tool"},
	} {
		var output bytes.Buffer
		if err := runToolCommand(context.Background(), manager, tools, market, &output, command); err != nil {
			t.Fatal(err)
		}
		if output.Len() == 0 {
			t.Fatalf("comando %+v não explicou o resultado", command)
		}
	}
	if manager.request.SourceDir != localPackage || manager.request.ExpectedSHA256 == "" {
		t.Fatalf("install request = %+v", manager.request)
	}
}

func TestToolInstallPorIDUsaMarketplace(t *testing.T) {
	market := &fakeMarketplace{install: toolinstall.Installation{ID: "example-tool", Version: "1.0.0", Path: "/tools/example-tool"}}
	var output bytes.Buffer
	if err := runToolCommand(context.Background(), &fakeToolManager{}, &fakeToolManagement{}, market, &output, toolCommand{install: "example-tool"}); err != nil {
		t.Fatal(err)
	}
	if market.id != "example-tool" || !strings.Contains(output.String(), "marketplace") {
		t.Fatalf("id=%q output=%q", market.id, output.String())
	}
}

func TestComandosDeOrigemGerenciamRepositoriosParalelos(t *testing.T) {
	market := &fakeMarketplace{origins: []marketplace.Origin{{
		Name: "lealing", Kind: marketplace.OriginRemote,
		Ref: "https://example.test/index.json", Builtin: true, Enabled: true,
	}}}

	var listing bytes.Buffer
	if err := runToolCommand(context.Background(), &fakeToolManager{}, &fakeToolManagement{}, market, &listing, toolCommand{sources: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listing.String(), "lealing\tremote\thabilitada\tembutida") {
		t.Fatalf("listagem = %q", listing.String())
	}

	var added bytes.Buffer
	if err := runToolCommand(context.Background(), &fakeToolManager{}, &fakeToolManagement{}, market, &added,
		toolCommand{sourceAdd: "https://exemplo.test/tools/index.json"}); err != nil {
		t.Fatal(err)
	}
	if market.added.Name != "exemplo-test" || market.added.Kind != marketplace.OriginRemote {
		t.Fatalf("origem cadastrada = %+v", market.added)
	}

	var removed bytes.Buffer
	if err := runToolCommand(context.Background(), &fakeToolManager{}, &fakeToolManagement{}, market, &removed,
		toolCommand{sourceRemove: "exemplo-test"}); err != nil {
		t.Fatal(err)
	}
	if market.removed != "exemplo-test" {
		t.Fatalf("origem removida = %q", market.removed)
	}

	var toggled bytes.Buffer
	if err := runToolCommand(context.Background(), &fakeToolManager{}, &fakeToolManagement{}, market, &toggled,
		toolCommand{sourceDisable: "lealing"}); err != nil {
		t.Fatal(err)
	}
	if market.toggled != "lealing" || !strings.Contains(toggled.String(), "desabilitada") {
		t.Fatalf("toggle = %q output = %q", market.toggled, toggled.String())
	}
}

func TestListagemDoMarketplaceAvisaSobreOrigemForaDoAr(t *testing.T) {
	market := &fakeMarketplace{
		list: []marketplace.Listing{{Entry: marketplace.Entry{
			ID: "example-tool", Version: "1.1.0", Summary: "Demonstra uma extensão.",
			DistributionTier: marketplace.ChannelOfficial,
			Origin:           marketplace.Origin{Name: "lealing"},
		}}},
		statuses: []marketplace.SourceStatus{{
			Origin: marketplace.Origin{Name: "meu-repo"}, Err: errors.New("sem rede"),
		}},
	}
	var output bytes.Buffer
	if err := runToolCommand(context.Background(), &fakeToolManager{}, &fakeToolManagement{}, market, &output, toolCommand{marketplace: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "aviso: origem meu-repo indisponível") {
		t.Fatalf("aviso ausente: %q", output.String())
	}
	if !strings.Contains(output.String(), "lealing/example-tool") {
		t.Fatalf("referência qualificada ausente: %q", output.String())
	}
}
