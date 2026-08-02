package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mateuslh/lealing/internal/core/marketplace"
	"github.com/mateuslh/lealing/internal/core/toolinstall"
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
		installed: []toolinstall.Installed{{ID: "token-usage", ActiveVersion: "1.1.0", PreviousVersion: "1.0.0"}},
		install:   toolinstall.Installation{ID: "token-usage", Version: "1.1.0", Path: "/tools/token-usage/1.1.0", SHA256: "abc"},
		removal:   toolinstall.Removal{ID: "token-usage", RecoveryDir: "/tools/.trash/token-usage"},
	}
	market := &fakeMarketplace{list: []marketplace.Listing{{Entry: marketplace.Entry{
		ID: "token-usage", Version: "1.1.0", Summary: "Mostra tokens.", DistributionTier: marketplace.ChannelOfficial,
	}}}}
	for _, command := range []toolCommand{
		{list: true},
		{marketplace: true},
		{install: localPackage, checksum: strings.Repeat("a", 64)},
		{rollback: "token-usage"},
		{remove: "token-usage"},
	} {
		var output bytes.Buffer
		if err := runToolCommand(context.Background(), manager, market, &output, command); err != nil {
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
	market := &fakeMarketplace{install: toolinstall.Installation{ID: "token-usage", Version: "1.0.0", Path: "/tools/token-usage"}}
	var output bytes.Buffer
	if err := runToolCommand(context.Background(), &fakeToolManager{}, market, &output, toolCommand{install: "token-usage"}); err != nil {
		t.Fatal(err)
	}
	if market.id != "token-usage" || !strings.Contains(output.String(), "marketplace") {
		t.Fatalf("id=%q output=%q", market.id, output.String())
	}
}

func TestComandosDeOrigemGerenciamRepositoriosParalelos(t *testing.T) {
	market := &fakeMarketplace{origins: []marketplace.Origin{{
		Name: "lealing", Kind: marketplace.OriginRemote,
		Ref: "https://example.test/index.json", Builtin: true, Enabled: true,
	}}}

	var listing bytes.Buffer
	if err := runToolCommand(context.Background(), &fakeToolManager{}, market, &listing, toolCommand{sources: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listing.String(), "lealing\tremote\thabilitada\tembutida") {
		t.Fatalf("listagem = %q", listing.String())
	}

	var added bytes.Buffer
	if err := runToolCommand(context.Background(), &fakeToolManager{}, market, &added,
		toolCommand{sourceAdd: "https://exemplo.test/tools/index.json"}); err != nil {
		t.Fatal(err)
	}
	if market.added.Name != "exemplo-test" || market.added.Kind != marketplace.OriginRemote {
		t.Fatalf("origem cadastrada = %+v", market.added)
	}

	var removed bytes.Buffer
	if err := runToolCommand(context.Background(), &fakeToolManager{}, market, &removed,
		toolCommand{sourceRemove: "exemplo-test"}); err != nil {
		t.Fatal(err)
	}
	if market.removed != "exemplo-test" {
		t.Fatalf("origem removida = %q", market.removed)
	}

	var toggled bytes.Buffer
	if err := runToolCommand(context.Background(), &fakeToolManager{}, market, &toggled,
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
			ID: "token-usage", Version: "1.1.0", Summary: "Mostra tokens.",
			DistributionTier: marketplace.ChannelOfficial,
			Origin:           marketplace.Origin{Name: "lealing"},
		}}},
		statuses: []marketplace.SourceStatus{{
			Origin: marketplace.Origin{Name: "meu-repo"}, Err: errors.New("sem rede"),
		}},
	}
	var output bytes.Buffer
	if err := runToolCommand(context.Background(), &fakeToolManager{}, market, &output, toolCommand{marketplace: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "aviso: origem meu-repo indisponível") {
		t.Fatalf("aviso ausente: %q", output.String())
	}
	if !strings.Contains(output.String(), "lealing/token-usage") {
		t.Fatalf("referência qualificada ausente: %q", output.String())
	}
}
