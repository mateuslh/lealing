// Package devkit é a tela compartilhada pelas oito ferramentas de engenharia.
//
// A identidade e o modo mudam por definição, mas o fluxo é deliberadamente
// igual: digitar, executar fora da thread da TUI e inspecionar o resultado.
package devkit

import (
	"context"
	"strings"
	"time"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wordwrap"
	"github.com/muesli/reflow/wrap"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/component"
	core "github.com/mateuslh/lealing/internal/core/devkit"
)

const runTimeout = 15 * time.Second

// Model é o estado de uma ferramenta da bancada.
type Model struct {
	deps       tui.Deps
	runner     core.Runner
	definition core.Definition
	input      textinput.Model

	width, height int
	mode          int
	scroll        int
	loading       bool
	result        core.Result
	err           error
	finishedAt    time.Time
}

var (
	_ tui.Screen                    = (*Model)(nil)
	_ interface{ Capturing() bool } = (*Model)(nil)
)

// New monta uma das oito telas a partir de sua definição.
func New(deps tui.Deps, runner core.Runner, definition core.Definition) *Model {
	input := textinput.New()
	input.Prompt = ""
	input.Placeholder = definition.Placeholder
	input.CharLimit = 256 << 10
	input.Focus()
	return &Model{
		deps:       deps,
		runner:     runner,
		definition: definition,
		input:      input,
	}
}

// ID implementa tui.Screen.
func (m *Model) ID() tui.ScreenID { return tui.ScreenID("tool/" + m.definition.ToolID) }

// Title implementa tui.Screen.
func (m *Model) Title() string { return m.definition.Title }

// Init implementa tui.Screen.
func (m *Model) Init() tea.Cmd { return textinput.Blink }

// Capturing mantém letras como "q" dentro do campo. Esc é tratado pela
// própria tela para continuar sendo uma saída visível.
func (m *Model) Capturing() bool { return true }

type completedMsg struct {
	result core.Result
	err    error
}

func (m *Model) execute() tea.Cmd {
	runner := m.runner
	request := core.Request{
		Tool:  m.definition.Tool,
		Mode:  m.currentMode().ID,
		Input: m.input.Value(),
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), runTimeout)
		defer cancel()
		result, err := runner.Run(ctx, request)
		return completedMsg{result: result, err: err}
	}
}

// Update implementa tui.Screen.
func (m *Model) Update(msg tea.Msg) (tui.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.Width = max(msg.Width-8, 8)
		return m, nil

	case completedMsg:
		m.loading = false
		m.result = msg.result
		m.err = msg.err
		m.scroll = 0
		m.finishedAt = time.Now()
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return tui.Back() }
		case "enter":
			if m.loading {
				return m, nil
			}
			m.loading = true
			m.err = nil
			m.result = core.Result{}
			m.scroll = 0
			return m, m.execute()
		case "tab":
			if !m.loading && len(m.definition.Modes) > 1 {
				m.mode = (m.mode + 1) % len(m.definition.Modes)
				m.result = core.Result{}
				m.err = nil
				m.scroll = 0
			}
			return m, nil
		case "shift+tab":
			if !m.loading && len(m.definition.Modes) > 1 {
				m.mode--
				if m.mode < 0 {
					m.mode = len(m.definition.Modes) - 1
				}
				m.result = core.Result{}
				m.err = nil
				m.scroll = 0
			}
			return m, nil
		case "ctrl+l":
			if !m.loading {
				m.input.SetValue("")
				m.result = core.Result{}
				m.err = nil
				m.scroll = 0
			}
			return m, nil
		case "up", "ctrl+p":
			m.scroll = max(m.scroll-1, 0)
			return m, nil
		case "down", "ctrl+n":
			m.scroll++
			return m, nil
		case "pgup":
			m.scroll = max(m.scroll-max(m.height/2, 1), 0)
			return m, nil
		case "pgdown":
			m.scroll += max(m.height/2, 1)
			return m, nil
		}
		if m.loading {
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *Model) currentMode() core.Mode {
	if len(m.definition.Modes) == 0 {
		return core.Mode{}
	}
	m.mode = min(max(m.mode, 0), len(m.definition.Modes)-1)
	return m.definition.Modes[m.mode]
}

