package macos

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mateuslh/lealing/internal/core/power"
)

// Os erros são os do núcleo: a tela precisa distingui-los sem saber qual
// adapter está do outro lado.
var (
	// ErrAuthCanceled: o usuário fechou o diálogo de senha.
	ErrAuthCanceled = power.ErrAuthCanceled
	// ErrPowerRead: falha ao ler as configurações atuais.
	ErrPowerRead = power.ErrRead
	// ErrPowerApply: falha ao gravar as configurações.
	ErrPowerApply = power.ErrApply
)

// sudoersPath é o arquivo de regra que dispensa a senha para o pmset.
const sudoersPath = "/etc/sudoers.d/lealing-power"

// PowerManager implementa power.Manager sobre o pmset.
type PowerManager struct{}

var _ power.Manager = (*PowerManager)(nil)

// NewPowerManager monta o gerenciador.
func NewPowerManager() *PowerManager { return &PowerManager{} }

// Features implementa power.Manager: o pmset expõe o painel inteiro.
func (m *PowerManager) Features() power.Feature { return power.AllFeatures }

// Defaults implementa power.Manager, reproduzindo os padrões de fábrica da
// Apple para cada fonte.
func (m *PowerManager) Defaults() power.Settings {
	return power.Settings{
		Battery: power.Profile{
			Sleep: 5, DisplaySleep: 2, DiskSleep: 10,
			TCPKeepAlive: true, TTYsKeepAwake: true,
			Standby: true, ReduceBrightness: true,
			HibernateMode: 3,
		},
		AC: power.Profile{
			Sleep: 10, DisplaySleep: 10, DiskSleep: 10,
			PowerNap: true, TCPKeepAlive: true, WakeOnNetwork: true,
			TTYsKeepAwake: true, Standby: true,
			HibernateMode: 3,
		},
	}
}

// Read implementa power.Manager.
func (m *PowerManager) Read(ctx context.Context) (power.Settings, error) {
	out, err := runCombined(ctx, "/usr/bin/pmset", "-g", "custom")
	if err != nil {
		return power.Settings{}, fmt.Errorf("%w: %s", ErrPowerRead, firstLine(out))
	}
	return ParseCustom(out), nil
}

// ParseCustom interpreta a saída de `pmset -g custom`.
//
// Exportada porque é a peça testável desta implementação: dá para verificar
// o parser inteiro com uma string, sem tocar no sistema.
func ParseCustom(out string) power.Settings {
	var (
		settings power.Settings
		current  *power.Profile
	)

	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case line == "":
			continue
		case strings.HasPrefix(line, "Battery Power"):
			current = &settings.Battery
			continue
		case strings.HasPrefix(line, "AC Power"):
			current = &settings.AC
			continue
		case current == nil:
			continue
		}

		// As linhas são "chave   valor", mas há chaves com espaço
		// ("Sleep On Power Button 1"), então o valor é o último campo.
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		key := strings.Join(fields[:len(fields)-1], " ")
		value := fields[len(fields)-1]
		applySetting(current, key, value)
	}

	return settings
}

// applySetting escreve uma chave do pmset no perfil.
func applySetting(p *power.Profile, key, value string) {
	n, err := strconv.Atoi(value)
	if err != nil {
		return
	}
	on := n != 0

	switch key {
	case "sleep":
		p.Sleep = n
	case "displaysleep":
		p.DisplaySleep = n
	case "disksleep":
		p.DiskSleep = n
	case "powernap":
		p.PowerNap = on
	case "lowpowermode":
		p.LowPowerMode = on
	case "tcpkeepalive":
		p.TCPKeepAlive = on
	case "womp":
		p.WakeOnNetwork = on
	case "ttyskeepawake":
		p.TTYsKeepAwake = on
	case "standby":
		p.Standby = on
	case "lessbright":
		p.ReduceBrightness = on
	case "hibernatemode":
		p.HibernateMode = n
	}
}

// Apply implementa power.Manager.
//
// Tenta primeiro `sudo -n` (silencioso, funciona com a regra de sudoers
// instalada). Só se isso falhar recorre ao diálogo gráfico — que pede a
// senha uma vez para os dois perfis, em vez de duas.
func (m *PowerManager) Apply(ctx context.Context, s power.Settings) error {
	batteryArgs := pmsetArgs(power.Battery, s.Battery)
	acArgs := pmsetArgs(power.AC, s.AC)

	if m.sudoQuiet(ctx, batteryArgs) && m.sudoQuiet(ctx, acArgs) {
		return nil
	}

	command := "/usr/bin/pmset " + strings.Join(batteryArgs, " ") +
		" ; /usr/bin/pmset " + strings.Join(acArgs, " ")
	if err := runAsAdmin(ctx, command); err != nil {
		return err
	}
	return nil
}

