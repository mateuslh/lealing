package windows

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mateuslh/lealing/internal/core/sysinfo"
)

// snapshotScript lê tudo em uma só invocação.
//
// Uma chamada, e não sete: subir o PowerShell custa perto de meio segundo, e
// pagar isso por campo deixaria a tela em branco por vários segundos. Todos
// os valores saem como texto para que o parser não dependa de como o
// ConvertTo-Json decide representar um uint64 ou uma data.
const snapshotScript = `$ErrorActionPreference='SilentlyContinue'
$os=Get-CimInstance -ClassName Win32_OperatingSystem
$cs=Get-CimInstance -ClassName Win32_ComputerSystem
$cpu=@(Get-CimInstance -ClassName Win32_Processor)[0]
$bat=@(Get-CimInstance -ClassName Win32_Battery)[0]
$up=''
if($os.LastBootUpTime){$up=[string][int]((Get-Date)-$os.LastBootUpTime).TotalSeconds}
ConvertTo-Json -Compress -InputObject ([pscustomobject]@{
 Caption=[string]$os.Caption
 Version=[string]$os.Version
 Build=[string]$os.BuildNumber
 Host=[string]$cs.Name
 Vendor=[string]$cs.Manufacturer
 Model=[string]$cs.Model
 Chip=[string]$cpu.Name
 Cores=[string]$cpu.NumberOfLogicalProcessors
 Memory=[string]$cs.TotalPhysicalMemory
 UptimeSeconds=$up
 BatteryStatus=[string]$bat.BatteryStatus
 BatteryCharge=[string]$bat.EstimatedChargeRemaining
 BatteryMinutes=[string]$bat.EstimatedRunTime
})`

// SystemInspector implementa sysinfo.Inspector lendo o Windows via CIM.
type SystemInspector struct {
	now func() time.Time
}

var _ sysinfo.Inspector = (*SystemInspector)(nil)

// NewSystemInspector monta o inspetor. Um now nil usa o relógio do sistema.
func NewSystemInspector(now func() time.Time) *SystemInspector {
	if now == nil {
		now = time.Now
	}
	return &SystemInspector{now: now}
}

// Inspect implementa sysinfo.Inspector.
//
// Como no adapter do macOS, campo ilegível vira traço: uma tela de
// diagnóstico que se recusa a abrir porque o WMI não respondeu por uma
// classe é pior que uma tela com um traço.
func (i *SystemInspector) Inspect(ctx context.Context) (sysinfo.Snapshot, error) {
	out, err := powershell(ctx, snapshotScript)
	if err != nil && out == "" {
		return emptySnapshot(), fmt.Errorf("falha ao consultar o WMI: %w", err)
	}
	return ParseSnapshot([]byte(out)), nil
}

// rawSnapshot é o JSON emitido pelo snapshotScript. Todos os campos são
// texto — ver o comentário do script.
type rawSnapshot struct {
	Caption        string
	Version        string
	Build          string
	Host           string
	Vendor         string
	Model          string
	Chip           string
	Cores          string
	Memory         string
	UptimeSeconds  string
	BatteryStatus  string
	BatteryCharge  string
	BatteryMinutes string
}

// ParseSnapshot converte a saída do snapshotScript em um Snapshot.
//
// Exportada porque é a peça testável desta implementação: dá para verificar o
// mapeamento inteiro com uma string, sem Windows à mão.
func ParseSnapshot(raw []byte) sysinfo.Snapshot {
	s := emptySnapshot()

	var r rawSnapshot
	if err := json.Unmarshal(raw, &r); err != nil {
		return s
	}

	if v := osVersion(r); v != "" {
		s.OSVersion = v
	}
	if r.Host != "" {
		s.HostName = r.Host
	}
	if v := strings.TrimSpace(r.Vendor + " " + r.Model); v != "" {
		s.Model = v
	}
	if r.Chip != "" {
		s.Chip = strings.Join(strings.Fields(r.Chip), " ") // o WMI enche o nome de espaços duplos
	}
	if n, err := strconv.Atoi(r.Cores); err == nil && n > 0 {
		s.Cores = strconv.Itoa(n) + " núcleos lógicos"
	}
	if b, err := strconv.ParseUint(r.Memory, 10, 64); err == nil && b > 0 {
		s.Memory = formatGB(b)
	}
	if secs, err := strconv.ParseInt(r.UptimeSeconds, 10, 64); err == nil && secs > 0 {
		s.Uptime = formatUptime(time.Duration(secs) * time.Second)
	}

	s.BatteryLevel, s.BatteryState, s.HasBattery = battery(r)
	return s
}

