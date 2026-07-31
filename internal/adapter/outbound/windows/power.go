package windows

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/mateuslh/lealing/internal/core/power"
)

// GUIDs das configurações que o lealing edita. São constantes do Windows,
// idênticas em qualquer instalação e em qualquer idioma — e é justamente por
// isso que a leitura vai pelo CIM e não pela saída do `powercfg /query`, cujos
// rótulos são traduzidos e mudariam o parser conforme o idioma da máquina.
const (
	subSleep    = "238c9fa8-0aad-41ed-83f4-97be242c8f20"
	subVideo    = "7516b95f-f776-4464-8c53-06167f40cc99"
	subDisk     = "0012ee47-9041-4b5d-9b77-535fba8b1442"
	guidStandby = "29f6c1db-86da-48c5-9fdb-f2b67b1f44da" // dormir o sistema
	guidVideo   = "3c0bc021-c8a8-4e07-a973-6b14cbcb2b7e" // desligar a tela
	guidDisk    = "6738e2c4-e8a5-4a42-b16a-e040e769756e" // dormir o disco
)

// settingsScript despeja o plano ativo e todos os índices de configuração.
//
// O filtro por plano fica no Go, e não aqui, para que o parser possa ser
// exercitado com uma amostra real — que traz os cinco planos da máquina —
// em vez de uma já recortada.
const settingsScript = `$ErrorActionPreference='Stop'
$plan=Get-CimInstance -Namespace root\cimv2\power -ClassName Win32_PowerPlan -Filter 'IsActive = True'
$rows=@(Get-CimInstance -Namespace root\cimv2\power -ClassName Win32_PowerSettingDataIndex |
 Select-Object InstanceID,SettingIndexValue)
ConvertTo-Json -Compress -Depth 4 -InputObject ([pscustomobject]@{
 Plan=[string]$plan.InstanceID
 Name=[string]$plan.ElementName
 Settings=$rows
})`

// PowerManager implementa power.Manager sobre o powercfg.
type PowerManager struct{}

var _ power.Manager = (*PowerManager)(nil)

// NewPowerManager monta o gerenciador.
func NewPowerManager() *PowerManager { return &PowerManager{} }

// Features implementa power.Manager.
//
// O powercfg cobre os três tempos de inatividade e nada mais do que o pmset
// oferece: Power Nap, standby, tcpkeepalive e hibernatemode não têm
// equivalente que signifique a mesma coisa, e mapeá-los para o ajuste
// "parecido" mais próximo mudaria o comportamento da máquina sem o usuário
// pedir. A dispensa de senha também fica de fora — mudar o plano de energia
// do próprio usuário não pede elevação no Windows.
func (m *PowerManager) Features() power.Feature {
	return power.FeatureSleep | power.FeatureDisplaySleep | power.FeatureDiskSleep
}

// Defaults implementa power.Manager com os tempos do plano "Equilibrado".
func (m *PowerManager) Defaults() power.Settings {
	return power.Settings{
		Battery: power.Profile{Sleep: 15, DisplaySleep: 5, DiskSleep: 10},
		AC:      power.Profile{Sleep: 30, DisplaySleep: 10, DiskSleep: 20},
	}
}

// Read implementa power.Manager.
func (m *PowerManager) Read(ctx context.Context) (power.Settings, error) {
	out, err := powershell(ctx, settingsScript)
	if err != nil {
		return power.Settings{}, fmt.Errorf("%w: %s", power.ErrRead, firstLine(out))
	}
	return ParseSettings([]byte(out))
}

// rawSettings é o JSON emitido pelo settingsScript.
type rawSettings struct {
	Plan     string
	Name     string
	Settings []struct {
		InstanceID        string
		SettingIndexValue json.Number
	}
}