// pmsetArgs monta os argumentos de um perfil.
//
// Todos os valores são inteiros gerados aqui — nada vem de texto do usuário,
// o que mantém a linha segura mesmo passando por um shell no caminho do
// diálogo de administrador.
func pmsetArgs(src power.Source, p power.Profile) []string {
	args := []string{
		src.Flag(),
		"sleep", strconv.Itoa(p.Sleep),
		"displaysleep", strconv.Itoa(p.DisplaySleep),
		"disksleep", strconv.Itoa(p.DiskSleep),
		"powernap", boolArg(p.PowerNap),
		"lowpowermode", boolArg(p.LowPowerMode),
		"tcpkeepalive", boolArg(p.TCPKeepAlive),
		"ttyskeepawake", boolArg(p.TTYsKeepAwake),
		"standby", boolArg(p.Standby),
		"hibernatemode", strconv.Itoa(p.HibernateMode),
	}
	// Cada fonte aceita só a sua chave exclusiva; passar a outra faz o pmset
	// recusar a linha inteira.
	if src == power.AC {
		args = append(args, "womp", boolArg(p.WakeOnNetwork))
	} else {
		args = append(args, "lessbright", boolArg(p.ReduceBrightness))
	}
	return args
}

func boolArg(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

// sudoQuiet tenta o pmset via sudo sem prompt.
func (m *PowerManager) sudoQuiet(ctx context.Context, args []string) bool {
	full := append([]string{"-n", "/usr/bin/pmset"}, args...)
	_, err := run(ctx, "/usr/bin/sudo", full...)
	return err == nil
}

// PasswordlessEnabled implementa power.Manager.
func (m *PowerManager) PasswordlessEnabled(ctx context.Context) bool {
	_, err := run(ctx, "/usr/bin/sudo", "-n", "/usr/bin/pmset", "-g")
	return err == nil
}

// EnablePasswordless implementa power.Manager.
//
// Instala a regra e a valida com visudo, removendo-a se a validação falhar:
// um sudoers inválido pode travar o sudo da máquina inteira.
func (m *PowerManager) EnablePasswordless(ctx context.Context) error {
	user, err := run(ctx, "/usr/bin/id", "-un")
	if err != nil || user == "" {
		return fmt.Errorf("%w: não foi possível determinar o usuário", ErrPowerApply)
	}
	if !safeUserName(user) {
		return fmt.Errorf("%w: nome de usuário inesperado", ErrPowerApply)
	}

	rule := user + " ALL=(ALL) NOPASSWD: /usr/bin/pmset"
	command := fmt.Sprintf(
		"/bin/echo '%s' > %s && /usr/sbin/chown root:wheel %s && /bin/chmod 440 %s && "+
			"( /usr/sbin/visudo -cf %s >/dev/null 2>&1 || /bin/rm -f %s )",
		rule, sudoersPath, sudoersPath, sudoersPath, sudoersPath, sudoersPath,
	)
	return runAsAdmin(ctx, command)
}

// DisablePasswordless implementa power.Manager.
func (m *PowerManager) DisablePasswordless(ctx context.Context) error {
	return runAsAdmin(ctx, "/bin/rm -f "+sudoersPath)
}

// safeUserName recusa nomes que poderiam escapar das aspas do shell.
func safeUserName(user string) bool {
	if user == "" || len(user) > 64 {
		return false
	}
	for _, r := range user {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '_', r == '-', r == '.':
		default:
			return false
		}
	}
	return true
}

// runAsAdmin executa um comando de shell com privilégio de administrador,
// usando o diálogo nativo do macOS.
//
// O diálogo é gráfico e aparece por cima do terminal; é o único caminho que
// funciona sem exigir que o usuário rode a TUI inteira sob sudo.
func runAsAdmin(ctx context.Context, command string) error {
	script := fmt.Sprintf("do shell script %s with administrator privileges", quoteAppleScript(command))
	out, err := runCombined(ctx, "/usr/bin/osascript", "-e", script)
	if err == nil {
		return nil
	}
	// -128 é o código do AppleScript para "usuário cancelou".
	if strings.Contains(out, "-128") || strings.Contains(strings.ToLower(out), "user canceled") {
		return ErrAuthCanceled
	}
	return fmt.Errorf("%w: %s", ErrPowerApply, firstLine(out))
}

// quoteAppleScript envolve o comando em aspas no formato do AppleScript.
func quoteAppleScript(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// firstLine reduz a saída de erro à primeira linha, que é a informativa.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}
