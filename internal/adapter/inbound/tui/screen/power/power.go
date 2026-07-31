package power

import (
	"context"
	"errors"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	corepower "github.com/mateuslh/lealing/internal/core/power"
)

// ScreenID identifica esta tela.
const ScreenID tui.ScreenID = "tool/power-control"

// Model é o estado da tela.
//
// Guarda dois retratos das configurações: `current`, que o usuário edita, e
// `saved`, o que está de fato no sistema. A diferença entre os dois é o que
// habilita "aplicar" e "descartar" — e o que impede sair sem avisar.
type Model struct {
	deps    tui.Deps
	manager corepower.Manager
	// features é lido uma vez na construção: é uma propriedade da
	// implementação, não do estado da máquina, e reconsultá-la a cada render
	// só faria a tela piscar controles.
	features corepower.Feature

	width, height int

	current corepower.Settings
	saved   corepower.Settings

	source corepower.Source // coluna focada
	cursor [2]int           // linha focada em cada coluna

	passwordless bool
	busy         bool
	loading      bool
	status       status
}

// status é a mensagem exibida no rodapé da tela.
type status struct {
	text string
	tone tone
}

type tone uint8

const (
	toneNeutral tone = iota
	toneOK
	toneWarn
	toneErr
)

var _ tui.Screen = (*Model)(nil)

// New monta a tela.
func New(deps tui.Deps, manager corepower.Manager) *Model {
	return &Model{
		deps:     deps,
		manager:  manager,
		features: manager.Features(),
		loading:  true,
	}
}

// fields devolve os campos que a fonte aceita e o sistema desta máquina sabe
// gravar.
func (m *Model) fields(src corepower.Source) []corepower.Field {
	return corepower.VisibleFields(src, m.features)
}

// supportsPasswordless informa se há dispensa de senha a oferecer.
func (m *Model) supportsPasswordless() bool {
	return m.features.Has(corepower.FeaturePasswordless)
}

// ID implementa tui.Screen.
func (m *Model) ID() tui.ScreenID { return ScreenID }

// Title implementa tui.Screen.
func (m *Model) Title() string { return "controle de energia" }

// Init implementa tui.Screen.
func (m *Model) Init() tea.Cmd { return m.load() }

// HasUnsavedChanges informa se há edição pendente de aplicação.
func (m *Model) HasUnsavedChanges() bool { return m.current != m.saved }

// --- Mensagens ---------------------------------------------------------

type loadedMsg struct {
	settings     corepower.Settings
	passwordless bool
	err          error
}

type appliedMsg struct{ err error }

type passwordlessMsg struct {
	enabled bool
	err     error
}

// --- Comandos ----------------------------------------------------------

func (m *Model) load() tea.Cmd {
	manager := m.manager
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		settings, err := manager.Read(ctx)
		return loadedMsg{
			settings:     settings,
			passwordless: manager.PasswordlessEnabled(ctx),
			err:          err,
		}
	}
}

// apply grava as configurações. O timeout é longo porque pode haver um
// diálogo de senha esperando o usuário na frente.
func (m *Model) apply() tea.Cmd {
	manager := m.manager
	settings := m.current
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		return appliedMsg{err: manager.Apply(ctx, settings)}
	}
}

func (m *Model) togglePasswordless() tea.Cmd {
	manager := m.manager
	enabling := !m.passwordless
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		var err error
		if enabling {
			err = manager.EnablePasswordless(ctx)
		} else {
			err = manager.DisablePasswordless(ctx)
		}
		return passwordlessMsg{enabled: manager.PasswordlessEnabled(ctx), err: err}
	}
}

// --- Update ------------------------------------------------------------