// View implementa tui.Screen.
func (m *Model) View(frame tui.Frame) string {
	if frame.Width < 12 || frame.Height < 4 {
		return component.Center(frame.Width, frame.Height, m.deps.Theme.Dim.Render("janela pequena"))
	}

	innerWidth := max(frame.Width-4, 8)
	innerHeight := max(frame.Height-2, 2)
	inputHeight := 4
	gap := 1
	if innerHeight < 8 {
		inputHeight = 3
		gap = 0
	}
	resultHeight := max(innerHeight-inputHeight-gap, 0)

	inputPanel := m.inputPanel(innerWidth, inputHeight)
	if resultHeight < 3 {
		return lipgloss.NewStyle().
			Padding(1, 2).
			MaxWidth(frame.Width).
			MaxHeight(frame.Height).
			Render(inputPanel)
	}

	resultPanel := m.resultPanel(innerWidth, resultHeight)
	body := lipgloss.JoinVertical(lipgloss.Left, inputPanel, resultPanel)
	if gap > 0 {
		body = lipgloss.JoinVertical(lipgloss.Left, inputPanel, "", resultPanel)
	}
	// MaxWidth e MaxHeight ficam no mesmo estilo do padding: assim as linhas
	// de respiro também pertencem ao retângulo recortado.
	return lipgloss.NewStyle().
		Padding(1, 2).
		MaxWidth(frame.Width).
		MaxHeight(frame.Height).
		Render(body)
}

func (m *Model) inputPanel(width, height int) string {
	th := m.deps.Theme
	contentWidth := max(width-2, 1)
	m.input.Width = max(contentWidth-2, 4)

	mode := th.Ghost.Render("modo ") + th.Pill.Render(m.currentMode().Label)
	content := mode + "\n" + m.input.View()
	if height == 3 {
		content = m.input.View()
	}

	footer := "↵ " + m.definition.Action
	if len(m.definition.Modes) > 1 {
		footer = "tab modo · " + footer
	}
	return component.Panel{
		Title:   m.definition.InputLabel,
		Glyph:   m.definition.Glyph,
		Accent:  th.Primary,
		Focused: true,
		Footer:  footer,
		Width:   width,
		Height:  height,
	}.Render(th, content)
}

func (m *Model) resultPanel(width, height int) string {
	th := m.deps.Theme
	contentWidth := max(width-2, 1)
	contentHeight := max(height-2, 0)

	var content string
	switch {
	case m.loading:
		content = component.Center(contentWidth, contentHeight, th.Dim.Render("executando…"))
	case m.err != nil:
		message := fitText("✗ "+displayText(m.err.Error()), max(contentWidth-2, 8))
		content = component.Center(contentWidth, contentHeight,
			lipgloss.NewStyle().Foreground(th.Danger).Render(message))
	case m.result.Title != "":
		content = m.resultContent(contentWidth, contentHeight)
	default:
		intro := fitText(displayText(m.definition.Summary), max(contentWidth-4, 8))
		content = component.Center(contentWidth, contentHeight,
			th.Dim.Render(intro),
			th.Ghost.Render("digite a entrada e pressione enter"))
	}

	return component.Panel{
		Title:  "resultado",
		Glyph:  "›",
		Accent: th.Accent,
		Footer: m.resultFooter(contentHeight),
		Width:  width,
		Height: height,
	}.Render(th, content)
}

