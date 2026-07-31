//go:build darwin

// Estes testes leem a máquina de verdade — sysctl e pmset. Não afirmam
// valores (que variam por máquina); servem para conferir
// que os adapters conversam com o sistema e para inspecionar a saída com
// `go test -v -run Smoke`. São pulados em `-short`, que é como a CI roda.
package macos_test

import (
	"context"
	"testing"

	"github.com/mateuslh/lealing/internal/adapter/outbound/macos"
)

func TestSmokeSysInfo(t *testing.T) {
	skipShort(t)
	s, err := macos.NewSystemInspector(nil).Inspect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, sec := range s.Sections() {
		t.Logf("[%s]", sec.Title)
		for _, f := range sec.Fields {
			t.Logf("  %-22s %s", f.Label, f.Value)
		}
	}
}

func TestSmokePower(t *testing.T) {
	skipShort(t)
	s, err := macos.NewPowerManager().Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("bateria: %+v", s.Battery)
	t.Logf("resumo:  %s", s.Battery.Summary())
	t.Logf("ac:      %+v", s.AC)
	t.Logf("sem senha: %v", macos.NewPowerManager().PasswordlessEnabled(context.Background()))
}

// skipShort pula testes que dependem da máquina local.
func skipShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("depende do sistema local")
	}
}
