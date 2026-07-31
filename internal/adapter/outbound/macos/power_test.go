package macos_test

import (
	"testing"

	"github.com/mateuslh/lealing/internal/adapter/outbound/macos"
	"github.com/mateuslh/lealing/internal/core/power"
)

// Saída real de `pmset -g custom` neste Mac. O parser é a peça mais frágil
// da integração — o formato tem chaves com espaço no nome ("Sleep On Power
// Button 1") e seções separadas apenas por cabeçalho —, então vale travá-lo
// contra uma amostra verdadeira.
const sample = `Battery Power:
 Sleep On Power Button 1
 lowpowermode         0
 standby              0
 ttyskeepawake        1
 hibernatemode        3
 powernap             0
 hibernatefile        /var/vm/sleepimage
 displaysleep         60
 womp                 0
 networkoversleep     0
 sleep                60
 lessbright           1
 tcpkeepalive         1
 disksleep            60
 SleepServices        0
AC Power:
 Sleep On Power Button 1
 lowpowermode         0
 standby              1
 ttyskeepawake        1
 hibernatemode        25
 powernap             1
 hibernatefile        /var/vm/sleepimage
 displaysleep         10
 womp                 1
 networkoversleep     0
 sleep                10
 tcpkeepalive         1
 disksleep            10
 SleepServices        1
`

func TestParseCustom(t *testing.T) {
	got := macos.ParseCustom(sample)

	want := power.Settings{
		Battery: power.Profile{
			Sleep: 60, DisplaySleep: 60, DiskSleep: 60,
			TCPKeepAlive: true, TTYsKeepAwake: true,
			ReduceBrightness: true, HibernateMode: 3,
		},
		AC: power.Profile{
			Sleep: 10, DisplaySleep: 10, DiskSleep: 10,
			PowerNap: true, TCPKeepAlive: true, WakeOnNetwork: true,
			TTYsKeepAwake: true, Standby: true, HibernateMode: 25,
		},
	}

	if got.Battery != want.Battery {
		t.Errorf("bateria:\n got %+v\nquero %+v", got.Battery, want.Battery)
	}
	if got.AC != want.AC {
		t.Errorf("carregador:\n got %+v\nquero %+v", got.AC, want.AC)
	}
}

func TestParseCustomIgnoraLixo(t *testing.T) {
	// Entrada vazia, sem cabeçalho de seção ou com valores não numéricos não
	// pode gerar pânico nem perfil parcialmente preenchido com lixo.
	for name, input := range map[string]string{
		"vazio":          "",
		"sem seção":      "sleep 30\ndisplaysleep 10\n",
		"valor textual":  "Battery Power:\n sleep abc\n displaysleep 5\n",
		"linha truncada": "Battery Power:\n sleep\n",
	} {
		t.Run(name, func(t *testing.T) {
			got := macos.ParseCustom(input)
			if name == "valor textual" && got.Battery.DisplaySleep != 5 {
				t.Errorf("DisplaySleep = %d, quero 5 (a linha válida deve passar)",
					got.Battery.DisplaySleep)
			}
			if name == "sem seção" && got.Battery != (power.Profile{}) {
				t.Errorf("linhas fora de seção foram aplicadas: %+v", got.Battery)
			}
		})
	}
}

func TestProfileSummary(t *testing.T) {
	tests := map[int]string{
		0:  "Nunca dorme",
		30: "Dorme após 30 min de inatividade",
		60: "Dorme após 1h de inatividade",
		90: "Dorme após 1h 30min de inatividade",
	}
	for sleep, want := range tests {
		got := power.Profile{Sleep: sleep}.Summary()
		if got != want {
			t.Errorf("Sleep=%d → %q, quero %q", sleep, got, want)
		}
	}
}

func TestSettingsWithNaoMutaOriginal(t *testing.T) {
	original := power.Settings{Battery: power.Profile{Sleep: 10}}
	modified := original.With(power.Battery, power.Profile{Sleep: 99})

	if original.Battery.Sleep != 10 {
		t.Errorf("With mutou o original: Sleep = %d", original.Battery.Sleep)
	}
	if modified.Battery.Sleep != 99 {
		t.Errorf("With não aplicou: Sleep = %d", modified.Battery.Sleep)
	}
}
