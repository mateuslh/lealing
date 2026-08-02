package usersync_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mateuslh/lealing/internal/core/usersync"
)

// --- Duplos ------------------------------------------------------------

type fakeAuth struct {
	code       usersync.DeviceCode
	credential usersync.Credential
	identity   usersync.Identity
	waitErr    error
}

func (f *fakeAuth) RequestDevice(context.Context) (usersync.DeviceCode, error) {
	return f.code, nil
}
func (f *fakeAuth) Wait(context.Context, usersync.DeviceCode) (usersync.Credential, error) {
	return f.credential, f.waitErr
}
func (f *fakeAuth) Identity(context.Context, usersync.Credential) (usersync.Identity, error) {
	return f.identity, nil
}

type fakeTokens struct{ credential usersync.Credential }

func (f *fakeTokens) Load(context.Context) (usersync.Credential, error) { return f.credential, nil }
func (f *fakeTokens) Save(_ context.Context, c usersync.Credential) error {
	f.credential = c
	return nil
}
func (f *fakeTokens) Delete(context.Context) error {
	f.credential = usersync.Credential{}
	return nil
}

type fakeRemote struct {
	snapshot usersync.Snapshot
	written  usersync.State
	expected string
	ensured  string
	writeErr error
	readErr  error
	revision int
}

func (f *fakeRemote) Ensure(_ context.Context, _ usersync.Credential, name string) (usersync.Repository, error) {
	f.ensured = name
	return usersync.Repository{Owner: "alguem", Name: name, Private: true}, nil
}

func (f *fakeRemote) Read(context.Context, usersync.Credential, string) (usersync.Snapshot, error) {
	return f.snapshot, f.readErr
}

func (f *fakeRemote) Write(
	_ context.Context, _ usersync.Credential, _ string, state usersync.State, expected string,
) (string, error) {
	if f.writeErr != nil {
		return "", f.writeErr
	}
	f.written, f.expected = state, expected
	f.revision++
	f.snapshot = usersync.Snapshot{State: state, Revision: revisionName(f.revision)}
	return f.snapshot.Revision, nil
}

func revisionName(n int) string { return "rev" + strings.Repeat("x", n) }

type fakeLocal struct {
	state   usersync.State
	applied usersync.State
	scope   usersync.Selection
}

func (f *fakeLocal) Collect(context.Context) (usersync.State, error) { return f.state, nil }
func (f *fakeLocal) Apply(_ context.Context, state usersync.State, selection usersync.Selection) (usersync.Applied, error) {
	f.applied, f.scope = state, selection
	return usersync.Applied{usersync.SectionUsage: len(state.Usage)}, nil
}

type fakeSettings struct{ settings usersync.Settings }

func (f *fakeSettings) Load(context.Context) (usersync.Settings, error) { return f.settings, nil }
func (f *fakeSettings) Save(_ context.Context, s usersync.Settings) error {
	f.settings = s
	return nil
}

func fixedNow() time.Time { return time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC) }

type harness struct {
	service  *usersync.Service
	auth     *fakeAuth
	tokens   *fakeTokens
	remote   *fakeRemote
	local    *fakeLocal
	settings *fakeSettings
}

func newHarness(connected bool) *harness {
	h := &harness{
		auth:   &fakeAuth{identity: usersync.Identity{Login: "alguem"}},
		tokens: &fakeTokens{},
		remote: &fakeRemote{},
		local: &fakeLocal{state: usersync.State{
			Usage:   []usersync.ToolUsage{{ID: "git-dev-radar", Runs: 3, Favorite: true}},
			Sources: []usersync.MarketplaceSource{{Name: "meu-repo", Kind: "local", Ref: "/tmp/tools", Enabled: true}},
			Tools:   []usersync.InstalledTool{{ID: "token-usage", Version: "1.0.1"}},
		}},
		settings: &fakeSettings{},
	}
	if connected {
		h.tokens.credential = usersync.Credential{Token: "t0ken"}
		h.settings.settings = usersync.Settings{
			Identity: usersync.Identity{Login: "alguem"}, Repository: usersync.DefaultRepository,
		}.WithSelection(usersync.DefaultSelection())
	}
	h.service = usersync.NewService(usersync.Config{
		Auth: h.auth, Tokens: h.tokens, Remote: h.remote, Local: h.local, Settings: h.settings,
		Now: fixedNow, Device: "mac-de-teste", Engine: "1.2.3",
	})
	return h
}

