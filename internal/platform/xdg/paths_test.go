package xdg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePriorizaXDGEmQualquerPlataforma(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "xdg"))
	t.Setenv("LOCALAPPDATA", filepath.Join(root, "windows"))

	got := Resolve(true)
	want := filepath.Join(root, "xdg", appName)
	if got.Data != want {
		t.Errorf("Data = %q, quero %q", got.Data, want)
	}
}

func TestResolveUsaDiretorioNativoSomenteNoWindows(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_DATA_HOME", "")
	t.Setenv("LOCALAPPDATA", filepath.Join(root, "windows"))

	if got := Resolve(true).Data; got != filepath.Join(root, "windows", appName) {
		t.Errorf("Windows Data = %q", got)
	}

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("sem diretório do usuário: %v", err)
	}
	wantPortable := filepath.Join(home, ".local", "share", appName)
	if got := Resolve(false).Data; got != wantPortable {
		t.Errorf("portátil Data = %q, quero %q", got, wantPortable)
	}
}
