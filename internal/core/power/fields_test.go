package power_test

import (
	"testing"

	"github.com/mateuslh/lealing/internal/core/power"
)

// O painel parcial é o do powercfg: só os três tempos de inatividade.
const parcial = power.FeatureSleep | power.FeatureDisplaySleep | power.FeatureDiskSleep

func TestVisibleFieldsRespeitaFonteEPlataforma(t *testing.T) {
	// Painel completo: "Reduzir brilho" só na bateria, "Acordar pela rede" só
	// no carregador — daí as colunas terem tamanhos diferentes.
	bateria := power.VisibleFields(power.Battery, power.AllFeatures)
	carregador := power.VisibleFields(power.AC, power.AllFeatures)
	if len(bateria) == 0 || len(carregador) == 0 {
		t.Fatal("painel completo veio vazio")
	}
	for _, f := range bateria {
		if f.Label == "Acordar pela rede" {
			t.Error("campo exclusivo do carregador apareceu na bateria")
		}
	}
	for _, f := range carregador {
		if f.Label == "Reduzir brilho" {
			t.Error("campo exclusivo da bateria apareceu no carregador")
		}
	}

	if n := len(power.VisibleFields(power.Battery, parcial)); n != 3 {
		t.Errorf("%d campos no painel parcial, quero 3", n)
	}
	if n := len(power.VisibleFields(power.Battery, 0)); n != 0 {
		t.Errorf("%d campos sem nenhuma feature declarada, quero 0", n)
	}
}

func TestMergeSoCopiaOQueAPlataformaGrava(t *testing.T) {
	atual := power.Settings{
		Battery: power.Profile{Sleep: 15, DisplaySleep: 5, DiskSleep: 10},
		AC:      power.Profile{Sleep: 30, DisplaySleep: 10, DiskSleep: 20},
	}

	got := power.Merge(atual, power.PresetNeverSleep(), parcial)

	if got.Battery.Sleep != 0 || got.AC.Sleep != 0 {
		t.Errorf("o preset não chegou aos campos suportados: %+v", got)
	}
	if got.Battery.TCPKeepAlive || got.Battery.TTYsKeepAwake || got.Battery.HibernateMode != 0 {
		t.Errorf("o preset escreveu campos que a plataforma não grava: %+v", got.Battery)
	}
}

// Idempotência é o que mantém honesto o aviso de alterações pendentes: adotar
// o mesmo preset duas vezes não pode produzir um estado diferente.
func TestMergeEIdempotente(t *testing.T) {
	base := power.Settings{Battery: power.Profile{Sleep: 15}}
	uma := power.Merge(base, power.PresetNeverSleep(), parcial)
	duas := power.Merge(uma, power.PresetNeverSleep(), parcial)

	if uma != duas {
		t.Errorf("segunda aplicação mudou o estado:\n %+v\n %+v", uma, duas)
	}
}

func TestMergeNaoMutaOsOriginais(t *testing.T) {
	base := power.Settings{Battery: power.Profile{Sleep: 15}}
	preset := power.Settings{Battery: power.Profile{Sleep: 0}}

	power.Merge(base, preset, power.AllFeatures)

	if base.Battery.Sleep != 15 {
		t.Errorf("Merge mutou a base: Sleep = %d", base.Battery.Sleep)
	}
}

// Toda feature declarada precisa ter campo correspondente na tabela, ou o
// Manager anunciaria uma capacidade que a tela não sabe desenhar.
func TestCadaFeatureDeCampoTemUmCampo(t *testing.T) {
	visto := power.Feature(0)
	for _, f := range power.Fields() {
		if f.Feature == 0 {
			t.Errorf("campo %q não declara Feature", f.Label)
		}
		if visto&f.Feature != 0 {
			t.Errorf("campo %q repete a Feature de outro", f.Label)
		}
		visto |= f.Feature
	}
	// FeaturePasswordless é a única que não é campo do perfil.
	if want := power.AllFeatures &^ power.FeaturePasswordless; visto != want {
		t.Errorf("features cobertas = %b, quero %b", visto, want)
	}
}

func TestStepMinutesAndaNaEscala(t *testing.T) {
	tests := []struct {
		current, delta, want int
	}{
		{10, 1, 15},
		{10, -1, 5},
		{0, -1, 0},    // já no piso
		{180, 1, 180}, // já no teto
		{7, 1, 10},    // valor fora da escala sobe para o degrau seguinte
		// Descendo, o valor fora da escala primeiro se alinha ao degrau
		// abaixo dele (5) e só então desce — daí 2, e não 5.
		{7, -1, 2},
	}
	for _, tc := range tests {
		if got := power.StepMinutes(tc.current, tc.delta); got != tc.want {
			t.Errorf("StepMinutes(%d, %+d) = %d, quero %d", tc.current, tc.delta, got, tc.want)
		}
	}
}

func TestStepHibernateCiclaModosValidos(t *testing.T) {
	if got := power.StepHibernate(3, 1); got != 25 {
		t.Errorf("de 3 para cima = %d, quero 25", got)
	}
	if got := power.StepHibernate(3, -1); got != 0 {
		t.Errorf("de 3 para baixo = %d, quero 0", got)
	}
	// Um valor que o sistema não aceita cai no padrão em vez de propagar.
	if got := power.StepHibernate(7, 0); got != 3 {
		t.Errorf("valor inválido virou %d, quero 3", got)
	}
}
