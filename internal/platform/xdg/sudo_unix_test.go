//go:build !windows

package xdg

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// O caminho que importa só existe com privilégio: rode a suíte sob
// `sudo -E` para exercitá-lo (as variáveis SUDO_* vêm do próprio sudo).
func TestAdoptSobSudoEntregaAoUsuárioReal(t *testing.T) {
	uid, gid, ok := realUser()
	if !ok {
		t.Skip("sem sudo: rode com `sudo -E go test ./internal/platform/xdg/`")
	}

	dir := filepath.Join(t.TempDir(), "share", "lealing")
	if err := MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	path := filepath.Join(dir, "usage.json")
	if err := os.WriteFile(path, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Adopt(path); err != nil {
		t.Fatalf("Adopt: %v", err)
	}

	for _, p := range []string{dir, filepath.Dir(dir), path} {
		gotUID, gotGID := ownerOf(t, p)
		if gotUID != uid || gotGID != gid {
			t.Errorf("%s pertence a %d:%d, quero %d:%d", p, gotUID, gotGID, uid, gid)
		}
	}
}

func ownerOf(t *testing.T, path string) (uid, gid int) {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("Stat_t indisponível para %s", path)
	}
	return int(st.Uid), int(st.Gid)
}
