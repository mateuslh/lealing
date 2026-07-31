// Package screen_test verifica a geometria de todas as telas de tool de uma
// vez só.
//
// Cada tela nova entra na tabela `cases` abaixo. É o teste que impede uma
// tela de transbordar o frame, e foi ele que pegou a fila de KPIs estourando
// a largura quando o lipgloss dimensiona o conteúdo, não o bloco com borda.
package screen_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/ccaccount"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/power"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/sysinfo"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/tokens"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/update"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	coreaccount "github.com/mateuslh/lealing/internal/core/ccaccount"
	corepower "github.com/mateuslh/lealing/internal/core/power"
	coreselfupdate "github.com/mateuslh/lealing/internal/core/selfupdate"
	coresysinfo "github.com/mateuslh/lealing/internal/core/sysinfo"
	coretokens "github.com/mateuslh/lealing/internal/core/tokens"
)

var frames = []tui.Frame{
	{Width: 200, Height: 60},
	{Width: 150, Height: 42},
	{Width: 120, Height: 36},
	{Width: 100, Height: 30},
	{Width: 84, Height: 26},
	{Width: 70, Height: 22},
	{Width: 50, Height: 16},
	{Width: 34, Height: 12},
	{Width: 26, Height: 8},
}

func deps() tui.Deps { return tui.Deps{Theme: theme.Default()} }

func fixedNow() time.Time { return time.Date(2026, 7, 30, 15, 4, 5, 0, time.UTC) }

// --- Duplos ------------------------------------------------------------

type fakeInspector struct{ snap coresysinfo.Snapshot }

func (f fakeInspector) Inspect(context.Context) (coresysinfo.Snapshot, error) {
	return f.snap, nil
}

// fakeManager finge uma plataforma qualquer: features vazio vale como o
// painel completo, para que os testes que não se importam com plataforma não
// precisem declará-lo.
type fakeManager struct {
	settings corepower.Settings
	features corepower.Feature
}

func (f fakeManager) Read(context.Context) (corepower.Settings, error) { return f.settings, nil }
func (f fakeManager) Apply(context.Context, corepower.Settings) error  { return nil }
func (f fakeManager) PasswordlessEnabled(context.Context) bool         { return true }
func (f fakeManager) EnablePasswordless(context.Context) error         { return nil }
func (f fakeManager) DisablePasswordless(context.Context) error        { return nil }
func (f fakeManager) Defaults() corepower.Settings                     { return f.settings }

func (f fakeManager) Features() corepower.Feature {
	if f.features == 0 {
		return corepower.AllFeatures
	}
	return f.features
}

// fakeSwitcher responde ao contrato da tool de contas sem chaveiro nem
// ~/.claude.json por perto. As ações não fazem nada: a tela só precisa
// desenhar o que já leu.
type fakeSwitcher struct{ state coreaccount.State }

func (f fakeSwitcher) State(context.Context) (coreaccount.State, error) { return f.state, nil }

func (f fakeSwitcher) Save(_ context.Context, name string) (coreaccount.Profile, error) {
	return coreaccount.Profile{Name: name}, nil
}

func (f fakeSwitcher) SaveOverwriting(ctx context.Context, name string) (coreaccount.Profile, error) {
	return f.Save(ctx, name)
}

func (f fakeSwitcher) Activate(context.Context, string) error            { return nil }
func (f fakeSwitcher) ActivateOverwriting(context.Context, string) error { return nil }
func (f fakeSwitcher) Forget(context.Context, string) error              { return nil }

// fakeUpdater devolve um veredito pronto: a tela não decide nada sobre
// versões, só desenha o que o núcleo concluiu.
type fakeUpdater struct {
	status coreselfupdate.Status
	err    error
}

func (f fakeUpdater) Check(context.Context) (coreselfupdate.Status, error) {
	return f.status, f.err
}

func (f fakeUpdater) Apply(context.Context, coreselfupdate.Status) (coreselfupdate.Outcome, error) {
	return coreselfupdate.Outcome{To: f.status.Latest.Tag, Restart: true}, nil
}

type fakeGenerator struct{ report coretokens.Report }

func (f fakeGenerator) Generate(context.Context) (coretokens.Report, error) {
	return f.report, nil
}

// --- Fixtures ----------------------------------------------------------

func sysinfoFixture() coresysinfo.Snapshot {
	return coresysinfo.Snapshot{
		OSVersion: "macOS 27.0 (26A5353q)", HostName: "MacBook Pro de Mateus",
		Model: "Mac15,6", Chip: "Apple M3 Pro", Cores: "11 núcleos",
		Memory: "18 GB", Uptime: "25d 2h 58min",
		BatteryLevel: "51%", BatteryState: "Carregando · 6h12 restantes",
		HasBattery: true,
	}
}