// Update implementa tui.Screen.
func (m *Model) Update(msg tea.Msg) (tui.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case loadedMsg:
		m.loading = false
		m.passwordless = msg.passwordless
		if msg.err != nil {
			m.status = status{msg.err.Error(), toneErr}
			return m, nil
		}
		m.current, m.saved = msg.settings, msg.settings
		m.status = status{"configurações atuais carregadas", toneNeutral}
		return m, nil

	case appliedMsg:
		m.busy = false
		switch {
		case errors.Is(msg.err, corepower.ErrAuthCanceled):
			m.status = status{"autenticação cancelada — nada foi alterado", toneWarn}
		case msg.err != nil:
			m.status = status{msg.err.Error(), toneErr}
		default:
			m.saved = m.current
			m.status = status{"configurações aplicadas", toneOK}
		}
		return m, nil

	case passwordlessMsg:
		m.busy = false
		m.passwordless = msg.enabled
		switch {
		case errors.Is(msg.err, corepower.ErrAuthCanceled):
			m.status = status{"autenticação cancelada", toneWarn}
		case msg.err != nil:
			m.status = status{msg.err.Error(), toneErr}
		case msg.enabled:
			m.status = status{"senha dispensada — as próximas mudanças aplicam direto", toneOK}
		default:
			m.status = status{"dispensa de senha removida", toneNeutral}
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m *Model) handleKey(msg tea.KeyMsg) (tui.Screen, tea.Cmd) {
	// Enquanto uma operação privilegiada está em curso, só o essencial
	// responde: editar campos que serão sobrescritos confunde mais que ajuda.
	if m.busy {
		return m, nil
	}

	// As setas editam o campo focado e o shift troca de fonte. A alternativa —
	// setas navegando entre as colunas — obrigava a alcançar shift para a
	// única coisa que a tela existe para fazer, que é mudar valores.
	switch msg.String() {
	case "up", "k":
		m.moveCursor(-1)
		return m, nil

	case "down", "j":
		m.moveCursor(1)
		return m, nil

	case "left", "h", "-", "_":
		m.adjust(-1)
		return m, nil

	case "right", "l", "+", "=":
		m.adjust(1)
		return m, nil

	case "shift+left", "H":
		m.setSource(corepower.Battery)
		return m, nil

	case "shift+right", "L":
		m.setSource(corepower.AC)
		return m, nil

	case "tab", "shift+tab":
		m.setSource(1 - m.source)
		return m, nil

	case " ", "enter":
		m.toggleOrStep()
		return m, nil

	case "1":
		m.loadPreset(corepower.PresetNeverSleep(), "preset “nunca dormir” carregado")
		return m, nil

	case "2":
		m.loadPreset(m.manager.Defaults(), "padrões do sistema carregados")
		return m, nil

	case "a":
		if !m.HasUnsavedChanges() {
			m.status = status{"nada mudou desde a última leitura", toneNeutral}
			return m, nil
		}
		m.busy = true
		m.status = status{"aplicando… confirme no diálogo do sistema, se aparecer", toneNeutral}
		return m, m.apply()

	case "d":
		if !m.HasUnsavedChanges() {
			return m, nil
		}
		m.current = m.saved
		m.status = status{"alterações descartadas", toneNeutral}
		return m, nil

	case "r":
		m.loading = true
		return m, m.load()

	case "p":
		if !m.supportsPasswordless() {
			return m, nil
		}
		m.busy = true
		m.status = status{"aguardando autenticação…", toneNeutral}
		return m, m.togglePasswordless()
	}

	return m, nil
}

// loadPreset adota um preset apenas nos campos que esta plataforma grava.
//
// Sem a máscara, um preset que liga Power Nap marcaria a tela como alterada
// no Windows: o usuário veria "alterações não aplicadas" sem nenhum controle
// visível diferente, e aplicar não fecharia a pendência.
func (m *Model) loadPreset(preset corepower.Settings, label string) {
	m.current = corepower.Merge(m.current, preset, m.features)
	m.status = status{label + " — aperte a para aplicar", toneNeutral}
}

// profile devolve um ponteiro para o perfil da coluna focada.
func (m *Model) profile() *corepower.Profile {
	if m.source == corepower.AC {
		return &m.current.AC
	}
	return &m.current.Battery
}

// setSource troca a coluna focada, mantendo o cursor dentro dos campos que a
// nova fonte oferece.
func (m *Model) setSource(src corepower.Source) {
	m.source = src
	m.clampCursor()
}

func (m *Model) moveCursor(delta int) {
	n := len(m.fields(m.source))
	if n == 0 {
		return
	}
	m.cursor[m.source] = min(max(m.cursor[m.source]+delta, 0), n-1)
}

// clampCursor evita que a coluna com menos campos herde um índice inválido.
func (m *Model) clampCursor() {
	n := len(m.fields(m.source))
	m.cursor[m.source] = min(m.cursor[m.source], max(n-1, 0))
}

// focused devolve o campo sob o cursor da coluna em edição.
func (m *Model) focused() (corepower.Field, bool) {
	visible := m.fields(m.source)
	i := m.cursor[m.source]
	if i >= len(visible) {
		return corepower.Field{}, false
	}
	return visible[i], true
}

// adjust move o valor do campo focado.
func (m *Model) adjust(delta int) {
	if f, ok := m.focused(); ok {
		f.Step(m.profile(), delta)
	}
}

// toggleOrStep é o gesto de "ativar" — alterna booleanos e avança escalas.
func (m *Model) toggleOrStep() {
	if f, ok := m.focused(); ok {
		f.Toggle(m.profile())
	}
}

// Hints implementa tui.Screen.
func (m *Model) Hints() []tui.Hint {
	hints := []tui.Hint{
		{Key: "↑↓", Label: "campo"},
		{Key: "←→", Label: "ajustar"},
		{Key: "⇧←→", Label: "fonte"},
		{Key: "␣", Label: "alternar"},
	}
	if m.HasUnsavedChanges() {
		hints = append(hints,
			tui.Hint{Key: "a", Label: "aplicar"},
			tui.Hint{Key: "d", Label: "descartar"},
		)
	}
	hints = append(hints, tui.Hint{Key: "1/2", Label: "presets"})
	if m.supportsPasswordless() {
		hints = append(hints, tui.Hint{Key: "p", Label: "dispensar senha"})
	}
	return append(hints, tui.Hint{Key: "esc", Label: "voltar"})
}

// Status alimenta a barra de status.
func (m *Model) Status() (string, lipgloss.TerminalColor) {
	th := m.deps.Theme
	if m.status.text == "" {
		return "", nil
	}
	switch m.status.tone {
	case toneOK:
		return "✓ " + m.status.text, th.Success
	case toneWarn:
		return "▲ " + m.status.text, th.Warning
	case toneErr:
		return "✗ " + m.status.text, th.Danger
	default:
		return m.status.text, th.Muted
	}
}
