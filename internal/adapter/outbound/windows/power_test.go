package windows_test

import (
	"errors"
	"testing"

	"github.com/mateuslh/lealing/internal/adapter/outbound/windows"
	"github.com/mateuslh/lealing/internal/core/power"
)

// Amostra do settingsScript: o plano ativo ("Equilibrado"), os índices das
// três configurações que o lealing edita nas duas fontes, uma configuração
// que ele não edita e as do plano inativo — que precisam ser ignoradas, ou a
// tela mostraria os tempos do plano errado.
const settingsJSON = `{
 "Plan":"Microsoft:PowerPlan\\{381b4222-f694-41f0-9685-ff5bb260df2e}",
 "Name":"Equilibrado",
 "Settings":[
  {"InstanceID":"Microsoft:PowerSettingDataIndex\\{381b4222-f694-41f0-9685-ff5bb260df2e}\\AC\\{29f6c1db-86da-48c5-9fdb-f2b67b1f44da}","SettingIndexValue":1800},
  {"InstanceID":"Microsoft:PowerSettingDataIndex\\{381b4222-f694-41f0-9685-ff5bb260df2e}\\DC\\{29f6c1db-86da-48c5-9fdb-f2b67b1f44da}","SettingIndexValue":900},
  {"InstanceID":"Microsoft:PowerSettingDataIndex\\{381b4222-f694-41f0-9685-ff5bb260df2e}\\AC\\{3c0bc021-c8a8-4e07-a973-6b14cbcb2b7e}","SettingIndexValue":600},
  {"InstanceID":"Microsoft:PowerSettingDataIndex\\{381b4222-f694-41f0-9685-ff5bb260df2e}\\DC\\{3c0bc021-c8a8-4e07-a973-6b14cbcb2b7e}","SettingIndexValue":300},
  {"InstanceID":"Microsoft:PowerSettingDataIndex\\{381b4222-f694-41f0-9685-ff5bb260df2e}\\AC\\{6738e2c4-e8a5-4a42-b16a-e040e769756e}","SettingIndexValue":1200},
  {"InstanceID":"Microsoft:PowerSettingDataIndex\\{381b4222-f694-41f0-9685-ff5bb260df2e}\\DC\\{6738e2c4-e8a5-4a42-b16a-e040e769756e}","SettingIndexValue":600},
  {"InstanceID":"Microsoft:PowerSettingDataIndex\\{381b4222-f694-41f0-9685-ff5bb260df2e}\\AC\\{bd3b718a-0680-4d9d-8ab2-e1d2b4ac806d}","SettingIndexValue":1},
  {"InstanceID":"Microsoft:PowerSettingDataIndex\\{8c5e7fda-e8bf-4a96-9a85-a6e23a8c635c}\\AC\\{29f6c1db-86da-48c5-9fdb-f2b67b1f44da}","SettingIndexValue":0}
 ]}`

func TestParseSettings(t *testing.T) {
	got, err := windows.ParseSettings([]byte(settingsJSON))
	if err != nil {
		t.Fatal(err)
	}

	// Os índices vêm em segundos; o perfil fala em minutos.
	want := power.Settings{
		Battery: power.Profile{Sleep: 15, DisplaySleep: 5, DiskSleep: 10},
		AC:      power.Profile{Sleep: 30, DisplaySleep: 10, DiskSleep: 20},
	}
	if got.Battery != want.Battery {
		t.Errorf("bateria:\n got %+v\nquero %+v", got.Battery, want.Battery)
	}
	if got.AC != want.AC {
		t.Errorf("carregador:\n got %+v\nquero %+v", got.AC, want.AC)
	}
}

func TestParseSettingsRecusaEntradaInutil(t *testing.T) {
	for name, raw := range map[string]string{
		"vazio":       "",
		"não é json":  "Get-CimInstance : Namespace inválido",
		"sem plano":   `{"Plan":"","Settings":[]}`,
		"plano torto": `{"Plan":"Microsoft:PowerPlan","Settings":[]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := windows.ParseSettings([]byte(raw)); !errors.Is(err, power.ErrRead) {
				t.Errorf("erro = %v, quero um %v", err, power.ErrRead)
			}
		})
	}
}

// Uma linha estranha no meio da lista não pode derrubar as boas: o WMI
// devolve dezenas de configurações e basta uma mudar de forma.
func TestParseSettingsIgnoraLinhasQuebradas(t *testing.T) {
	raw := `{"Plan":"Microsoft:PowerPlan\\{381b4222-f694-41f0-9685-ff5bb260df2e}","Settings":[
  {"InstanceID":"lixo","SettingIndexValue":10},
  {"InstanceID":"Microsoft:PowerSettingDataIndex\\{381b4222-f694-41f0-9685-ff5bb260df2e}\\AC","SettingIndexValue":10},
  {"InstanceID":"Microsoft:PowerSettingDataIndex\\{381b4222-f694-41f0-9685-ff5bb260df2e}\\AC\\{29f6c1db-86da-48c5-9fdb-f2b67b1f44da}","SettingIndexValue":1800}
 ]}`

	got, err := windows.ParseSettings([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if got.AC.Sleep != 30 {
		t.Errorf("Sleep no carregador = %d, quero 30 (a linha válida deve passar)", got.AC.Sleep)
	}
}

// O contrato de capacidades é o que a tela consulta para decidir o que
// desenhar: se ele crescer sem o Apply crescer junto, a tela passa a oferecer
// controles que não chegam ao powercfg.
func TestFeaturesDeclaradasSaoAsQueOApplyEscreve(t *testing.T) {
	feats := windows.NewPowerManager().Features()

	want := power.FeatureSleep | power.FeatureDisplaySleep | power.FeatureDiskSleep
	if feats != want {
		t.Errorf("Features = %b, quero %b", feats, want)
	}
	if feats.Has(power.FeaturePasswordless) {
		t.Error("o Windows não tem senha a dispensar para mudar o plano de energia")
	}
	for _, src := range []power.Source{power.Battery, power.AC} {
		if n := len(power.VisibleFields(src, feats)); n != 3 {
			t.Errorf("%v: %d campos visíveis, quero 3", src, n)
		}
	}
}

func TestPasswordlessNaoSuportado(t *testing.T) {
	m := windows.NewPowerManager()
	if !m.PasswordlessEnabled(t.Context()) {
		t.Error("aplicar não pede elevação: nada fica pendente de senha")
	}
	if err := m.EnablePasswordless(t.Context()); !errors.Is(err, power.ErrUnsupported) {
		t.Errorf("erro = %v, quero um %v", err, power.ErrUnsupported)
	}
	if err := m.DisablePasswordless(t.Context()); !errors.Is(err, power.ErrUnsupported) {
		t.Errorf("erro = %v, quero um %v", err, power.ErrUnsupported)
	}
}