// accountsFixture tem uma conta ativa já guardada e outra com a sessão
// vencida, que é o par que exercita os dois tons da lista.
func accountsFixture() coreaccount.State {
	pessoal := coreaccount.Identity{
		Email: "mateus@exemplo.com", DisplayName: "Mateus",
		Organization: "Conta pessoal do Mateus", AccountUUID: "uuid-pessoal", Plan: "pro",
		ExpiresAt: fixedNow().Add(3 * time.Hour), RenewsUntil: fixedNow().AddDate(0, 0, 20),
	}
	trabalho := coreaccount.Identity{
		Email:        "mateus@uma-empresa-com-nome-comprido.com.br",
		Organization: "Uma Empresa Com Nome Bem Comprido Ltda", AccountUUID: "uuid-trabalho",
		Plan: "max", ExpiresAt: fixedNow().AddDate(0, 0, -3), RenewsUntil: fixedNow().AddDate(0, 0, -1),
	}
	return coreaccount.State{
		Active: pessoal, HasActive: true, ActiveProfile: "pessoal",
		Origin: "chaveiro do macOS",
		Profiles: []coreaccount.Profile{
			{Name: "pessoal", Identity: pessoal, SavedAt: fixedNow().AddDate(0, 0, -2)},
			{Name: "trabalho", Identity: trabalho, SavedAt: fixedNow().AddDate(0, 0, -9)},
		},
	}
}

// updateFixture é o caso apertado da tela de atualização: notas longas, que
// é o que empurra o painel para fora do frame quando a altura não é medida.
func updateFixture() coreselfupdate.Status {
	return coreselfupdate.Status{
		Install: coreselfupdate.Install{
			Mode:       coreselfupdate.ModeRelease,
			BinaryPath: "/Users/alguem/.local/bin/lealing",
			Writable:   true,
		},
		Current: coreselfupdate.ParseVersion("v1.3.0"),
		Latest: coreselfupdate.Release{
			Tag:         "v1.4.0",
			PublishedAt: fixedNow().Add(-36 * time.Hour),
			URL:         "https://github.com/mateuslh/lealing/releases/tag/v1.4.0",
			Notes: "Nova tool de atualização, que compara a versão instalada com o " +
				"último release e troca o binário sem sair da TUI.\n" +
				"Correção do painel de energia que estourava a largura em janelas estreitas.\n" +
				"A busca agora considera as palavras-chave declaradas no catálogo.",
		},
		State: coreselfupdate.StateOutdated,
	}
}

func tokensFixture() coretokens.Report {
	totals := coretokens.Totals{Input: 1_000_000, Output: 500_000, Messages: 3393, Cost: 330.68}
	slice := func(label string, cost float64) coretokens.Slice {
		return coretokens.Slice{Label: label, Totals: coretokens.Totals{Cost: cost, Messages: 10}}
	}

	days := make([]coretokens.DayPoint, 0, 30)
	base := fixedNow().AddDate(0, 0, -29)
	for i := range 30 {
		d := base.AddDate(0, 0, i)
		days = append(days, coretokens.DayPoint{
			Date: d, Day: d.Format(time.DateOnly),
			Totals: coretokens.Totals{Cost: float64(i) * 3.7},
		})
	}

	return coretokens.Report{
		Overall: totals,
		ByProvider: []coretokens.Slice{
			slice("Claude Code", 328.81), slice("Codex", 1.87),
		},
		ByModel: []coretokens.Slice{
			slice("claude-sonnet-5", 184.52), slice("claude-opus-4-8", 91.21),
			slice("claude-opus-5", 52.57), slice("gpt-5.5", 1.39),
		},
		ByProject: []coretokens.Slice{
			slice("leanling", 120), slice("um-nome-de-projeto-absurdamente-longo", 80),
		},
		ByDay:     days,
		Providers: []string{"Claude Code", "Codex"},
		Windows: []coretokens.WindowUsage{
			{Label: "Últimas 5h", Totals: coretokens.Totals{Cost: 48.62},
				ByProvider: []coretokens.Slice{slice("Claude Code", 48.62)}},
			{Label: "Hoje", Totals: coretokens.Totals{Cost: 52.57},
				ByProvider: []coretokens.Slice{slice("Claude Code", 52.57)}},
			{Label: "7 dias", Totals: coretokens.Totals{Cost: 144.26},
				ByProvider: []coretokens.Slice{slice("Claude Code", 143.1), slice("Codex", 1.16)}},
			{Label: "Este mês", Totals: coretokens.Totals{Cost: 329.53}},
		},
		// Cotas consultadas na conta (Claude Code) e lidas do log (Codex), que
		// é o par de origens que o painel de limites precisa distinguir.
		RateWindows: []coretokens.RateWindow{
			{Provider: "Claude Code", Label: "Sessão 5h", UsedPercent: 53, WindowMinutes: 300,
				ResetsAt: fixedNow().Add(90 * time.Minute), ObservedAt: fixedNow(), Source: "conta"},
			{Provider: "Claude Code", Label: "Semana", UsedPercent: 31, WindowMinutes: 10080,
				ResetsAt: fixedNow().AddDate(0, 0, 2), ObservedAt: fixedNow(), Source: "conta"},
			{Provider: "Codex", Label: "Semana", UsedPercent: 1, WindowMinutes: 10080,
				ResetsAt: fixedNow().AddDate(0, 0, 4), ObservedAt: fixedNow().Add(-10 * time.Minute),
				Source: "log local"},
			{Provider: "Codex", Label: "Sessão 5h", UsedPercent: 94, WindowMinutes: 300,
				ResetsAt: fixedNow().Add(2 * time.Hour), ObservedAt: fixedNow().Add(-10 * time.Minute),
				Source: "log local"},
		},
		// Um saldo com teto (barra) e uma carteira sem teto (só o valor):
		// as duas formas que as CLIs usam para contar crédito.
		Credits: []coretokens.Credits{
			{Provider: "Claude Code", Used: 83.89, Limit: 275, Currency: "BRL", Enabled: true},
			{Provider: "Codex", Balance: 12.5, Enabled: true},
		},
	}
}

