// Package selfupdate é o domínio da tool "Atualizar o lealing".
//
// O pacote não sabe baixar nada, não fala com o GitHub e não roda git: ele
// decide *se* há atualização e *por qual caminho* ela se aplica. Quem executa
// é o adapter — é isso que permite testar a decisão inteira sem rede.
package selfupdate

import (
	"context"
	"errors"
	"time"
)

// Mode é a forma como este binário chegou na máquina, e é ela que determina
// como ele se atualiza.
type Mode uint8

const (
	// ModeUnknown é o que sobra quando nem um clone nem um binário de
	// release foram reconhecidos (um binário copiado à mão, por exemplo).
	// A tela então mostra as instruções manuais em vez de um botão.
	ModeUnknown Mode = iota
	// ModeRelease é um binário baixado de uma release do GitHub: atualizar
	// é trocar o arquivo por outro.
	ModeRelease
	// ModeSource é um binário compilado de um clone do repositório:
	// atualizar é `git pull` seguido de `go build`.
	ModeSource
)

// Label devolve o rótulo de exibição do modo.
func (m Mode) Label() string {
	switch m {
	case ModeRelease:
		return "binário de release"
	case ModeSource:
		return "compilado do fonte"
	default:
		return "origem desconhecida"
	}
}

// String implementa fmt.Stringer com o nome curto usado em log.
func (m Mode) String() string {
	switch m {
	case ModeRelease:
		return "release"
	case ModeSource:
		return "source"
	default:
		return "unknown"
	}
}

// Install descreve a instalação em execução.
type Install struct {
	Mode Mode
	// BinaryPath é o executável em disco, com os symlinks já resolvidos.
	BinaryPath string
	// RepoDir é a raiz do clone; preenchido apenas em ModeSource.
	RepoDir string
	// Branch é a branch em que o clone está, informativa: atualizar uma
	// branch de trabalho puxa o que estiver nela, não a última release.
	Branch string
	// Writable diz se dá para escrever no lugar do binário. Falso aponta
	// uma instalação em diretório de sistema, que exigiria privilégio — e é
	// melhor dizer isso antes de baixar 8 MB do que depois.
	Writable bool
}

// Release é o último lançamento publicado.
type Release struct {
	Tag         string
	Notes       string
	PublishedAt time.Time
	URL         string
}

// State classifica a comparação entre o que roda e o que foi publicado.
type State uint8

const (
	// StateUnknown é o veredito quando a versão em execução não é
	// interpretável ("dev"): não dá para afirmar que está velha nem nova.
	StateUnknown State = iota
	// StateUpToDate é a versão em execução igual à última publicada.
	StateUpToDate
	// StateOutdated é a versão em execução atrás da última publicada.
	StateOutdated
	// StateAhead é um build local à frente da última tag publicada — o caso
	// normal na máquina de quem desenvolve o lealing.
	StateAhead
)

// Label devolve o rótulo de exibição do estado.
func (s State) Label() string {
	switch s {
	case StateUpToDate:
		return "em dia"
	case StateOutdated:
		return "atualização disponível"
	case StateAhead:
		return "à frente do último release"
	default:
		return "versão local não comparável"
	}
}

// Status é o resultado de uma verificação.
type Status struct {
	Install Install
	Current Version
	Latest  Release
	State   State
}

// CanApply informa se a ação de atualizar faz sentido agora.
//
// Um clone sempre pode: a última tag é uma referência, não o teto do que
// existe na branch, e `git pull` num clone já em dia simplesmente não faz
// nada. Um binário de release só se move quando há release mais novo — ou
// quando não deu para comparar, caso em que reinstalar o último é a saída.
func (s Status) CanApply() bool {
	switch s.Install.Mode {
	case ModeSource:
		return true
	case ModeRelease:
		return s.Latest.Tag != "" && (s.State == StateOutdated || s.State == StateUnknown)
	default:
		return false
	}
}

// Outcome é o que uma atualização produziu.
type Outcome struct {
	From string
	To   string
	// Detail é a mensagem curta que a tela mostra ao final.
	Detail string
	// Restart marca que o binário em disco mudou e o processo em execução
	// ainda é o antigo. É sempre verdadeiro na prática, mas explícito
	// porque é a única coisa que o usuário precisa fazer depois.
	Restart bool
}

// Erros do domínio.
var (
	// ErrNotApplicable é devolvido quando se pede para aplicar uma
	// atualização que não se aplica — instalação desconhecida ou já em dia.
	ErrNotApplicable = errors.New("atualização não se aplica a esta instalação")
	// ErrNoAsset é devolvido quando a release não tem binário para esta
	// combinação de sistema e arquitetura.
	ErrNoAsset = errors.New("release sem binário para esta plataforma")
	// ErrChecksum é devolvido quando o arquivo baixado não bate com o
	// checksum publicado. Nunca se instala um binário nesse estado.
	ErrChecksum = errors.New("checksum do arquivo baixado não confere")
)

// Locator é a porta que descobre como este binário foi instalado.
type Locator interface {
	Locate(ctx context.Context) (Install, error)
}

// Releases é a porta que consulta os lançamentos publicados.
type Releases interface {
	Latest(ctx context.Context) (Release, error)
}

// Applier é a porta que efetivamente troca o binário.
type Applier interface {
	Apply(ctx context.Context, in Install, rel Release) (Outcome, error)
}

// Manager é a porta de entrada consumida pela tela.
type Manager interface {
	Check(ctx context.Context) (Status, error)
	Apply(ctx context.Context, st Status) (Outcome, error)
}

// Service orquestra as três portas.
type Service struct {
	current  string
	locator  Locator
	releases Releases
	applier  Applier
}

var _ Manager = (*Service)(nil)

// NewService monta o serviço com a versão em execução e as portas.
func NewService(current string, locator Locator, releases Releases, applier Applier) *Service {
	return &Service{current: current, locator: locator, releases: releases, applier: applier}
}

// Check descobre a instalação, consulta o último release e compara os dois.
//
// Falhar em localizar a instalação não interrompe a verificação: saber que
// existe uma versão nova já é informação útil, mesmo sem saber como aplicá-la.
// Falhar em consultar o release, sim — sem ele não há o que comparar.
func (s *Service) Check(ctx context.Context) (Status, error) {
	st := Status{Current: ParseVersion(s.current)}

	if s.locator != nil {
		in, err := s.locator.Locate(ctx)
		if err == nil {
			st.Install = in
		}
	}

	if s.releases == nil {
		return st, errors.New("nenhuma fonte de releases configurada")
	}
	rel, err := s.releases.Latest(ctx)
	if err != nil {
		return st, err
	}
	st.Latest = rel
	st.State = compare(st.Current, ParseVersion(rel.Tag))
	return st, nil
}

// Apply executa a atualização decidida por Check.
func (s *Service) Apply(ctx context.Context, st Status) (Outcome, error) {
	if !st.CanApply() {
		return Outcome{}, ErrNotApplicable
	}
	if s.applier == nil {
		return Outcome{}, ErrNotApplicable
	}
	out, err := s.applier.Apply(ctx, st.Install, st.Latest)
	if err != nil {
		return Outcome{}, err
	}
	if out.From == "" {
		out.From = st.Current.String()
	}
	return out, nil
}

// compare classifica a versão em execução contra a publicada.
func compare(current, latest Version) State {
	if !current.Known || !latest.Known {
		return StateUnknown
	}
	switch current.Compare(latest) {
	case 0:
		return StateUpToDate
	case 1:
		return StateAhead
	default:
		return StateOutdated
	}
}
