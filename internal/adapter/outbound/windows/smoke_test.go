//go:build windows

// Estes testes leem a máquina de verdade — CIM/WMI e o plano de energia
// ativo. Não afirmam valores (que variam por máquina); servem para conferir
// que os adapters conversam com o sistema e para inspecionar a saída com
// `go test -v -run Smoke`. São pulados em `-short`, que é como a CI roda.
//
// Espelham os de macos/smoke_test.go: é a única verificação que prova que os
// scripts do PowerShell continuam válidos, já que os testes de parser rodam
// contra amostras fixas.
package windows_test

import (
	"context"
	"testing"

	"github.com/mateuslh/lealing/internal/adapter/outbound/windows"
	"github.com/mateuslh/lealing/internal/core/power"
)

func TestSmokeSysInfo(t *testing.T) {
	skipShort(t)
	s, err := windows.NewSystemInspector(nil).Inspect(context.Background())
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
	m := windows.NewPowerManager()
	s, err := m.Read(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("bateria: %+v", s.Battery)
	t.Logf("resumo:  %s", s.Battery.Summary())
	t.Logf("ac:      %+v", s.AC)

	// Uma leitura que devolve tudo zerado quase sempre quer dizer que o
	// filtro por plano ativo não casou — e a tela mostraria "nunca dorme"
	// para uma máquina que dorme.
	if s == (power.Settings{}) {
		t.Error("nenhum tempo de inatividade lido: o plano ativo não casou?")
	}
}

// skipShort pula testes que dependem da máquina local.
func skipShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("depende do sistema local")
	}
}