// osVersion monta "Windows 11 Pro 10.0.26100 (26100)".
func osVersion(r rawSnapshot) string {
	name := strings.TrimSpace(strings.TrimPrefix(r.Caption, "Microsoft "))
	switch {
	case name == "" && r.Version == "":
		return ""
	case name == "":
		return "Windows " + r.Version
	case r.Version == "":
		return name
	case r.Build == "":
		return name + " " + r.Version
	default:
		return fmt.Sprintf("%s %s (%s)", name, r.Version, r.Build)
	}
}

// batteryStates traduz o código do Win32_Battery.
//
// A tabela do WMI mistura carga e alimentação no mesmo campo, e alguns
// códigos ("Unknown") só aparecem na prática com a máquina na tomada — daí a
// tradução ser por intenção, não literal.
var batteryStates = map[int]string{
	1:  "Na bateria",
	2:  "Ligado à tomada",
	3:  "Carregada",
	4:  "Na bateria · carga baixa",
	5:  "Na bateria · carga crítica",
	6:  "Carregando",
	7:  "Carregando",
	8:  "Carregando · carga baixa",
	9:  "Carregando · carga crítica",
	11: "Parcialmente carregada",
}

// maxRuntimeMinutes descarta a estimativa impossível que o WMI devolve
// quando não sabe (71582788, o "indefinido" da classe).
const maxRuntimeMinutes = 3 * 24 * 60

// battery lê carga e estado. O terceiro retorno é falso em desktops, o que é
// informação, não erro.
func battery(r rawSnapshot) (level, state string, has bool) {
	code, err := strconv.Atoi(r.BatteryStatus)
	if err != nil {
		// Sem a classe Win32_Battery não há bateria a relatar.
		return "", "", false
	}

	level = sysinfo.Unknown
	if pct, err := strconv.Atoi(r.BatteryCharge); err == nil {
		level = strconv.Itoa(pct) + "%"
	}

	state = batteryStates[code]
	if state == "" {
		state = sysinfo.Unknown
	}
	if mins, err := strconv.Atoi(r.BatteryMinutes); err == nil && mins > 0 && mins < maxRuntimeMinutes {
		state += fmt.Sprintf(" · %dh%02d restantes", mins/60, mins%60)
	}
	return level, state, true
}

// emptySnapshot é o retrato com todos os campos marcados como ilegíveis.
func emptySnapshot() sysinfo.Snapshot {
	return sysinfo.Snapshot{
		OSVersion: sysinfo.Unknown,
		HostName:  sysinfo.Unknown,
		Model:     sysinfo.Unknown,
		Chip:      sysinfo.Unknown,
		Cores:     sysinfo.Unknown,
		Memory:    sysinfo.Unknown,
		Uptime:    sysinfo.Unknown,
	}
}

// formatGB apresenta bytes em gigabytes inteiros.
func formatGB(bytes uint64) string {
	const gb = 1 << 30
	return fmt.Sprintf("%.0f GB", float64(bytes)/gb)
}

// formatUptime encurta a duração para "3d 4h 12min".
func formatUptime(d time.Duration) string {
	total := int(d.Seconds())
	days := total / 86400
	hours := (total % 86400) / 3600
	mins := (total % 3600) / 60

	var parts []string
	if days > 0 {
		parts = append(parts, strconv.Itoa(days)+"d")
	}
	if hours > 0 {
		parts = append(parts, strconv.Itoa(hours)+"h")
	}
	return strings.Join(append(parts, strconv.Itoa(mins)+"min"), " ")
}