// --- Testes ------------------------------------------------------------

func TestStatusSemContaNaoTocaNoRemoto(t *testing.T) {
	h := newHarness(false)
	status, err := h.service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Connected {
		t.Fatal("status disse conectado sem credencial")
	}
	// O painel ainda precisa mostrar o que existe na máquina.
	if len(status.Local.Usage) != 1 {
		t.Fatalf("estado local = %+v", status.Local)
	}
}

func TestStatusSobreviveAoRemotoIndisponivel(t *testing.T) {
	h := newHarness(true)
	h.remote.readErr = errors.New("sem rede")

	status, err := h.service.Status(context.Background())
	if err != nil {
		t.Fatalf("Status devolveu erro em vez de degradar: %v", err)
	}
	if !status.Connected || status.RemoteErr == nil {
		t.Fatalf("status = %+v", status)
	}
	if len(status.Local.Usage) != 1 {
		t.Fatal("o estado local sumiu junto com a rede")
	}
}

func TestLoginGuardaCredencialEPreparaRepositorio(t *testing.T) {
	h := newHarness(false)
	h.auth.credential = usersync.Credential{Token: "novo"}

	identity, err := h.service.CompleteLogin(context.Background(), usersync.DeviceCode{})
	if err != nil {
		t.Fatal(err)
	}
	if identity.Login != "alguem" || h.tokens.credential.Token != "novo" {
		t.Fatalf("identidade=%+v credencial=%+v", identity, h.tokens.credential)
	}
	if h.remote.ensured != usersync.DefaultRepository {
		t.Fatalf("repositório preparado = %q", h.remote.ensured)
	}
	if h.settings.settings.Selection().Empty() {
		t.Fatal("nenhuma seção ficou habilitada após o login")
	}
}

