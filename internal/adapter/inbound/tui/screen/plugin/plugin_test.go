package plugin_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/plugin"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/interactive"
)

type fakeSession struct {
	updates chan interactive.Update

	mu        sync.Mutex
	events    []interactive.Event
	responses []interactive.HostResponse
	shutdown  bool
}

func newSession(updates ...interactive.Update) *fakeSession {
	channel := make(chan interactive.Update, 16)
	for _, update := range updates {
		channel <- update
	}
	return &fakeSession{updates: channel}
}

func (s *fakeSession) Updates() <-chan interactive.Update { return s.updates }
func (s *fakeSession) Send(_ context.Context, event interactive.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
	return nil
}
func (s *fakeSession) Respond(_ context.Context, response interactive.HostResponse) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.responses = append(s.responses, response)
	return nil
}
func (s *fakeSession) Shutdown(context.Context) error {
	s.mu.Lock()
	s.shutdown = true
	s.mu.Unlock()
	return nil
}

type fakeOpener struct {
	mu      sync.Mutex
	session interactive.Session
	err     error
	calls   int
}

func (o *fakeOpener) Open(context.Context, domain.ToolID, interactive.OpenOptions) (interactive.Session, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.calls++
	return o.session, o.err
}

func tool() domain.Tool {
	return domain.Tool{
		ID: "external-demo", Name: "Demo Externa", Kind: domain.KindProcess,
		Runtime: &domain.ExternalRuntime{UIMode: "screen-v1", ProtocolMin: 1, ProtocolMax: 1},
	}
}

func deps() tui.Deps { return tui.Deps{Theme: theme.Default()} }

func open(t *testing.T, opener *fakeOpener) (*plugin.Model, tea.Cmd) {
	t.Helper()
	model := plugin.New(deps(), opener, nil, tool())
	message := model.Init()()
	next, command := model.Update(message)
	return next.(*plugin.Model), command
}

func TestPluginScreenLoadingRunningErroECrash(t *testing.T) {
	t.Run("loading", func(t *testing.T) {
		model := plugin.New(deps(), &fakeOpener{}, nil, tool())
		if view := model.View(tui.Frame{Width: 60, Height: 12}); !strings.Contains(view, "iniciando") {
			t.Fatalf("view = %q", view)
		}
	})

	t.Run("erro de inicialização", func(t *testing.T) {
		model, _ := open(t, &fakeOpener{err: errors.New("manifest incompatível")})
		if view := model.View(tui.Frame{Width: 60, Height: 12}); !strings.Contains(view, "manifest incompatível") {
			t.Fatalf("view = %q", view)
		}
	})

	t.Run("running e crashed", func(t *testing.T) {
		session := newSession(interactive.Update{State: interactive.StateRunning, Snapshot: &interactive.Snapshot{
			Sequence: 1, Body: "conteúdo externo", Hints: []interactive.Hint{{Key: "x", Label: "ação"}}, Status: "pronta",
		}})
		model, wait := open(t, &fakeOpener{session: session})
		if wait == nil {
			t.Fatal("abertura não começou a esperar updates")
		}
		next, nextWait := model.Update(wait())
		model = next.(*plugin.Model)
		if view := model.View(tui.Frame{Width: 60, Height: 12}); !strings.Contains(view, "conteúdo externo") {
			t.Fatalf("view = %q", view)
		}
		status, _ := model.Status()
		if status != "pronta" {
			t.Errorf("status = %q", status)
		}

		failed := interactive.Update{State: interactive.StateFailed, Err: errors.New("exit status 7")}
		session.updates <- failed
		next, _ = model.Update(nextWait())
		model = next.(*plugin.Model)
		if view := model.View(tui.Frame{Width: 60, Height: 12}); !strings.Contains(view, "exit status 7") {
			t.Fatalf("crash não apareceu: %q", view)
		}
	})
}

func TestSnapshotERecortadoESanitizado(t *testing.T) {
	body := "\x1b]0;roubar chrome\x07" + strings.Repeat("x", 100) + "\nlinha2\nlinha3"
	session := newSession(interactive.Update{State: interactive.StateRunning, Snapshot: &interactive.Snapshot{Sequence: 1, Body: body}})
	model, wait := open(t, &fakeOpener{session: session})
	next, _ := model.Update(wait())
	model = next.(*plugin.Model)
	view := model.View(tui.Frame{Width: 20, Height: 2})
	lines := strings.Split(view, "\n")
	if len(lines) > 2 || strings.ContainsRune(view, '\x1b') {
		t.Fatalf("frame inseguro: %q", view)
	}
	for _, line := range lines {
		if lipgloss.Width(line) > 20 {
			t.Errorf("linha excedeu: %q", line)
		}
	}
}

