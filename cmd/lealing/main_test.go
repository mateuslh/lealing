package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/mateuslh/lealing/internal/core/toolinstall"
)

type fakeToolManager struct {
	installed []toolinstall.Installed
	install   toolinstall.Installation
	removal   toolinstall.Removal
	request   toolinstall.InstallRequest
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
	manager := &fakeToolManager{
		installed: []toolinstall.Installed{{ID: "token-usage", ActiveVersion: "1.1.0", PreviousVersion: "1.0.0"}},
		install:   toolinstall.Installation{ID: "token-usage", Version: "1.1.0", Path: "/tools/token-usage/1.1.0", SHA256: "abc"},
		removal:   toolinstall.Removal{ID: "token-usage", RecoveryDir: "/tools/.trash/token-usage"},
	}
	for _, command := range []toolCommand{
		{list: true},
		{install: "/artifact", checksum: strings.Repeat("a", 64)},
		{rollback: "token-usage"},
		{remove: "token-usage"},
	} {
		var output bytes.Buffer
		if err := runToolCommand(context.Background(), manager, &output, command); err != nil {
			t.Fatal(err)
		}
		if output.Len() == 0 {
			t.Fatalf("comando %+v não explicou o resultado", command)
		}
	}
	if manager.request.SourceDir != "/artifact" || manager.request.ExpectedSHA256 == "" {
		t.Fatalf("install request = %+v", manager.request)
	}
}
