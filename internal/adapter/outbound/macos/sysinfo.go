package macos

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mateuslh/lealing/internal/core/sysinfo"
)

// SystemInspector implementa sysinfo.Inspector lendo o macOS.
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
// Nenhum campo indisponível derruba a leitura: cada um cai para "—" por
// conta própria. Uma tela de diagnóstico que se recusa a abrir porque um
// sysctl não existe é pior que uma tela com um traço.
func (i *SystemInspector) Inspect(ctx context.Context) (sysinfo.Snapshot, error) {
	s := sysinfo.Snapshot{
		OSVersion: sysinfo.Unknown,
		HostName:  sysinfo.Unknown,
		Model:     sysinfo.Unknown,
		Chip:      sysinfo.Unknown,
		Cores:     sysinfo.Unknown,
		Memory:    sysinfo.Unknown,
		Uptime:    sysinfo.Unknown,
	}

	if v := i.osVersion(ctx); v != "" {
		s.OSVersion = v
	}
	if v := i.hostName(ctx); v != "" {
		s.HostName = v
	}
	if v := sysctl(ctx, "hw.model"); v != "" {
		s.Model = v
	}
	if v := firstNonEmpty(sysctl(ctx, "machdep.cpu.brand_string"), sysctl(ctx, "hw.model")); v != "" {
		s.Chip = v
	}
	if v := sysctl(ctx, "hw.ncpu"); v != "" {
		s.Cores = v + " núcleos"
	}
	if v := sysctl(ctx, "hw.memsize"); v != "" {
		if bytes, err := strconv.ParseUint(v, 10, 64); err == nil {
			s.Memory = formatGB(bytes)
		}
	}
	if up, ok := i.uptime(ctx); ok {
		s.Uptime = formatUptime(up)
	}

	level, state, has := i.battery(ctx)
	s.BatteryLevel, s.BatteryState, s.HasBattery = level, state, has

	return s, nil
}

// osVersion monta "macOS 27.0 (26A5353q)" a partir do sw_vers.
func (i *SystemInspector) osVersion(ctx context.Context) string {
	version, err := run(ctx, "/usr/bin/sw_vers", "-productVersion")
	if err != nil || version == "" {
		return ""
	}
	build, err := run(ctx, "/usr/bin/sw_vers", "-buildVersion")
	if err != nil || build == "" {
		return "macOS " + version
	}
	return fmt.Sprintf("macOS %s (%s)", version, build)
}

// hostName prefere o nome amigável do Mac ao hostname de rede.
func (i *SystemInspector) hostName(ctx context.Context) string {
	if name, err := run(ctx, "/usr/sbin/scutil", "--get", "ComputerName"); err == nil && name != "" {
		return name
	}
	name, err := run(ctx, "/bin/hostname")
	if err != nil {
		return ""
	}
	return name
}

// bootTimeRe extrai os segundos do formato "{ sec = 1783274715, usec = ... }".
var bootTimeRe = regexp.MustCompile(`sec\s*=\s*(\d+)`)

// uptime calcula o tempo ligado a partir de kern.boottime.
func (i *SystemInspector) uptime(ctx context.Context) (time.Duration, bool) {
	raw := sysctl(ctx, "kern.boottime")
	match := bootTimeRe.FindStringSubmatch(raw)
	if len(match) != 2 {
		return 0, false
	}
	secs, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		return 0, false
	}
	d := i.now().Sub(time.Unix(secs, 0))
	if d < 0 {
		return 0, false
	}
	return d, true
}

// batteryLevelRe captura a porcentagem na saída do pmset.
var batteryLevelRe = regexp.MustCompile(`(\d+)%`)

// battery lê carga e estado. O terceiro retorno é falso em máquinas sem
// bateria, o que é informação, não erro.
func (i *SystemInspector) battery(ctx context.Context) (level, state string, has bool) {
	out, err := run(ctx, "/usr/bin/pmset", "-g", "batt")
	if err != nil {
		return sysinfo.Unknown, sysinfo.Unknown, false
	}
	if !strings.Contains(out, "InternalBattery") {
		return "", "", false
	}

	level = sysinfo.Unknown
	if m := batteryLevelRe.FindString(out); m != "" {
		level = m
	}

	lower := strings.ToLower(out)
	switch {
	case strings.Contains(lower, "charged"):
		state = "Carregada"
	case strings.Contains(lower, "charging"):
		state = "Carregando"
	case strings.Contains(lower, "discharging"):
		state = "Na bateria"
	case strings.Contains(lower, "ac attached"):
		state = "Ligada à tomada"
	default:
		state = sysinfo.Unknown
	}

	// O pmset informa o tempo restante quando consegue estimá-lo; "0:00" e
	// "(no estimate)" significam que ainda está calculando.
	if rem := remainingTime(out); rem != "" {
		state += " · " + rem + " restantes"
	}
	return level, state, true
}

var remainingRe = regexp.MustCompile(`(\d+):(\d{2}) remaining`)

func remainingTime(out string) string {
	m := remainingRe.FindStringSubmatch(out)
	if len(m) != 3 || (m[1] == "0" && m[2] == "00") {
		return ""
	}
	return m[1] + "h" + m[2]
}

// formatGB apresenta bytes em gigabytes inteiros, como o painel da Apple.
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
	parts = append(parts, strconv.Itoa(mins)+"min")
	return strings.Join(parts, " ")
}
