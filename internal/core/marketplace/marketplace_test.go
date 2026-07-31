package marketplace_test

import (
	"strings"
	"testing"

	"github.com/mateuslh/lealing/internal/core/marketplace"
)

func TestSelecionaMaisNovaCompativelComEngineProtocoloEPlataforma(t *testing.T) {
	artifact := func(platform string) []marketplace.Artifact {
		return []marketplace.Artifact{{Platform: platform, URL: "https://example.test/tool", SHA256: strings.Repeat("a", 64)}}
	}
	index := marketplace.Index{Tools: []marketplace.Entry{
		{ID: "demo", Version: "1.0.0", Protocol: marketplace.VersionRange{Min: 1, Max: 1}, MinimumEngine: "1.0.0", Artifacts: artifact("darwin-arm64")},
		{ID: "demo", Version: "1.2.0", Protocol: marketplace.VersionRange{Min: 1, Max: 1}, MinimumEngine: "1.1.0", Artifacts: artifact("darwin-arm64")},
		{ID: "demo", Version: "2.0.0", Protocol: marketplace.VersionRange{Min: 2, Max: 2}, MinimumEngine: "2.0.0", Artifacts: artifact("darwin-arm64")},
	}}
	entry, _, err := index.SelectLatest("demo", "darwin-arm64", "1.5.0", marketplace.VersionRange{Min: 1, Max: 1})
	if err != nil {
		t.Fatal(err)
	}
	if entry.Version != "1.2.0" {
		t.Fatalf("versão = %s", entry.Version)
	}
}
