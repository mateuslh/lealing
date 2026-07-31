// Package ccaccount é a tela da tool "Contas do Claude Code".
package ccaccount

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	core "github.com/mateuslh/lealing/internal/core/ccaccount"
)

// ScreenID identifica esta tela.
const ScreenID tui.ScreenID = "tool/claude-accounts"

// mode é o que a tela está pedindo ao usuário no momento.
type mode uint8

const (
	modeList    mode = iota // navegando os perfis
	modeNaming              // digitando o nome de um perfil novo
	modeConfirm             // respondendo a uma pergunta de sim ou não
)

// intent é a ação que espera confirmação.
type intent uint8

const (
	intentNone intent = iota
	intentForget
	intentOverwriteProfile // salvar por cima de um perfil de outra conta
	intentActivateUnsaved  // ativar descartando uma sessão não guardada
)

// tone classifica a mensagem do rodapé.
type tone uint8

const (
	toneNeutral tone = iota
	toneOK
	toneWarn
	toneErr
)

// status é a mensagem exibida no rodapé da tela.
type status struct {
	text string
	tone tone
}

// Model é o estado da tela.
type Model struct {
	deps     tui.Deps
	switcher core.Switcher
	now      func() time.Time

	width, height int

	state   core.State
	cursor  int
	loading bool
	busy    bool
	status  status

	mode mode
	// input é o nome sendo digitado em modeNaming.
	input string
	// pending é a ação aguardando confirmação em modeConfirm, com o alvo.
	pending intent
	target  string
	// question é o texto da confirmação, montado quando ela é pedida.
	question string
}

var _ tui.Screen = (*Model)(nil)

// New monta a tela.
func New(deps tui.Deps, switcher core.Switcher, now func() time.Time) *Model {
	if now == nil {
		now = time.Now
	}
	return &Model{deps: deps, switcher: switcher, now: now, loading: true}
}

// ID implementa tui.Screen.
func (m *Model) ID() tui.ScreenID { return ScreenID }

// Title implementa tui.Screen.
func (m *Model) Title() string { return "contas do claude code" }

// Init implementa tui.Screen.
func (m *Model) Init() tea.Cmd { return m.load() }

// Capturing implementa o contrato opcional do App: enquanto o nome está
// sendo digitado, "q" é uma letra, não o comando de sair.
func (m *Model) Capturing() bool { return m.mode != modeList }

// --- Mensagens ---------------------------------------------------------

// stateMsg entrega o retrato lido das contas.
type stateMsg struct {
	state core.State
	err   error
}

// actionMsg entrega o resultado de salvar, ativar ou esquecer.
type actionMsg struct {
	// verb descreve o que foi tentado, para a mensagem de sucesso.
	verb string
	name string
	err  error
}

// --- Comandos ----------------------------------------------------------

// timeout cobre o pior caso: no macOS a escrita no chaveiro pode esperar o
// usuário liberar o acesso em um diálogo do sistema.
const timeout = 30 * time.Second

func (m *Model) load() tea.Cmd {
	switcher := m.switcher
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		st, err := switcher.State(ctx)
		return stateMsg{state: st, err: err}
	}
}

func (m *Model) save(name string, force bool) tea.Cmd {
	switcher := m.switcher
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		var err error
		if force {
			_, err = switcher.SaveOverwriting(ctx, name)
		} else {
			_, err = switcher.Save(ctx, name)
		}
		return actionMsg{verb: "salvo", name: name, err: err}
	}
}

func (m *Model) activate(name string, force bool) tea.Cmd {
	switcher := m.switcher
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()

		var err error
		if force {
			err = switcher.ActivateOverwriting(ctx, name)
		} else {
			err = switcher.Activate(ctx, name)
		}
		return actionMsg{verb: "ativado", name: name, err: err}
	}
}

func (m *Model) forget(name string) tea.Cmd {
	switcher := m.switcher
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		return actionMsg{verb: "removido", name: name, err: switcher.Forget(ctx, name)}
	}
}

// --- Consultas ---------------------------------------------------------

// selected devolve o perfil sob o cursor.
func (m *Model) selected() (core.Profile, bool) {
	if m.cursor < 0 || m.cursor >= len(m.state.Profiles) {
		return core.Profile{}, false
	}
	return m.state.Profiles[m.cursor], true
}

// suggestName propõe um nome para a sessão ativa: o apelido da conta, ou a
// parte local do e-mail. Digitar de novo o que a tela já sabe é trabalho à
// toa.
func (m *Model) suggestName() string {
	if m.state.ActiveProfile != "" {
		return m.state.ActiveProfile
	}
	id := m.state.Active
	if id.Email != "" {
		for i, r := range id.Email {
			if r == '@' {
				return sanitize(id.Email[:i])
			}
		}
		return sanitize(id.Email)
	}
	return sanitize(id.DisplayName)
}

// sanitize descarta o que NormalizeName recusaria, para a sugestão nunca
// nascer inválida.
func sanitize(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		if core.AllowedInName(r) {
			out = append(out, r)
		}
	}
	if len(out) > core.NameLimit {
		out = out[:core.NameLimit]
	}
	return string(out)
}
