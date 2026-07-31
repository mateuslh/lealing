// Package power é o domínio da tool "Controle de Energia".
//
// Modela as configurações de energia de uma máquina sem saber quem as guarda:
// ler e escrever é responsabilidade da porta Manager, e o painel que cada
// sistema oferece é declarado por ela em Features.
package power

import (
	"context"
	"errors"
)

// Erros que a tela distingue para escolher a mensagem certa. Vivem no núcleo
// porque a tela precisa compará-los sem importar o adapter da plataforma —
// era isso, e não o pmset, que a fazia depender do macOS.
var (
	// ErrAuthCanceled: o usuário fechou o diálogo de autenticação.
	ErrAuthCanceled = errors.New("autenticação cancelada")
	// ErrRead: falha ao ler as configurações atuais.
	ErrRead = errors.New("falha ao ler as configurações de energia")
	// ErrApply: falha ao gravar as configurações.
	ErrApply = errors.New("falha ao aplicar as configurações de energia")
	// ErrUnsupported: a operação não existe nesta plataforma.
	ErrUnsupported = errors.New("operação não suportada nesta plataforma")
)

// Profile são as configurações de energia de uma fonte de alimentação.
// Os campos de tempo são em minutos, onde 0 significa "nunca".
type Profile struct {
	Sleep        int // pmset sleep        — dormir do sistema
	DisplaySleep int // pmset displaysleep — desligar a tela
	DiskSleep    int // pmset disksleep    — dormir do disco

	PowerNap         bool // pmset powernap
	LowPowerMode     bool // pmset lowpowermode
	TCPKeepAlive     bool // pmset tcpkeepalive
	WakeOnNetwork    bool // pmset womp       — só faz sentido no carregador
	TTYsKeepAwake    bool // pmset ttyskeepawake
	Standby          bool // pmset standby
	ReduceBrightness bool // pmset lessbright — só faz sentido na bateria

	HibernateMode int // pmset hibernatemode — 0, 3 ou 25
}

// Source identifica a fonte de alimentação de um perfil.
type Source uint8

const (
	// Battery é o perfil aplicado quando o Mac está na bateria.
	Battery Source = iota
	// AC é o perfil aplicado quando o Mac está no carregador.
	AC
)

// String implementa fmt.Stringer.
func (s Source) String() string {
	if s == AC {
		return "Carregador"
	}
	return "Bateria"
}

// Flag é o argumento do pmset correspondente à fonte.
func (s Source) Flag() string {
	if s == AC {
		return "-c"
	}
	return "-b"
}

// Summary resume o perfil em uma linha, para o cabeçalho do painel.
func (p Profile) Summary() string {
	if p.Sleep == 0 {
		return "Nunca dorme"
	}
	return "Dorme após " + minutes(p.Sleep) + " de inatividade"
}

// minutes formata uma duração em minutos de forma legível.
func minutes(m int) string {
	switch {
	case m == 0:
		return "nunca"
	case m < 60:
		return itoa(m) + " min"
	case m%60 == 0:
		return itoa(m/60) + "h"
	default:
		return itoa(m/60) + "h " + itoa(m%60) + "min"
	}
}

// itoa evita importar strconv só para isto.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// Settings são os dois perfis, que sempre são lidos e aplicados juntos —
// o pmset os expõe em uma só leitura e aplicá-los em duas autenticações
// separadas pediria a senha duas vezes.
type Settings struct {
	Battery Profile
	AC      Profile
}

// Get devolve o perfil da fonte indicada.
func (s Settings) Get(src Source) Profile {
	if src == AC {
		return s.AC
	}
	return s.Battery
}

// With devolve uma cópia com o perfil da fonte substituído.
func (s Settings) With(src Source, p Profile) Settings {
	if src == AC {
		s.AC = p
		return s
	}
	s.Battery = p
	return s
}

// NeverSleep mantém a máquina acordada. É o único preset que o núcleo define:
// "nunca dormir" quer dizer a mesma coisa em qualquer sistema, enquanto
// "padrões de fábrica" é conhecimento de plataforma e vem do Manager.
var NeverSleep = Profile{
	Sleep: 0, DisplaySleep: 0, DiskSleep: 0,
	TCPKeepAlive: true, TTYsKeepAwake: true,
	HibernateMode: 3,
}

// PresetNeverSleep aplica "nunca dormir" nas duas fontes.
func PresetNeverSleep() Settings {
	return Settings{Battery: NeverSleep, AC: NeverSleep}
}

// Manager é a porta de saída: lê e escreve as configurações de energia.
//
// Escrever pode exigir privilégio de administrador, e é por isso que a porta
// expõe a dispensa de senha: a decisão de pedir autenticação a cada mudança
// ou uma vez só é do usuário, não da implementação. Onde não há elevação a
// pedir, o Manager simplesmente não anuncia FeaturePasswordless.
type Manager interface {
	Read(ctx context.Context) (Settings, error)
	Apply(ctx context.Context, s Settings) error

	// Features declara o que esta implementação sabe ler e escrever. É o
	// contrato que permite a mesma tela servir plataformas com painéis
	// diferentes, em vez de uma tela por sistema.
	Features() Feature

	// Defaults são os padrões de fábrica da plataforma, oferecidos como
	// preset. Cada sistema tem os seus, e só a implementação os conhece.
	Defaults() Settings

	// PasswordlessEnabled informa se aplicar hoje dispensa autenticação.
	// Sempre true onde FeaturePasswordless não é anunciada — não há senha a
	// pedir, então nada fica pendente.
	PasswordlessEnabled(ctx context.Context) bool
	EnablePasswordless(ctx context.Context) error
	DisablePasswordless(ctx context.Context) error
}
