package main

import (
	"path/filepath"
	"testing"

	"github.com/mateuslh/lealing/sdk/protocol"
)

func TestGrantedReadUsaSomentePathEntreguePelaEngine(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".codex", "sessions")
	initialize := protocol.Initialize{Permissions: protocol.Permissions{
		Filesystem: protocol.FilesystemPermissions{Read: []string{root}},
	}}
	if got := grantedRead(initialize, ".codex/sessions"); got != root {
		t.Fatalf("grant = %q", got)
	}
	if got := grantedRead(initialize, ".claude/projects"); got != "" {
		t.Fatalf("path não concedido = %q", got)
	}
}