func (m *Model) resultContent(width, height int) string {
	th := m.deps.Theme
	var sections []string

	heading := th.Strong.Render(component.TruncateTail(displayText(m.result.Title), width))
	if m.result.Summary != "" {
		heading += "\n" + th.Dim.Render(fitText(displayText(m.result.Summary), width))
	}
	sections = append(sections, heading)

	if len(m.result.Rows) > 0 {
		rows := make([]component.Row, len(m.result.Rows))
		for i, row := range m.result.Rows {
			rows[i] = component.Row{Label: displayText(row.Label), Value: displayText(row.Value)}
		}
		sections = append(sections, component.FieldList{Rows: rows, Width: width}.Render(th))
	}
	if m.result.Warning != "" {
		warning := fitText("! "+displayText(m.result.Warning), max(width, 8))
		sections = append(sections, lipgloss.NewStyle().Foreground(th.Warning).Render(warning))
	}
	if m.result.Body != "" {
		sections = append(sections, th.Body.Render(fitText(displayText(m.result.Body), max(width, 8))))
	}

	lines := strings.Split(strings.Join(sections, "\n\n"), "\n")
	maxScroll := max(len(lines)-height, 0)
	start := min(max(m.scroll, 0), maxScroll)
	end := min(start+height, len(lines))
	if start >= end {
		return ""
	}
	return strings.Join(lines[start:end], "\n")
}

func (m *Model) resultFooter(height int) string {
	if m.result.Title == "" || height <= 0 {
		return ""
	}
	total := len(strings.Split(m.resultContentLines(max(m.width-6, 8)), "\n"))
	if total <= height {
		return ""
	}
	start := min(max(m.scroll, 0), max(total-height, 0))
	return "linhas " + itoa(start+1) + "–" + itoa(min(start+height, total)) + "/" + itoa(total)
}

// resultContentLines mede o conteúdo sem viewport. Ele replica apenas a
// composição textual; estilos não mudam a quantidade de linhas.
func (m *Model) resultContentLines(width int) string {
	var sections []string
	heading := component.TruncateTail(displayText(m.result.Title), width)
	if m.result.Summary != "" {
		heading += "\n" + fitText(displayText(m.result.Summary), width)
	}
	sections = append(sections, heading)
	for _, row := range m.result.Rows {
		label, value := displayText(row.Label), displayText(row.Value)
		sections = append(sections, label+"  "+component.TruncateTail(value, max(width-lipgloss.Width(label)-2, 4)))
	}
	if m.result.Warning != "" {
		sections = append(sections, fitText("! "+displayText(m.result.Warning), width))
	}
	if m.result.Body != "" {
		sections = append(sections, fitText(displayText(m.result.Body), width))
	}
	return strings.Join(sections, "\n\n")
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}

// fitText prefere quebras entre palavras e força uma segunda passagem só
// para tokens sem separadores, como hashes e JWTs.
func fitText(value string, width int) string {
	width = max(width, 1)
	return wrap.String(wordwrap.String(value, width), width)
}

// displayText é a última fronteira antes do terminal: resultados de rede e
// erros não podem carregar controles capazes de mover o cursor.
func displayText(value string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' {
			return r
		}
		if r == '\t' {
			return ' '
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value)
}

// Hints implementa tui.Screen.
func (m *Model) Hints() []tui.Hint {
	hints := []tui.Hint{{Key: "↵", Label: m.definition.Action}}
	if len(m.definition.Modes) > 1 {
		hints = append(hints, tui.Hint{Key: "tab", Label: "trocar modo"})
	}
	return append(hints,
		tui.Hint{Key: "↑↓", Label: "rolar saída"},
		tui.Hint{Key: "ctrl+l", Label: "limpar"},
		tui.Hint{Key: "esc", Label: "voltar"},
	)
}

// Meta mostra o modo no chrome sem repetir a entrada potencialmente sensível.
func (m *Model) Meta() []string { return []string{m.currentMode().Label} }

// Status alimenta o canto direito da barra.
func (m *Model) Status() (string, lipgloss.TerminalColor) {
	th := m.deps.Theme
	switch {
	case m.loading:
		return "executando…", th.Accent
	case m.err != nil:
		return "entrada recusada", th.Danger
	case !m.finishedAt.IsZero():
		return "concluído às " + m.finishedAt.Format("15:04:05"), th.Success
	default:
		return "processamento sob demanda", th.Faint
	}
}
