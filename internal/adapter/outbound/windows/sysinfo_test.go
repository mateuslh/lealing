package windows_test

import (
	"testing"

	"github.com/mateuslh/lealing/internal/adapter/outbound/windows"
	"github.com/mateuslh/lealing/internal/core/sysinfo"
)

// Saída real do snapshotScript em um notebook com Windows 11. O parser é a
// peça frágil da integração — o WMI mistura tipos, enche o nome do
// processador de espaços e usa um sentinela absurdo para "não sei estimar" —,
// então vale travá-lo contra uma amostra verdadeira.
const laptopJSON = `{"Caption":"Microsoft Windows 11 Pro","Version":"10.0.26100","Build":"26100",` +
	`"Host":"DESKTOP-4F2K1","Vendor":"Dell Inc.","Model":"XPS 15 9530",` +
	`"Chip":"13th Gen Intel(R)  Core(TM) i7-13700H","Cores":"20","Memory":"34089504768",` +
	`"UptimeSeconds":"183960","BatteryStatus":"2","BatteryCharge":"98","BatteryMinutes":"71582788"}`

func TestParseSnapshot(t *testing.T) {
	got := windows.ParseSnapshot([]byte(laptopJSON))

	want := map[string]string{
		"OSVersion": "Windows 11 Pro 10.0.26100 (26100)",
		"HostName":  "DESKTOP-4F2K1",
		"Model":     "Dell Inc. XPS 15 9530",
		"Chip":      "13th Gen Intel(R) Core(TM) i7-13700H",
		"Cores":     "20 núcleos lógicos",
		"Memory":    "32 GB",
		"Uptime":    "2d 3h 6min",
	}
	fields := map[string]string{
		"OSVersion": got.OSVersion, "HostName": got.HostName, "Model": got.Model,
		"Chip": got.Chip, "Cores": got.Cores, "Memory": got.Memory, "Uptime": got.Uptime,
	}
	for name, want := range want {
		if fields[name] != want {
			t.Errorf("%s = %q, quero %q", name, fields[name], want)
		}
	}

	if !got.HasBattery {
		t.Fatal("bateria não detectada")
	}
	if got.BatteryLevel != "98%" {
		t.Errorf("carga = %q, quero 98%%", got.BatteryLevel)
	}
	// 71582788 é o "não sei" do WMI: não pode virar "1193046h restantes".
	if got.BatteryState != "Ligado à tomada" {
		t.Errorf("estado = %q, quero %q", got.BatteryState, "Ligado à tomada")
	}
}

func TestParseSnapshotEstimaTempoRestante(t *testing.T) {
	raw := `{"Caption":"Microsoft Windows 11 Home","Version":"10.0.22631","Build":"22631",` +
		`"BatteryStatus":"1","BatteryCharge":"51","BatteryMinutes":"372"}`

	got := windows.ParseSnapshot([]byte(raw))
	if want := "Na bateria · 6h12 restantes"; got.BatteryState != want {
		t.Errorf("estado = %q, quero %q", got.BatteryState, want)
	}
}

func TestParseSnapshotSemBateria(t *testing.T) {
	// Desktop: a classe Win32_Battery não existe e os campos vêm vazios.
	raw := `{"Caption":"Microsoft Windows 11 Pro","Version":"10.0.26100","Build":"26100",` +
		`"Host":"TORRE","BatteryStatus":"","BatteryCharge":"","BatteryMinutes":""}`

	got := windows.ParseSnapshot([]byte(raw))
	if got.HasBattery {
		t.Error("desktop reportado com bateria")
	}
	if got.OSVersion == sysinfo.Unknown {
		t.Error("a ausência da bateria derrubou o resto da leitura")
	}
}

// Campo ilegível vira traço, nunca erro: uma tela de diagnóstico que não abre
// porque o WMI não respondeu por uma classe é pior que uma com um traço.
func TestParseSnapshotDegradaEmVezDeFalhar(t *testing.T) {
	for name, raw := range map[string]string{
		"vazio":            "",
		"não é json":       "Get-CimInstance : Acesso negado.",
		"objeto vazio":     "{}",
		"campos numéricos": `{"Cores":"não sei","Memory":"?","UptimeSeconds":"-3"}`,
	} {
		t.Run(name, func(t *testing.T) {
			got := windows.ParseSnapshot([]byte(raw))
			if got.OSVersion != sysinfo.Unknown || got.Memory != sysinfo.Unknown {
				t.Errorf("campos ilegíveis não caíram para %q: %+v", sysinfo.Unknown, got)
			}
			if got.HasBattery {
				t.Error("bateria inventada a partir de entrada inválida")
			}
		})
	}
}