func TestSnapshotNaoEscapaPelosHintsOuStatus(t *testing.T) {
	session := newSession(interactive.Update{State: interactive.StateRunning, Snapshot: &interactive.Snapshot{
		Sequence: 1, Body: "seguro",
		Hints:  []interactive.Hint{{Key: "\x1b]0;título\x07esc", Label: "\x1b[2Jlimpar"}},
		Status: "\x1b]52;c;Y2xpcA==\x07status",
	}})
	model, wait := open(t, &fakeOpener{session: session})
	next, _ := model.Update(wait())
	model = next.(*plugin.Model)
	for _, hint := range model.Hints() {
		if strings.ContainsRune(hint.Key+hint.Label, '\x1b') {
			t.Fatalf("hint inseguro: %+v", hint)
		}
	}
	status, _ := model.Status()
	if strings.ContainsRune(status, '\x1b') || status != "status" {
		t.Fatalf("status inseguro: %q", status)
	}
}

func TestHintsSempreContemEscECapturingEncaminhaTexto(t *testing.T) {
	session := newSession(interactive.Update{State: interactive.StateRunning, Snapshot: &interactive.Snapshot{
		Sequence: 1, Body: "campo", Capturing: true, Hints: []interactive.Hint{{Key: "tab", Label: "completar"}},
	}})
	model, wait := open(t, &fakeOpener{session: session})
	next, _ := model.Update(wait())
	model = next.(*plugin.Model)
	if !model.Capturing() {
		t.Fatal("snapshot capturando não chegou ao chrome")
	}
	hasEsc := false
	for _, hint := range model.Hints() {
		hasEsc = hasEsc || strings.Contains(hint.Key, "esc")
	}
	if !hasEsc {
		t.Fatal("PluginScreen não adicionou esc")
	}
	next, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	model = next.(*plugin.Model)
	if command == nil {
		t.Fatal("q capturado não foi enviado à tool")
	}
	_ = command()
	session.mu.Lock()
	defer session.mu.Unlock()
	if len(session.events) == 0 || session.events[len(session.events)-1].Key.Text != "q" {
		t.Fatalf("eventos = %+v", session.events)
	}
}

func TestConfirmacaoGlobalRespondeSemEntregarTeclaATool(t *testing.T) {
	session := newSession(interactive.Update{State: interactive.StateRunning, HostRequest: &interactive.HostRequest{
		ID: "confirm-1", Method: interactive.CapabilityConfirmationRequest,
		Params: []byte(`{"title":"Apagar?","message":"Confirme a operação"}`),
	}})
	model, wait := open(t, &fakeOpener{session: session})
	next, _ := model.Update(wait())
	model = next.(*plugin.Model)
	if !model.Capturing() || !strings.Contains(model.View(tui.Frame{Width: 60, Height: 12}), "Confirme") {
		t.Fatal("modal global não abriu")
	}
	next, responseCmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	model = next.(*plugin.Model)
	if responseCmd == nil {
		t.Fatal("confirmação não produziu resposta")
	}
	_ = responseCmd()
	session.mu.Lock()
	defer session.mu.Unlock()
	if len(session.responses) != 1 || !strings.Contains(string(session.responses[0].Result), "true") {
		t.Fatalf("respostas = %+v", session.responses)
	}
	if len(session.events) != 0 {
		t.Fatalf("tecla do modal vazou para tool: %+v", session.events)
	}
}

func TestCloseEncerraSessao(t *testing.T) {
	session := newSession()
	model, _ := open(t, &fakeOpener{session: session})
	if message := model.Close()(); message != nil {
		t.Fatalf("Close devolveu mensagem %T", message)
	}
	session.mu.Lock()
	defer session.mu.Unlock()
	if !session.shutdown {
		t.Fatal("sessão não recebeu shutdown")
	}
}

func TestReiniciaDepoisDeFalhaDeInicializacao(t *testing.T) {
	opener := &fakeOpener{err: errors.New("crash")}
	model, _ := open(t, opener)
	opener.mu.Lock()
	opener.err = nil
	opener.session = newSession()
	opener.mu.Unlock()
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if command == nil {
		t.Fatal("r não reiniciou")
	}
	message := command()
	_, _ = model.Update(message)
	opener.mu.Lock()
	defer opener.mu.Unlock()
	if opener.calls != 2 {
		t.Fatalf("Open chamado %d vezes", opener.calls)
	}
}