// --- Tabela de telas ---------------------------------------------------

type screenCase struct {
	name string
	// build monta a tela já carregada, drenando o Init de forma síncrona.
	build func(t *testing.T) tui.Screen
	// keys são interações aplicadas antes do render.
	keys []string
}

var cases = []screenCase{
	{
		name: "sysinfo",
		build: func(t *testing.T) tui.Screen {
			return settle(t, sysinfo.New(deps(), fakeInspector{snap: sysinfoFixture()}))
		},
	},
	{
		name: "power",
		build: func(t *testing.T) tui.Screen {
			settings := corepower.Settings{
				Battery: corepower.Profile{Sleep: 60, DisplaySleep: 60, DiskSleep: 60, HibernateMode: 3},
				AC:      corepower.Profile{Sleep: 10, DisplaySleep: 10, PowerNap: true, HibernateMode: 3},
			}
			return settle(t, power.New(deps(), fakeManager{settings: settings}))
		},
		// Ajusta com as setas e troca de fonte com shift — o caminho que a
		// tela pede — além do preset, que marca tudo como pendente.
		keys: []string{"down", "right", "shift+right", "left", "1"},
	},
	{
		// Plataforma parcial (o powercfg do Windows): três campos por coluna
		// e nenhum cadeado no rodapé. O painel encolhe, e é aí que sobra
		// espaço vazio dentro da moldura se a altura não acompanhar.
		name: "power parcial",
		build: func(t *testing.T) tui.Screen {
			settings := corepower.Settings{
				Battery: corepower.Profile{Sleep: 15, DisplaySleep: 5, DiskSleep: 10},
				AC:      corepower.Profile{Sleep: 30, DisplaySleep: 10, DiskSleep: 20},
			}
			return settle(t, power.New(deps(), fakeManager{
				settings: settings,
				features: corepower.FeatureSleep | corepower.FeatureDisplaySleep | corepower.FeatureDiskSleep,
			}))
		},
		keys: []string{"down", "right", "tab", "2"},
	},
	{
		name: "tokens",
		build: func(t *testing.T) tui.Screen {
			return settle(t, tokens.New(deps(), fakeGenerator{report: tokensFixture()}, fixedNow))
		},
		keys: []string{"tab", "down", "down"},
	},
	{
		name: "atualizar",
		build: func(t *testing.T) tui.Screen {
			return settle(t, update.New(deps(), fakeUpdater{status: updateFixture()}))
		},
	},
	{
		// Instalação de fonte: o painel troca o caminho do binário por clone
		// e branch, e o rodapé vira o convite a recompilar.
		name: "atualizar do fonte",
		build: func(t *testing.T) tui.Screen {
			st := updateFixture()
			st.Install = coreselfupdate.Install{
				Mode:       coreselfupdate.ModeSource,
				BinaryPath: "/Users/alguem/projetos/lealing/bin/lealing",
				RepoDir:    "/Users/alguem/projetos/lealing",
				Branch:     "main",
			}
			st.State = coreselfupdate.StateAhead
			return settle(t, update.New(deps(), fakeUpdater{status: st}))
		},
	},
	{
		// Sem rede: o erro ocupa o painel do release, e é texto livre — o
		// único lugar da tela onde o comprimento não é nosso.
		name: "atualizar sem rede",
		build: func(t *testing.T) tui.Screen {
			err := errors.New("Get \"https://api.github.com/repos/mateuslh/lealing/releases/latest\": dial tcp: lookup api.github.com: no such host")
			return settle(t, update.New(deps(), fakeUpdater{err: err}))
		},
	},
	{
		name: "tokens vazio",
		build: func(t *testing.T) tui.Screen {
			return settle(t, tokens.New(deps(), fakeGenerator{}, fixedNow))
		},
	},
	{
		name: "contas",
		build: func(t *testing.T) tui.Screen {
			return settle(t, ccaccount.New(deps(), fakeSwitcher{state: accountsFixture()}, fixedNow))
		},
		keys: []string{"down"},
	},
	{
		// O campo de nome e a confirmação são linhas de rodapé que crescem
		// com o texto: é onde uma janela estreita estoura primeiro.
		name: "contas digitando",
		build: func(t *testing.T) tui.Screen {
			return settle(t, ccaccount.New(deps(), fakeSwitcher{state: accountsFixture()}, fixedNow))
		},
		keys: []string{"s", "x", "d"},
	},
	{
		name: "contas sem sessão",
		build: func(t *testing.T) tui.Screen {
			return settle(t, ccaccount.New(deps(), fakeSwitcher{}, fixedNow))
		},
	},
}

