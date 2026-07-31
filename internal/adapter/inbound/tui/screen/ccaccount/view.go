package ccaccount

import (
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/component"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	core "github.com/mateuslh/lealing/internal/core/ccaccount"
)

// fullLayoutMin é a altura a partir da qual o painel da conta ativa cabe
// inteiro. Abaixo dela ele vira uma linha só, porque a lista de perfis é o
// que a tool existe para mostrar.
const fullLayoutMin = 13

// View implementa tui.Screen.
func (m *Model) View(f tui.Frame) string {
	th := m.deps.Theme

	if m.loading && !m.state.HasActive && len(m.state.Profiles) == 0 {
		return component.Center(f.Width, f.Height, th.Dim.Render("lendo as sessões do Claude Code…"))
	}

	inner := max(f.Width-4, 20)
	footer := m.viewFooter(th, inner)
	// O corpo é o frame menos o padding vertical, o rodapé e a linha em
	// branco que o separa dos painéis.
	body := max(f.Height-2-lipgloss.Height(footer)-1, 3)

	// A lista encolhe até o conteúdo: esticada até a base, duas contas
	// deixariam um retângulo vazio maior que a própria lista.
	fit := max(len(m.state.Profiles), 1) + 2

	var blocks []string
	if body >= fullLayoutMin {
		active := m.viewActive(th, inner)
		blocks = []string{active, "", m.viewProfiles(th, inner, min(body-lipgloss.Height(active)-1, fit))}
	} else {
		summary := m.viewActiveLine(th, inner)
		blocks = []string{summary, m.viewProfiles(th, inner, min(body-1, fit))}
	}
	blocks = append(blocks, "", footer)

	// Os limites vêm no mesmo estilo do padding: recortar só o miolo
	// deixaria as linhas de respiro estourando o frame.
	return lipgloss.NewStyle().
		Padding(1, 2).
		MaxWidth(f.Width).
		MaxHeight(f.Height).
		Render(lipgloss.JoinVertical(lipgloss.Left, blocks...))
}

// --- Conta ativa -------------------------------------------------------

// viewActive é o painel que descreve em qual conta a CLI está agora.
func (m *Model) viewActive(th *theme.Theme, width int) string {
	rows := m.activeRows(th)
	content := component.FieldList{Rows: rows, Width: width - 4}.Render(th)
	return component.Panel{
		Title:  "Conta ativa",
		Glyph:  "✦",
		Accent: th.Primary,
		Width:  width,
		Height: len(rows) + 2,
	}.Render(th, content)
}

func (m *Model) activeRows(th *theme.Theme) []component.Row {
	if !m.state.HasActive {
		return []component.Row{
			{Label: "Sessão", Value: "nenhuma", Tone: th.Warning},
			{Label: "Entrar", Value: "rode `claude` no terminal e faça login"},
			{Label: "Credencial", Value: m.state.Origin},
		}
	}

	id := m.state.Active
	sessionText, sessionTone := m.sessionState(th, id)

	rows := []component.Row{
		{Label: "Conta", Value: id.Label()},
	}
	if id.Organization != "" {
		rows = append(rows, component.Row{Label: "Organização", Value: id.Organization})
	}
	if id.Plan != "" {
		rows = append(rows, component.Row{Label: "Plano", Value: id.Plan})
	}
	rows = append(rows,
		component.Row{Label: "Sessão", Value: sessionText, Tone: sessionTone},
		component.Row{Label: "Perfil", Value: m.activeProfileText(), Tone: m.activeProfileTone(th)},
		component.Row{Label: "Credencial", Value: m.state.Origin},
	)
	return rows
}

// sessionState traduz as duas validades da credencial em uma frase.
//
// A distinção importa: um access token vencido não exige nada do usuário —
// a CLI o renova sozinha —, enquanto um refresh token vencido só volta com
// um login novo.
func (m *Model) sessionState(th *theme.Theme, id core.Identity) (string, lipgloss.TerminalColor) {
	now := m.now()
	switch {
	case id.Dead(now):
		return "expirada — rode `claude` para entrar de novo", th.Danger
	case id.Stale(now):
		return "renova sozinha no próximo uso do claude", th.Warning
	case !id.ExpiresAt.IsZero():
		return "válida até " + id.ExpiresAt.Local().Format("02/01 15:04"), th.Success
	default:
		return "ativa", th.Success
	}
}

func (m *Model) activeProfileText() string {
	if m.state.ActiveProfile != "" {
		return "“" + m.state.ActiveProfile + "”"
	}
	return "não guardada — “s” salva esta sessão"
}

func (m *Model) activeProfileTone(th *theme.Theme) lipgloss.TerminalColor {
	if m.state.ActiveProfile == "" {
		return th.Warning
	}
	return nil
}

// viewActiveLine é a versão de uma linha, para janelas baixas.
func (m *Model) viewActiveLine(th *theme.Theme, width int) string {
	if !m.state.HasActive {
		return component.TruncateTail(th.Dim.Render("ativa: nenhuma — rode `claude`"), width)
	}
	id := m.state.Active
	left := th.Strong.Render("✦ " + id.Label())
	right := th.Dim.Render(strings.TrimSpace(id.Plan + " " + m.activeProfileText()))
	return component.Spread(left, right, width)
}

// --- Perfis ------------------------------------------------------------