// ParseSettings interpreta a saída do settingsScript.
//
// Exportada porque é a peça testável desta implementação: o formato do
// InstanceID (…\{plano}\AC\{configuração}) é a parte com bug em potencial, e
// exportá-la permite verificá-la com uma string, sem tocar no sistema.
func ParseSettings(raw []byte) (power.Settings, error) {
	var r rawSettings
	if err := json.Unmarshal(raw, &r); err != nil {
		return power.Settings{}, fmt.Errorf("%w: resposta ilegível do WMI", power.ErrRead)
	}

	plan := guidOf(r.Plan)
	if plan == "" {
		return power.Settings{}, fmt.Errorf("%w: nenhum plano de energia ativo", power.ErrRead)
	}

	var settings power.Settings
	for _, row := range r.Settings {
		// Microsoft:PowerSettingDataIndex\{plano}\AC\{configuração}
		parts := strings.Split(row.InstanceID, `\`)
		if len(parts) != 4 || !strings.EqualFold(guidOf(parts[1]), plan) {
			continue
		}

		seconds, err := strconv.Atoi(row.SettingIndexValue.String())
		if err != nil || seconds < 0 {
			continue
		}

		profile := &settings.Battery
		if strings.EqualFold(parts[2], "AC") {
			profile = &settings.AC
		}
		// Os índices são em segundos; a tela e o pmset falam em minutos.
		applySetting(profile, strings.ToLower(guidOf(parts[3])), seconds/60)
	}
	return settings, nil
}

// applySetting escreve uma configuração conhecida no perfil. GUIDs que o
// lealing não edita — e há dezenas — passam batido.
func applySetting(p *power.Profile, setting string, minutes int) {
	switch setting {
	case guidStandby:
		p.Sleep = minutes
	case guidVideo:
		p.DisplaySleep = minutes
	case guidDisk:
		p.DiskSleep = minutes
	}
}

// guidOf extrai o GUID de "…\{xxxx-…}" ou de "{xxxx-…}", sem as chaves.
func guidOf(s string) string {
	open := strings.LastIndexByte(s, '{')
	closeAt := strings.LastIndexByte(s, '}')
	if open < 0 || closeAt < open {
		return ""
	}
	return s[open+1 : closeAt]
}

// Apply implementa power.Manager.
//
// Cada par (fonte, configuração) é uma invocação do powercfg, e o /setactive
// final é o que faz o plano em edição valer de fato — sem ele, o Windows
// guarda os índices mas continua rodando os antigos.
func (m *PowerManager) Apply(ctx context.Context, s power.Settings) error {
	type change struct {
		flag       string // /setacvalueindex ou /setdcvalueindex
		sub, guid  string
		minutes    int
		sourceName string
	}

	changes := []change{
		{"/setdcvalueindex", subSleep, guidStandby, s.Battery.Sleep, "bateria"},
		{"/setdcvalueindex", subVideo, guidVideo, s.Battery.DisplaySleep, "bateria"},
		{"/setdcvalueindex", subDisk, guidDisk, s.Battery.DiskSleep, "bateria"},
		{"/setacvalueindex", subSleep, guidStandby, s.AC.Sleep, "carregador"},
		{"/setacvalueindex", subVideo, guidVideo, s.AC.DisplaySleep, "carregador"},
		{"/setacvalueindex", subDisk, guidDisk, s.AC.DiskSleep, "carregador"},
	}

	for _, c := range changes {
		if c.minutes < 0 {
			continue
		}
		// Os argumentos são GUIDs constantes e um inteiro gerado aqui: nada
		// vem de texto do usuário.
		out, err := run(ctx, "powercfg.exe", c.flag, "SCHEME_CURRENT",
			c.sub, c.guid, strconv.Itoa(c.minutes*60))
		if err != nil {
			return fmt.Errorf("%w (%s): %s", power.ErrApply, c.sourceName, firstLine(out))
		}
	}

	if out, err := run(ctx, "powercfg.exe", "/setactive", "SCHEME_CURRENT"); err != nil {
		return fmt.Errorf("%w: %s", power.ErrApply, firstLine(out))
	}
	return nil
}

// PasswordlessEnabled implementa power.Manager.
//
// Sempre verdadeiro: aplicar não pede elevação, então não há senha pendente
// a anunciar. A tela esconde o controle porque FeaturePasswordless não é
// declarada; este retorno é o que mantém o rodapé coerente caso apareça.
func (m *PowerManager) PasswordlessEnabled(context.Context) bool { return true }

// EnablePasswordless implementa power.Manager.
func (m *PowerManager) EnablePasswordless(context.Context) error {
	return fmt.Errorf("%w: o Windows não pede senha para mudar o plano de energia", power.ErrUnsupported)
}

// DisablePasswordless implementa power.Manager.
func (m *PowerManager) DisablePasswordless(context.Context) error {
	return fmt.Errorf("%w: o Windows não pede senha para mudar o plano de energia", power.ErrUnsupported)
}
