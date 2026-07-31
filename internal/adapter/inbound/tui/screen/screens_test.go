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
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/confirmation"
	devkitscreen "github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/devkit"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/gitinsight"
	pluginscreen "github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/plugin"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/power"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/repoclone"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/requirements"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/sysinfo"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/update"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	coreaccount "github.com/mateuslh/lealing/internal/core/ccaccount"
	coredevkit "github.com/mateuslh/lealing/internal/core/devkit"
	"github.com/mateuslh/lealing/internal/core/domain"
	coregitinsight "github.com/mateuslh/lealing/internal/core/gitinsight"
	"github.com/mateuslh/lealing/internal/core/interactive"
	corepower "github.com/mateuslh/lealing/internal/core/power"
	corerepoclone "github.com/mateuslh/lealing/internal/core/repoclone"
	coreselfupdate "github.com/mateuslh/lealing/internal/core/selfupdate"
	coresysinfo "github.com/mateuslh/lealing/internal/core/sysinfo"
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

type fakeInteractiveOpener struct{ session interactive.Session }

func (f fakeInteractiveOpener) Open(context.Context, domain.ToolID, interactive.OpenOptions) (interactive.Session, error) {
	return f.session, nil
}

type fakeInteractiveSession struct{ updates chan interactive.Update }

func (f *fakeInteractiveSession) Updates() <-chan interactive.Update                      { return f.updates }
func (f *fakeInteractiveSession) Send(context.Context, interactive.Event) error           { return nil }
func (f *fakeInteractiveSession) Respond(context.Context, interactive.HostResponse) error { return nil }
func (f *fakeInteractiveSession) Shutdown(context.Context) error                          { return nil }

type fakeDevkitRunner struct{}

func (fakeDevkitRunner) Run(context.Context, coredevkit.Request) (coredevkit.Result, error) {
	return coredevkit.Result{
		Title:   "Diagnóstico concluído",
		Summary: "Resultado funcional com campos suficientes para exercitar rolagem e truncamento.",
		Rows: []coredevkit.Row{
			{Label: "Status", Value: "200 OK"},
			{Label: "Protocolo", Value: "HTTP/2.0"},
			{Label: "Destino final", Value: "https://api.uma-empresa-com-nome-muito-comprido.example/health"},
			{Label: "Latência", Value: "183ms"},
		},
		Warning: "A assinatura não foi verificada; use a chave do emissor antes de confiar no conteúdo.",
		Body: "{\n  \"servico\": \"pagamentos\",\n  \"regiao\": \"sa-east-1\",\n" +
			"  \"dependencias\": [\"postgres\", \"kafka\", \"redis\"],\n" +
			"  \"observacao\": \"conteúdo longo que precisa permanecer dentro do painel em qualquer geometria\"\n}",
	}, nil
}

type fakeGitScanner struct{ report coregitinsight.Report }

func (f fakeGitScanner) Scan(context.Context) (coregitinsight.Report, error) {
	return f.report, nil
}
func (fakeGitScanner) Fetch(context.Context, string) error { return nil }
func (fakeGitScanner) Push(context.Context, string, coregitinsight.Branch) error {
	return nil
}
func (fakeGitScanner) DeleteLocalBranch(context.Context, string, string) error {
	return nil
}
func (fakeGitScanner) UpdateAll(context.Context) (coregitinsight.UpdateReport, error) {
	return coregitinsight.UpdateReport{
		Results: []coregitinsight.UpdateResult{
			{Repository: "pagamentos/pagamentos", Branch: "main", State: coregitinsight.UpdateUpdated, Detail: "avançou 2 commits"},
			{Repository: "pagamentos/pagamentos-config", Branch: "main", State: coregitinsight.UpdateCurrent, Detail: "já estava em dia"},
			{Repository: "clientes/clientes-api", Branch: "master", State: coregitinsight.UpdateSkipped, Detail: "working tree alterada"},
			{Repository: "fraudes/motor-fraudes", State: coregitinsight.UpdateFailed, Detail: "fetch falhou"},
		},
	}, nil
}

type fakeRepoCloner struct {
	plan   corerepoclone.Plan
	result corerepoclone.Result
}

