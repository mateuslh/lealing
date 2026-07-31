package intellij_test

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mateuslh/lealing/internal/adapter/outbound/intellij"
)

func TestAddEntriesRegistraProjetosSemDuplicar(t *testing.T) {
	home := filepath.Join(t.TempDir(), "usuario")
	first := filepath.Join(home, "dev", "pix", "pix")
	second := filepath.Join(home, "dev", "pix", "pix-config")
	raw := []byte(`<application>
  <component name="RecentProjectsManager">
    <option name="additionalInfo">
      <map>
        <entry key="$USER_HOME$/dev/pix/pix"><value /></entry>
      </map>
    </option>
  </component>
</application>`)

	got, changed, err := intellij.AddEntries(raw, home, []string{first, second},
		time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("changed = false")
	}
	text := string(got)
	if n := strings.Count(text, `key="$USER_HOME$/dev/pix/pix"`); n != 1 {
		t.Fatalf("entrada preexistente apareceu %d vezes\n%s", n, text)
	}
	if !strings.Contains(text, `key="$USER_HOME$/dev/pix/pix-config"`) {
		t.Fatalf("entrada nova ausente\n%s", text)
	}
	if !strings.Contains(text, `frameTitle="pix-config"`) {
		t.Fatalf("título novo ausente\n%s", text)
	}
	var doc any
	if err := xml.Unmarshal(got, &doc); err != nil {
		t.Fatalf("XML inválido: %v\n%s", err, got)
	}
}

func TestLatestRecentProjectsPrefereOMaisNovo(t *testing.T) {
	root := t.TempDir()
	old := filepath.Join(root, "IntelliJIdea2025.3", "options", "recentProjects.xml")
	newer := filepath.Join(root, "IntelliJIdea2026.1", "options", "recentProjects.xml")
	for _, file := range []string{old, newer} {
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("<application/>"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	oldTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(old, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	got, err := intellij.LatestRecentProjects(filepath.Join(root, "IntelliJIdea*", "options", "recentProjects.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if got != newer {
		t.Fatalf("arquivo = %q, quero %q", got, newer)
	}
}
