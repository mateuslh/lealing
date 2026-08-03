package update

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wordwrap"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/component"
	"github.com/mateuslh/lealing/internal/core/selfupdate"
)

func (m *Model) View(f tui.Frame) string {
	th := m.deps.Theme

	if m.manager == nil {
		return component.Center(f.Width, f.Height,
			lipgloss.NewStyle().Foreground(th.Danger).Render("✗ "+errNotConfigured.Error()))
	}
	if m.phase == phaseChecking {
		return component.Center(f.Width, f.Height, th.Dim.Render("consultando o último release…"))
	}
	if m.phase == phaseApplying {
		return component.Center(f.Width, f.Height, th.Dim.Render(m.applyingLabel()))
	}

	inner := max(f.Width-4, 20)
	blocks := []string{m.installPanel(inner), "", m.releasePanel(inner)}

	if note := m.footer(inner); note != "" {
		blocks = append(blocks, "", note)
	}

	// A altura já gasta pelos painéis decide se sobra espaço para as notas;
	// medir o bloco pronto é mais barato que prever a altura de cada peça.
	body := lipgloss.JoinVertical(lipgloss.Left, blocks...)
	if rest := f.Height - 2 - lipgloss.Height(body) - 1; rest >= 4 {
		if notes := m.notesPanel(inner, rest); notes != "" {
			body = lipgloss.JoinVertical(lipgloss.Left, body, "", notes)
		}
	}

	// Os limites vêm no mesmo estilo do padding: recortar só o miolo deixaria
	// as linhas de respiro estourando o frame.
	return lipgloss.NewStyle().
		Padding(1, 2).
		MaxWidth(f.Width).
		MaxHeight(f.Height).
		Render(body)
}

// applyingLabel diz o que está acontecendo, que é diferente em cada modo.
func (m *Model) applyingLabel() string {
	if m.status.Install.Mode == selfupdate.ModeSource {
		return "atualizando o clone e recompilando…"
	}
	return "baixando " + m.status.Latest.Tag + " e conferindo o checksum…"
}

// installPanel descreve a instalação em execução.
func (m *Model) installPanel(width int) string {
	th := m.deps.Theme
	in := m.status.Install

	rows := []component.Row{
		{Label: "Versão", Value: m.status.Current.String()},
		{Label: "Origem", Value: in.Mode.Label()},
	}
	switch in.Mode {
	case selfupdate.ModeSource:
		rows = append(rows,
			component.Row{Label: "Clone", Value: shortPath(in.RepoDir, m.home)},
			component.Row{Label: "Branch", Value: orDash(in.Branch)},
		)
	case selfupdate.ModeRelease:
		row := component.Row{Label: "Binário", Value: shortPath(in.BinaryPath, m.home)}
		if !in.Writable {
			row.Hint = "somente leitura"
			row.Tone = th.Warning
		}
		rows = append(rows, row)
	default:
		rows = append(rows, component.Row{
			Label: "Binário",
			Value: shortPath(in.BinaryPath, m.home),
			Hint:  "reinstale pelo install.sh para atualizar daqui",
		})
	}

	return m.panel("Instalação", "⌬", th.Primary, width, rows)
}

// releasePanel descreve o último lançamento publicado.
func (m *Model) releasePanel(width int) string {
	th := m.deps.Theme

	if m.err != nil && m.status.Latest.Tag == "" {
		content := lipgloss.NewStyle().
			Foreground(th.Danger).
			Render(wordwrap.String("✗ "+m.err.Error(), max(width-4, 10)))
		return component.Panel{
			Title: "Último release", Glyph: "⇪", Accent: th.Danger,
			Width: width, Height: lipgloss.Height(content) + 2,
		}.Render(th, content)
	}

	rel := m.status.Latest
	rows := []component.Row{
		{Label: "Versão", Value: orDash(rel.Tag)},
		{Label: "Estado", Value: m.status.State.Label(), Tone: m.stateTone()},
	}
	if !rel.PublishedAt.IsZero() {
		rows = append(rows, component.Row{
			Label: "Publicado",
			Value: rel.PublishedAt.Local().Format("02/01/2006"),
			Hint:  humanAge(m.now().Sub(rel.PublishedAt)),
		})
	}

	return m.panel("Último release", "⇪", m.stateTone(), width, rows)
}

// notesPanel mostra as novidades do release, cortadas na altura disponível.
func (m *Model) notesPanel(width, height int) string {
	th := m.deps.Theme
	notes := strings.TrimSpace(m.status.Latest.Notes)
	if notes == "" {
		return ""
	}

	wrapped := wordwrap.String(notes, max(width-4, 10))
	lines := strings.Split(wrapped, "\n")
	if limit := height - 2; len(lines) > limit {
		// A última linha vira o aviso de corte em vez de sumir calada: sem
		// isso, notas longas parecem terminar no meio de uma frase.
		lines = append(lines[:limit-1], th.Ghost.Render("… continua em "+shortURL(m.status.Latest.URL)))
	}

	return component.Panel{
		Title: "Novidades", Glyph: "✧", Accent: th.Accent,
		Width: width, Height: len(lines) + 2,
	}.Render(th, th.Dim.Render(strings.Join(lines, "\n")))
}