// viewProfiles é o painel da lista de perfis guardados.
func (m *Model) viewProfiles(th *theme.Theme, width, height int) string {
	height = max(height, 3)
	rows := max(height-2, 1)

	var content string
	if len(m.state.Profiles) == 0 {
		// Os dois espaços alinham o texto com a coluna do cursor das linhas
		// de perfil, para a moldura não parecer torta quando a lista enche.
		content = th.Ghost.Render(component.TruncateTail(
			"  nenhum perfil guardado · “s” salva a sessão atual", width-4))
	} else {
		content = m.profileLines(th, width-4, rows)
	}

	footer := ""
	if n := len(m.state.Profiles); n > rows {
		footer = strconv.Itoa(m.cursor+1) + "/" + strconv.Itoa(n)
	}

	return component.Panel{
		Title:  "Perfis",
		Glyph:  "⇆",
		Accent: th.Accent,
		Width:  width,
		Height: height,
		Footer: footer,
	}.Render(th, content)
}

// profileLines desenha a janela visível da lista, seguindo o cursor.
func (m *Model) profileLines(th *theme.Theme, width, visible int) string {
	start := 0
	if len(m.state.Profiles) > visible {
		start = min(max(m.cursor-visible/2, 0), len(m.state.Profiles)-visible)
	}
	end := min(start+visible, len(m.state.Profiles))

	lines := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		lines = append(lines, m.profileLine(th, m.state.Profiles[i], i == m.cursor, width))
	}
	return strings.Join(lines, "\n")
}

// profileLine é "▎ ● nome   e-mail   plano · salvo em".
func (m *Model) profileLine(th *theme.Theme, p core.Profile, selected bool, width int) string {
	active := p.Name == m.state.ActiveProfile

	cursor := "  "
	nameStyle := th.Body
	if selected {
		cursor = th.Cursor.Render("▎") + " "
		nameStyle = th.Strong
	}

	mark := th.Ghost.Render("○")
	if active {
		mark = lipgloss.NewStyle().Foreground(th.Success).Render("●")
	}

	// O nome tem coluna própria para que os e-mails se alinhem: uma lista de
	// duas contas é lida na vertical, comparando os campos.
	nameW := min(m.widestName(), max(width/3, 8))
	left := cursor + mark + " " + nameStyle.Render(component.PadRight(
		component.TruncateTail(p.Name, nameW), nameW))

	detail := p.Identity.Email
	if detail == "" {
		detail = p.Identity.DisplayName
	}
	right := m.profileMeta(p)

	// O detalhe ocupa o que sobrar entre o nome e a coluna da direita.
	space := max(width-lipgloss.Width(left)-lipgloss.Width(right)-1, 0)
	middle := th.Dim.Render(component.PadRight(component.TruncateTail(detail, space), space))

	return component.TruncateTail(left+middle+" "+th.Ghost.Render(right), width)
}

// profileMeta é a coluna da direita: plano e quando o perfil foi salvo.
func (m *Model) profileMeta(p core.Profile) string {
	parts := make([]string, 0, 2)
	if p.Identity.Plan != "" {
		parts = append(parts, p.Identity.Plan)
	}
	if !p.SavedAt.IsZero() {
		parts = append(parts, p.SavedAt.Local().Format("02/01"))
	}
	if p.Identity.Dead(m.now()) {
		parts = append(parts, "expirado")
	}
	return strings.Join(parts, " · ")
}

// widestName é a largura do maior nome, para alinhar a coluna.
func (m *Model) widestName() int {
	w := 0
	for _, p := range m.state.Profiles {
		w = max(w, lipgloss.Width(p.Name))
	}
	return w
}

// --- Rodapé ------------------------------------------------------------

// viewFooter é a linha de diálogo: o campo de nome, a pergunta pendente ou
// a última mensagem.
func (m *Model) viewFooter(th *theme.Theme, width int) string {
	switch m.mode {
	case modeNaming:
		label := th.Dim.Render("nome do perfil ")
		field := th.Strong.Render(m.input) + th.Cursor.Render("▌")
		return component.TruncateTail(label+field, width)

	case modeConfirm:
		return component.TruncateTail(
			lipgloss.NewStyle().Foreground(th.Warning).Render("▸ "+m.question)+
				th.Ghost.Render("  s / n"), width)
	}

	if m.busy {
		return th.Dim.Render("aplicando…")
	}
	if m.status.text == "" {
		return th.Ghost.Render(component.TruncateTail(
			"feche as sessões do `claude` antes de trocar: ao sair, a CLI regrava a conta em que estava", width))
	}

	style := th.Dim
	switch m.status.tone {
	case toneOK:
		style = lipgloss.NewStyle().Foreground(th.Success)
	case toneWarn:
		style = lipgloss.NewStyle().Foreground(th.Warning)
	case toneErr:
		style = lipgloss.NewStyle().Foreground(th.Danger)
	}
	return style.Render(component.TruncateTail(m.status.text, width))
}

// Status alimenta a barra de status com a conta em uso.
func (m *Model) Status() (string, lipgloss.TerminalColor) {
	th := m.deps.Theme
	if m.loading {
		return "lendo…", th.Accent
	}
	if !m.state.HasActive {
		return "sem sessão", th.Warning
	}
	if m.state.Active.Dead(m.now()) {
		return "sessão expirada", th.Danger
	}
	return m.state.Active.Label(), th.Faint
}