// settle executa o Init e entrega a mensagem resultante à tela.
func settle(t *testing.T, s tui.Screen) tui.Screen {
	t.Helper()
	cmd := s.Init()
	if cmd == nil {
		return s
	}
	if msg := cmd(); msg != nil {
		s, _ = s.Update(msg)
	}
	return s
}

func TestTelasNuncaEstouramOFrame(t *testing.T) {
	for _, tc := range cases {
		for _, f := range frames {
			t.Run(tc.name, func(t *testing.T) {
				s := tc.build(t)
				s, _ = s.Update(tea.WindowSizeMsg{Width: f.Width, Height: f.Height})
				for _, key := range tc.keys {
					s, _ = s.Update(keyMsg(key))
				}

				out := s.View(f)
				lines := strings.Split(out, "\n")

				if len(lines) > f.Height {
					t.Errorf("%dx%d: %d linhas excedem a altura", f.Width, f.Height, len(lines))
				}
				for i, line := range lines {
					if got := lipgloss.Width(line); got > f.Width {
						t.Errorf("%dx%d: linha %d tem %d colunas\n%s",
							f.Width, f.Height, i, got, stripANSI(line))
					}
				}
			})
		}
	}
}

func TestTelasAnunciamAtalhosEIdentidade(t *testing.T) {
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.build(t)
			if s.ID() == "" {
				t.Error("ID vazio")
			}
			if s.Title() == "" {
				t.Error("Title vazio: o breadcrumb ficaria em branco")
			}
			if len(s.Hints()) == 0 {
				t.Error("sem hints: a statusbar não teria o que mostrar")
			}
			// Toda tela de tool precisa dizer como sair dela.
			var hasEsc bool
			for _, h := range s.Hints() {
				if strings.Contains(h.Key, "esc") {
					hasEsc = true
				}
			}
			if !hasEsc {
				t.Error("nenhum hint menciona esc: o usuário fica sem saída visível")
			}
		})
	}
}

func TestPowerDetectaAlteracaoPendente(t *testing.T) {
	settings := corepower.Settings{Battery: corepower.Profile{Sleep: 60}}
	s := settle(t, power.New(deps(), fakeManager{settings: settings}))

	m, ok := s.(*power.Model)
	if !ok {
		t.Fatalf("tipo inesperado %T", s)
	}
	if m.HasUnsavedChanges() {
		t.Fatal("tela recém-carregada já se diz alterada")
	}

	// Espaço no primeiro campo (Dormir) avança a escala de minutos.
	next, _ := m.Update(keyMsg(" "))
	m = next.(*power.Model)
	if !m.HasUnsavedChanges() {
		t.Error("editar um campo não marcou alteração pendente")
	}

	// "d" descarta e volta ao estado do sistema.
	next, _ = m.Update(keyMsg("d"))
	m = next.(*power.Model)
	if m.HasUnsavedChanges() {
		t.Error("descartar não voltou ao estado salvo")
	}
}

func keyMsg(key string) tea.KeyMsg {
	switch key {
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "shift+left":
		return tea.KeyMsg{Type: tea.KeyShiftLeft}
	case "shift+right":
		return tea.KeyMsg{Type: tea.KeyShiftRight}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