// footer é a linha de resultado ou de convite à ação, abaixo dos painéis.
func (m *Model) footer(width int) string {
	th := m.deps.Theme
	style := func(tone lipgloss.TerminalColor, s string) string {
		return lipgloss.NewStyle().Foreground(tone).Render(
			component.TruncateTail(s, width))
	}

	switch {
	case m.phase == phaseDone && m.err != nil:
		return style(th.Danger, "✗ "+firstLine(m.err.Error()))

	case m.phase == phaseDone && m.outcome.Restart:
		return style(th.Success, fmt.Sprintf("✓ atualizado para %s — reinicie o lealing para usar a versão nova",
			orDash(m.outcome.To)))

	case m.phase == phaseDone:
		return style(th.Success, "✓ "+orDash(m.outcome.Detail))

	// Erro de verificação sem release conhecido já ocupa o painel acima;
	// repeti-lo aqui só gasta a linha que serve para dizer o que fazer.
	case m.err != nil && m.status.Latest.Tag != "":
		return style(th.Warning, "! "+firstLine(m.err.Error()))

	case m.status.CanApply() && m.status.Install.Mode == selfupdate.ModeSource:
		return style(th.Accent, "u traz os commits novos da branch e recompila")

	case m.status.CanApply():
		return style(th.Accent, "u baixa "+m.status.Latest.Tag+", confere o checksum e troca o binário")

	default:
		return ""
	}
}

// panel monta uma moldura de lista rótulo/valor com a altura do conteúdo.
func (m *Model) panel(title, glyph string, accent lipgloss.TerminalColor, width int, rows []component.Row) string {
	th := m.deps.Theme
	content := component.FieldList{Rows: rows, Width: width - 4}.Render(th)
	return component.Panel{
		Title: title, Glyph: glyph, Accent: accent,
		Width: width, Height: len(rows) + 2,
	}.Render(th, content)
}

// stateTone traduz o estado da comparação em cor.
func (m *Model) stateTone() lipgloss.TerminalColor {
	th := m.deps.Theme
	switch m.status.State {
	case selfupdate.StateOutdated:
		return th.Warning
	case selfupdate.StateUpToDate:
		return th.Success
	case selfupdate.StateAhead:
		return th.Accent
	default:
		return th.Muted
	}
}

func (m *Model) Hints() []tui.Hint {
	hints := make([]tui.Hint, 0, 3)
	if m.status.CanApply() && m.phase != phaseApplying {
		hints = append(hints, tui.Hint{Key: "u", Label: "atualizar"})
	}
	return append(hints,
		tui.Hint{Key: "r", Label: "verificar"},
		tui.Hint{Key: "esc", Label: "voltar"},
	)
}

// Status alimenta a barra de status com a fase atual.
func (m *Model) Status() (string, lipgloss.TerminalColor) {
	th := m.deps.Theme
	switch m.phase {
	case phaseChecking:
		return "verificando…", th.Accent
	case phaseApplying:
		return "atualizando…", th.Accent
	case phaseDone:
		if m.err != nil {
			return "falhou", th.Danger
		}
		return "concluído", th.Success
	default:
		if m.err != nil {
			return "não verificado", th.Warning
		}
		return m.status.State.Label(), m.stateTone()
	}
}

var _ interface {
	Status() (string, lipgloss.TerminalColor)
} = (*Model)(nil)

// --- Formatação --------------------------------------------------------

// shortPath troca o diretório do usuário por "~", que é como o usuário
// reconhece o caminho e cabe em telas estreitas.
func shortPath(p, home string) string {
	if p == "" {
		return "—"
	}
	if home != "" && strings.HasPrefix(p, home) {
		return "~" + p[len(home):]
	}
	return p
}

// shortURL tira o esquema da URL, que é ruído numa linha apertada.
func shortURL(u string) string {
	if u == "" {
		return "github.com"
	}
	return strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
}

// humanAge descreve há quanto tempo o release saiu.
func humanAge(d time.Duration) string {
	switch {
	case d < time.Hour:
		return "agora há pouco"
	case d < 24*time.Hour:
		return fmt.Sprintf("há %dh", int(d.Hours()))
	case d < 48*time.Hour:
		return "ontem"
	default:
		return fmt.Sprintf("há %dd", int(d.Hours()/24))
	}
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// firstLine reduz um erro de várias linhas ao que cabe no rodapé.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