func (f fakeRepoCloner) Discover(context.Context, string) (corerepoclone.Plan, error) {
	return f.plan, nil
}

func (f fakeRepoCloner) Resolve(context.Context, corerepoclone.Source, string) (corerepoclone.Repository, error) {
	return corerepoclone.Repository{
		Owner: "banco-bradesco", Name: "pagamentos-extra",
		CloneURL: "https://github.com/banco-bradesco/pagamentos-extra",
	}, nil
}

func (f fakeRepoCloner) Clone(context.Context, corerepoclone.Plan) (corerepoclone.Result, error) {
	return f.result, nil
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

func repoCloneFixture() corerepoclone.Plan {
	source := corerepoclone.Source{
		Owner: "banco-bradesco", Repository: "pagamentos", Prefix: "pagamentos",
	}
	names := []string{
		"pagamentos", "pagamentos-config", "pagamentos-worker",
		"pagamentos-integracao-legado", "pagamentos-observabilidade",
	}
	repos := make([]corerepoclone.Repository, 0, len(names))
	for i, name := range names {
		repos = append(repos, corerepoclone.Repository{
			Owner: "banco-bradesco", Name: name,
			CloneURL:      "https://github.com/banco-bradesco/" + name,
			Description:   "Serviço da família de pagamentos responsável por integração e conciliação.",
			Visibility:    []string{"PRIVATE", "INTERNAL", "PUBLIC"}[i%3],
			Language:      []string{"Java", "Kotlin", "Go"}[i%3],
			DefaultBranch: "main",
			UpdatedAt:     fixedNow().AddDate(0, 0, -i),
			DiskUsageKB:   1536 * (i + 1),
			Archived:      i == len(names)-1,
		})
	}
	return corerepoclone.Plan{
		Source: source, Destination: "/Users/mateus/dev/pagamentos",
		Repositories: repos,
	}
}

func gitInsightFixture() coregitinsight.Report {
	branch := func(name, upstream string, current bool, ahead, behind int, subject string) coregitinsight.Branch {
		remote := ""
		if upstream != "" {
			remote = "origin"
		}
		return coregitinsight.Branch{
			Name: name, Upstream: upstream, Remote: remote,
			RemoteRef: "refs/heads/" + name, Current: current,
			Ahead: ahead, Behind: behind, Hash: "a1b2c3d",
			Subject: subject, CommittedAt: fixedNow().Add(-time.Duration(ahead+behind) * time.Hour),
		}
	}
	return coregitinsight.Report{
		Root:      "/Users/mateus/dev",
		ScannedAt: fixedNow(),
		Repositories: []coregitinsight.Repository{
			{
				Name: "pagamentos", Relative: "pagamentos/pagamentos",
				Path: "/Users/mateus/dev/pagamentos/pagamentos", DirtyFiles: 3,
				Branches: []coregitinsight.Branch{
					branch("main", "origin/main", true, 2, 1, "corrige conciliação de pagamentos"),
					branch("feature/pix-agendado", "origin/feature/pix-agendado", false, 4, 0, "adiciona pix agendado"),
					branch("feature/pronta", "origin/feature/pronta", false, 0, 2, "feature já publicada"),
					branch("rascunho-local", "", false, 0, 0, "experimento sem upstream"),
				},
			},
			{
				Name: "pagamentos-config", Relative: "pagamentos/pagamentos-config",
				Path: "/Users/mateus/dev/pagamentos/pagamentos-config",
				Branches: []coregitinsight.Branch{
					branch("main", "origin/main", true, 0, 0, "atualiza configurações"),
					branch("release/2026.07", "origin/release/2026.07", false, 0, 0, "release publicada"),
				},
			},
			{
				Name: "clientes", Relative: "clientes/clientes-api",
				Path: "/Users/mateus/dev/clientes/clientes-api",
				Branches: []coregitinsight.Branch{
					branch("main", "origin/main", true, 0, 0, "release estável"),
					{Name: "legado", Upstream: "origin/legado", Gone: true, Hash: "d4e5f6a", Subject: "branch removida no remoto"},
				},
			},
			{
				Name: "fraudes", Relative: "fraudes/motor-fraudes",
				Path: "/Users/mateus/dev/fraudes/motor-fraudes",
				Err:  "fatal: referência inválida",
			},
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

var cases = append([]screenCase{
	{
		name: "sysinfo",
		build: func(t *testing.T) tui.Screen {
			return settle(t, sysinfo.New(deps(), fakeInspector{snap: sysinfoFixture()}, fixedNow))
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
		name:  "plugin screen-v1",
		build: pluginScreenFixture,
		keys:  []string{"tab", "down"},
	},
	{
		name: "confirmação global",
		build: func(t *testing.T) tui.Screen {
			return confirmation.New(deps(), domain.Tool{
				ID: "external-danger", Name: "Tool externa destrutiva",
				Detail: "Altera um ambiente externo e exige confirmação da engine antes do spawn.",
				Risk:   domain.RiskDestructive,
			}, nil)
		},
	},
	{
		name: "atualizar",
		build: func(t *testing.T) tui.Screen {
			return settle(t, update.New(deps(), fakeUpdater{status: updateFixture()}, "", fixedNow))
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
			return settle(t, update.New(deps(), fakeUpdater{status: st}, "", fixedNow))
		},
	},
	{
		// Sem rede: o erro ocupa o painel do release, e é texto livre — o
		// único lugar da tela onde o comprimento não é nosso.
		name: "atualizar sem rede",
		build: func(t *testing.T) tui.Screen {
			err := errors.New("Get \"https://api.github.com/repos/mateuslh/lealing/releases/latest\": dial tcp: lookup api.github.com: no such host")
			return settle(t, update.New(deps(), fakeUpdater{err: err}, "", fixedNow))
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
	{
		name: "clone repo revisão",
		build: func(t *testing.T) tui.Screen {
			return cloneRepoPreview(t)
		},
		keys: []string{"down", " ", "down", "d", "up"},
	},
	{
		name: "clone repo adicionando",
		build: func(t *testing.T) tui.Screen {
			return cloneRepoPreview(t)
		},
		keys: []string{"a", "p", "a", "g", "a", "m", "e", "n", "t", "o", "s", "-", "extra"},
	},
	{
		name: "clone repo executando",
		build: func(t *testing.T) tui.Screen {
			return cloneRepoPreview(t)
		},
		keys: []string{"enter"},
	},
	{
		name: "radar git",
		build: func(t *testing.T) tui.Screen {
			return settle(t, gitinsight.New(deps(), fakeGitScanner{report: gitInsightFixture()}))
		},
		keys: []string{"down", "pgdown"},
	},
	{
		name: "radar git para push",
		build: func(t *testing.T) tui.Screen {
			return settle(t, gitinsight.New(deps(), fakeGitScanner{report: gitInsightFixture()}))
		},
		keys: []string{"right", "down", "pgdown"},
	},
	{
		name: "radar git locais publicadas",
		build: func(t *testing.T) tui.Screen {
			return settle(t, gitinsight.New(deps(), fakeGitScanner{report: gitInsightFixture()}))
		},
		keys: []string{"3", "down"},
	},
	{
		name: "radar git alterados",
		build: func(t *testing.T) tui.Screen {
			return settle(t, gitinsight.New(deps(), fakeGitScanner{report: gitInsightFixture()}))
		},
		keys: []string{"5"},
	},
	{
		name: "radar git escolhendo push",
		build: func(t *testing.T) tui.Screen {
			return settle(t, gitinsight.New(deps(), fakeGitScanner{report: gitInsightFixture()}))
		},
		keys: []string{"p", "down"},
	},
	{
		name: "radar git confirmando limpeza",
		build: func(t *testing.T) tui.Screen {
			return settle(t, gitinsight.New(deps(), fakeGitScanner{report: gitInsightFixture()}))
		},
		keys: []string{"d", "enter"},
	},
	{
		name: "radar git confirmando atualização geral",
		build: func(t *testing.T) tui.Screen {
			return settle(t, gitinsight.New(deps(), fakeGitScanner{report: gitInsightFixture()}))
		},
		keys: []string{"u"},
	},
	{
		name: "radar git atualizando todos",
		build: func(t *testing.T) tui.Screen {
			return settle(t, gitinsight.New(deps(), fakeGitScanner{report: gitInsightFixture()}))
		},
		keys: []string{"u", "enter"},
	},
	{
		name: "radar git resultado da atualização",
		build: func(t *testing.T) tui.Screen {
			return gitUpdateResults(t)
		},
		keys: []string{"down"},
	},
	{
		name: "pré-requisitos",
		build: func(t *testing.T) tui.Screen {
			tool := domain.Tool{ID: "clone-repo-bradesco", Name: "Clone Repo Bradesco"}
			missing := []domain.Requirement{
				{Executable: "git", Name: "Git", InstallHint: "instale o Git e adicione `git` ao PATH"},
				{Executable: "gh", Name: "GitHub CLI", InstallHint: "instale o GitHub CLI e rode `gh auth login`"},
			}
			return requirements.New(deps(), tool, missing)
		},
	},
}, devkitScreenCases()...)

func devkitScreenCases() []screenCase {
	definitions := coredevkit.Definitions()
	out := make([]screenCase, 0, len(definitions))
	for _, definition := range definitions {
		definition := definition
		out = append(out, screenCase{
			name: "devkit " + definition.ToolID,
			build: func(t *testing.T) tui.Screen {
				screen := tui.Screen(devkitscreen.New(deps(), fakeDevkitRunner{}, definition))
				next, cmd := screen.Update(keyMsg("enter"))
				if cmd == nil {
					t.Fatal("executar tool não devolveu comando")
				}
				next, _ = next.Update(cmd())
				return next
			},
			keys: []string{"down", "pgdown", "up"},
		})
	}
	return out
}

func pluginScreenFixture(t *testing.T) tui.Screen {
	t.Helper()
	updates := make(chan interactive.Update, 1)
	updates <- interactive.Update{
		State: interactive.StateRunning,
		Snapshot: &interactive.Snapshot{
			Sequence: 1,
			Body: "\x1b[1;36mTOOL EXTERNA\x1b[0m\n" +
				strings.Repeat("linha extensa do conteúdo central · ", 8) + "\n" +
				strings.Repeat("gráfico ▂▃▄▅▆▇█ ", 24),
			Hints:  []interactive.Hint{{Key: "tab", Label: "trocar aba"}, {Key: "esc", Label: "voltar"}},
			Status: "screen-v1 conectado",
		},
	}
	session := &fakeInteractiveSession{updates: updates}
	tool := domain.Tool{
		ID: "external-fixture", Name: "Tool externa", Kind: domain.KindProcess,
		Runtime: &domain.ExternalRuntime{UIMode: "screen-v1"},
	}
	screen := tui.Screen(pluginscreen.New(deps(), fakeInteractiveOpener{session: session}, nil, tool))
	opened := screen.Init()()
	screen, command := screen.Update(opened)
	if command == nil {
		t.Fatal("abertura screen-v1 não iniciou espera do snapshot")
	}
	message := command()
	if batch, ok := message.(tea.BatchMsg); ok {
		if len(batch) == 0 {
			t.Fatal("abertura produziu lote vazio")
		}
		message = batch[0]()
	}
	screen, _ = screen.Update(message)
	return screen
}

func cloneRepoPreview(t *testing.T) tui.Screen {
	t.Helper()
	s := tui.Screen(repoclone.New(deps(), fakeRepoCloner{plan: repoCloneFixture()}))
	for _, r := range "git@github.com:banco-bradesco/pagamentos.git" {
		s, _ = s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	var cmd tea.Cmd
	s, cmd = s.Update(keyMsg("enter"))
	if cmd == nil {
		t.Fatal("buscar não devolveu comando")
	}
	s, _ = s.Update(cmd())
	return s
}

func gitUpdateResults(t *testing.T) tui.Screen {
	t.Helper()
	s := settle(t, gitinsight.New(deps(), fakeGitScanner{report: gitInsightFixture()}))
	s, _ = s.Update(keyMsg("u"))
	var cmd tea.Cmd
	s, cmd = s.Update(keyMsg("enter"))
	if cmd == nil {
		t.Fatal("atualização geral não devolveu comando")
	}
	s, _ = s.Update(cmd())
	return s
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
