package main

import (
	"bytes"
	"context"
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
	list    []marketplace.Listing
	install toolinstall.Installation
	id      string
}

func (f *fakeMarketplace) List(context.Context) ([]marketplace.Listing, error) { return f.list, nil }
func (f *fakeMarketplace) Install(_ context.Context, id string) (toolinstall.Installation, error) {
	f.id = id
	return f.install, nil
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
