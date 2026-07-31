package home

import (
	"testing"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
)

// O render roda a cada tecla. Se um frame custar mais que alguns
// milissegundos, a digitação na busca começa a engasgar em terminais lentos
// — daí medir o caminho quente separado do resto da suíte.

func BenchmarkViewNavegacao(b *testing.B) {
	m := newTestModel(&testing.T{})
	f := tui.Frame{Width: 150, Height: 40}

	b.ReportAllocs()
	for b.Loop() {
		_ = m.View(f)
	}
}

func BenchmarkViewBusca(b *testing.B) {
	t := &testing.T{}
	m := newTestModel(t)
	m, _ = press(t, m, "/")
	m = typeText(t, m, "sistema")
	f := tui.Frame{Width: 150, Height: 40}

	b.ReportAllocs()
	for b.Loop() {
		_ = m.View(f)
	}
}