func TestPushCarimbaMaquinaEVersaoEGuardaRevisao(t *testing.T) {
	h := newHarness(true)

	result, err := h.service.Push(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if h.remote.written.Device != "mac-de-teste" || h.remote.written.Engine != "1.2.3" {
		t.Fatalf("documento enviado = %+v", h.remote.written)
	}
	if !h.remote.written.UpdatedAt.Equal(fixedNow()) {
		t.Fatalf("carimbo = %v", h.remote.written.UpdatedAt)
	}
	if h.settings.settings.Revision != result.Revision || result.Revision == "" {
		t.Fatalf("revisão guardada = %q, resultado = %q", h.settings.settings.Revision, result.Revision)
	}
}

// Uma seção desligada não pode sair da máquina: é a promessa que o painel faz.
func TestSecaoDesligadaNaoEEnviada(t *testing.T) {
	h := newHarness(true)
	if err := h.service.SetSection(context.Background(), usersync.SectionTools, false); err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.Push(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if len(h.remote.written.Tools) != 0 {
		t.Fatalf("tools foram enviadas mesmo desligadas: %+v", h.remote.written.Tools)
	}
	if len(h.remote.written.Usage) == 0 {
		t.Fatal("a seção ligada deixou de ser enviada")
	}
}

func TestPushRecusaSobrescreverRemotoQueAvancou(t *testing.T) {
	h := newHarness(true)
	// Esta máquina conhece uma revisão; o remoto já está em outra.
	h.settings.settings.Revision = "conhecida"
	h.remote.writeErr = usersync.ErrConflict

	if _, err := h.service.Push(context.Background(), false); !errors.Is(err, usersync.ErrConflict) {
		t.Fatalf("Push = %v, quero conflito", err)
	}

	// Com force, a revisão esperada é abandonada de propósito.
	h.remote.writeErr = nil
	if _, err := h.service.Push(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if h.remote.expected != "" {
		t.Fatalf("force ainda enviou a revisão esperada %q", h.remote.expected)
	}
}

func TestPullAplicaSomenteAsSecoesLigadas(t *testing.T) {
	h := newHarness(true)
	h.remote.snapshot = usersync.Snapshot{
		Revision: "remota",
		State: usersync.State{
			Version: usersync.StateVersion,
			Usage:   []usersync.ToolUsage{{ID: "power-control", Runs: 9}},
			Sources: []usersync.MarketplaceSource{{Name: "outro", Kind: "remote", Ref: "https://x.test/i.json"}},
		},
	}
	if err := h.service.SetSection(context.Background(), usersync.SectionSources, false); err != nil {
		t.Fatal(err)
	}

	if _, err := h.service.Pull(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if h.local.scope.Enabled(usersync.SectionSources) {
		t.Fatal("a aplicação recebeu uma seção desligada")
	}
	if h.settings.settings.Revision != "remota" {
		t.Fatalf("revisão após pull = %q", h.settings.settings.Revision)
	}
}

func TestPullRecusaEstadoDeVersaoFutura(t *testing.T) {
	h := newHarness(true)
	h.remote.snapshot = usersync.Snapshot{
		Revision: "remota",
		State:    usersync.State{Version: usersync.StateVersion + 1},
	}
	_, err := h.service.Pull(context.Background(), false)
	if err == nil || !strings.Contains(err.Error(), "versão mais nova") {
		t.Fatalf("Pull = %v", err)
	}
}

func TestPullSemNadaPublicadoDizIsso(t *testing.T) {
	h := newHarness(true)
	h.remote.snapshot = usersync.Snapshot{Missing: true}
	if _, err := h.service.Pull(context.Background(), false); !errors.Is(err, usersync.ErrNoRemote) {
		t.Fatalf("Pull = %v", err)
	}
}

// Baixar por cima de mudanças locais ainda não enviadas precisa perguntar.
func TestPullDetectaMudancaLocalNaoEnviada(t *testing.T) {
	h := newHarness(true)
	h.settings.settings.Revision = "mesma"
	h.remote.snapshot = usersync.Snapshot{
		Revision: "mesma",
		State: usersync.State{
			Version: usersync.StateVersion,
			Usage:   []usersync.ToolUsage{{ID: "outra-coisa", Runs: 1}},
		},
	}
	if _, err := h.service.Pull(context.Background(), false); !errors.Is(err, usersync.ErrConflict) {
		t.Fatalf("Pull = %v, quero conflito", err)
	}
	if _, err := h.service.Pull(context.Background(), true); err != nil {
		t.Fatalf("Pull com force = %v", err)
	}
}

func TestOperacoesRemotasExigemConta(t *testing.T) {
	h := newHarness(false)
	if _, err := h.service.Push(context.Background(), false); !errors.Is(err, usersync.ErrNotAuthenticated) {
		t.Fatalf("Push sem conta = %v", err)
	}
	if _, err := h.service.Pull(context.Background(), false); !errors.Is(err, usersync.ErrNotAuthenticated) {
		t.Fatalf("Pull sem conta = %v", err)
	}
}

func TestLogoutEsqueceCredencialERevisao(t *testing.T) {
	h := newHarness(true)
	h.settings.settings.Revision = "alguma"

	if err := h.service.Logout(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !h.tokens.credential.Empty() || h.settings.settings.Revision != "" {
		t.Fatalf("credencial=%+v revisão=%q", h.tokens.credential, h.settings.settings.Revision)
	}
	if !h.settings.settings.Identity.Empty() {
		t.Fatal("a identidade sobreviveu ao logout")
	}
}

func TestNormalizeProduzOrdemEstavel(t *testing.T) {
	state := usersync.State{
		Usage: []usersync.ToolUsage{{ID: "zulu"}, {ID: "alfa"}},
		Tools: []usersync.InstalledTool{{ID: "zeta"}, {ID: "beta"}},
	}
	state.Normalize()
	if state.Usage[0].ID != "alfa" || state.Tools[0].ID != "beta" {
		t.Fatalf("ordem = %+v", state)
	}
}

func TestValidateRecusaDocumentoIncoerente(t *testing.T) {
	for name, state := range map[string]usersync.State{
		"uso sem id":       {Usage: []usersync.ToolUsage{{ID: " "}}},
		"uso duplicado":    {Usage: []usersync.ToolUsage{{ID: "a"}, {ID: "a"}}},
		"origem sem ref":   {Sources: []usersync.MarketplaceSource{{Name: "x"}}},
		"origem duplicada": {Sources: []usersync.MarketplaceSource{{Name: "x", Ref: "r"}, {Name: "x", Ref: "r"}}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := state.Validate(); err == nil {
				t.Fatal("documento inválido foi aceito")
			}
		})
	}
}
