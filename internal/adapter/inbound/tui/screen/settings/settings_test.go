package settings

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	core "github.com/mateuslh/lealing/internal/core/settings"
)

type fakeManager struct {
	values  []core.Value
	info    []core.InfoRow
	listErr error
	setErr  error

	setKey   core.Key
	setValue string
	resetKey core.Key
}

func (f *fakeManager) All() ([]core.Value, error) { return f.values, f.listErr }
func (f *fakeManager) Get(key core.Key) (core.Value, error) {
	for _, value := range f.values {
		if value.Key == key {
			return value, nil
		}
	}
	return core.Value{}, core.ErrUnknownField
}
func (f *fakeManager) Set(key core.Key, value string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.setKey, f.setValue = key, value
	return nil
}
func (f *fakeManager) Reset(key core.Key) error { f.resetKey = key; return nil }
func (f *fakeManager) Info() []core.InfoRow     { return f.info }

func deps() tui.Deps { return tui.Deps{Theme: theme.Default()} }

// values monta o retrato que a tela desenha, um campo por seção relevante.
func values() []core.Value {
	fields := core.Fields()
	out := make([]core.Value, 0, len(fields))
	for _, field := range fields {
		value := core.Value{Field: field, Current: field.Default, Source: core.SourceDefault}
		switch field.Key {
		case core.KeyGitHubClientID:
			value.Current, value.Source = "Ov23liDoBuild", core.SourceDefault
		case core.KeyGreetingName:
			value.Current, value.Source = "Chefia", core.SourceUser
		}
		out = append(out, value)
	}
	return out
}

func loaded(t *testing.T, manager *fakeManager) *Model {
	t.Helper()
	model := New(deps(), manager)
	next, _ := model.Update(model.reload()())
	return next.(*Model)
}

func TestDesenhaSecoesECamposDeclarados(t *testing.T) {
	manager := &fakeManager{values: values(), info: []core.InfoRow{
		{Section: core.SectionEnvironment.ID, Label: "configuração", Value: "/tmp/x"},
	}}
	view := loaded(t, manager).View(tui.Frame{Width: 120, Height: 28})

	for _, section := range core.Sections() {
		if !strings.Contains(view, section.Name) {
			t.Errorf("seção %q ausente:\n%s", section.Name, view)
		}
	}
	if !strings.Contains(view, "Client ID do OAuth App") {
		t.Errorf("o campo da seção inicial não apareceu:\n%s", view)
	}
}

func TestSelecionadoMostraDescricaoEOrigem(t *testing.T) {
	model := loaded(t, &fakeManager{values: values()})
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyRight})

	view := model.View(tui.Frame{Width: 120, Height: 28})
	if !strings.Contains(view, "device flow") {
		t.Errorf("a descrição do campo selecionado não apareceu:\n%s", view)
	}
	if !strings.Contains(view, "padrão") {
		t.Errorf("a origem do valor não apareceu:\n%s", view)
	}
}

func TestEditarTextoGravaOValor(t *testing.T) {
	manager := &fakeManager{values: values()}
	model := loaded(t, manager)

	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyRight})
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if !model.editing || !model.Capturing() {
		t.Fatal("Enter não abriu a edição")
	}
	// Substitui o conteúdo por um valor novo.
	model.input.SetValue("Ov23liNovo")

	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("Enter não gravou")
	}
	command()
	if manager.setKey != core.KeyGitHubClientID || manager.setValue != "Ov23liNovo" {
		t.Fatalf("gravou %q = %q", manager.setKey, manager.setValue)
	}
}

func TestEscCancelaAEdicaoSemGravar(t *testing.T) {
	manager := &fakeManager{values: values()}
	model := loaded(t, manager)

	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyRight})
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	model.input.SetValue("descartado")
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyEsc})

	if model.editing {
		t.Fatal("esc não fechou a edição")
	}
	if manager.setKey != "" {
		t.Fatalf("esc gravou %q", manager.setKey)
	}
}

func TestInterruptorAlternaComEnter(t *testing.T) {
	manager := &fakeManager{values: values()}
	model := loaded(t, manager)

	// Segunda seção, segundo campo: o interruptor da vitrine.
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyDown})
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyRight})
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyDown})

	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("Enter não alternou o interruptor")
	}
	command()
	if manager.setKey != core.KeyMarketplaceOnHome || manager.setValue != "false" {
		t.Fatalf("alternou %q = %q", manager.setKey, manager.setValue)
	}
}

// Reset só faz sentido no que o usuário mudou; no que está no padrão, a tecla
// não pode fingir que fez algo.
func TestResetSoAgeSobreValorDoUsuario(t *testing.T) {
	manager := &fakeManager{values: values()}
	model := loaded(t, manager)

	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyRight})
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if manager.resetKey != "" {
		t.Fatalf("reset agiu sobre um valor no padrão: %q", manager.resetKey)
	}

	// Aparência: o campo com valor definido pelo usuário.
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyLeft})
	for range 2 {
		model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyDown})
	}
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyRight})
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if command == nil {
		t.Fatal("reset não gerou comando no valor do usuário")
	}
	command()
	if manager.resetKey != core.KeyGreetingName {
		t.Fatalf("reset = %q", manager.resetKey)
	}
}

func TestValorRecusadoApareceSemSerGravado(t *testing.T) {
	manager := &fakeManager{values: values(), setErr: errors.New("informe uma URL HTTPS completa")}
	model := loaded(t, manager)

	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyRight})
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	model.input.SetValue("http://inseguro")
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model, _ = update(t, model, command())

	if model.err == nil {
		t.Fatal("a recusa não chegou à tela")
	}
	if !strings.Contains(model.View(tui.Frame{Width: 110, Height: 26}), "HTTPS") {
		t.Fatal("a mensagem da recusa não foi renderizada")
	}
}

// Um ajuste que só vale ao reabrir precisa dizer isso depois de salvo.
func TestAjusteQueExigeReinicioAvisa(t *testing.T) {
	manager := &fakeManager{values: values()}
	model := loaded(t, manager)

	model, _ = update(t, model, savedMsg{key: core.KeyMarketplaceIndex, message: "ajuste salvo", restart: true})
	if status, _ := model.Status(); !strings.Contains(status, "reabrir") {
		t.Fatalf("status = %q", status)
	}
}

func TestSemGerenciadorATelaExplica(t *testing.T) {
	model := New(deps(), nil)
	next, _ := model.Update(model.reload()())
	view := next.View(tui.Frame{Width: 90, Height: 20})
	if !strings.Contains(view, "indisponível") {
		t.Fatalf("a tela não explicou a ausência:\n%s", view)
	}
}

func TestHintsIncluemEsc(t *testing.T) {
	model := loaded(t, &fakeManager{values: values()})
	for _, hint := range model.Hints() {
		if strings.Contains(hint.Key, "esc") {
			return
		}
	}
	t.Fatal("hint esc ausente")
}

func TestAcaoAdministrativaAbreFluxoDaSecao(t *testing.T) {
	target := func() tui.Screen { return stubScreen{} }
	model := New(deps(), &fakeManager{values: values()}, Action{
		Section: core.SectionAccount.ID, Label: "Sincronização", Screen: target,
	})
	next, _ := model.Update(model.reload()())
	model = next.(*Model)
	model, _ = update(t, model, tea.KeyMsg{Type: tea.KeyRight})
	// A ação vem antes dos campos na lista: já é o item selecionado ao entrar.
	_, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		t.Fatal("ação não navegou")
	}
	navigation, ok := command().(tui.NavigateMsg)
	if !ok || navigation.Screen.ID() != "stub-settings-action" {
		t.Fatalf("navegação = %#v", navigation)
	}
}

// TestCapacidadeAparecePrimeiroEComDescricaoSempreVisivel garante que a
// promessa da tela — capacidades antes de ajustes, contexto sem precisar
// selecionar — continua valendo depois de qualquer mudança de layout.
func TestCapacidadeAparecePrimeiroEComDescricaoSempreVisivel(t *testing.T) {
	model := New(deps(), &fakeManager{values: values()}, Action{
		Section: core.SectionAccount.ID, Glyph: "⇄", Label: "Sincronização",
		Description: "descrição sempre visível", Value: "gerenciar",
	})
	next, _ := model.Update(model.reload()())
	model = next.(*Model)

	entries := model.sectionEntries()
	if len(entries) == 0 || !entries[0].isAction {
		t.Fatalf("a capacidade não veio primeiro: %+v", entries)
	}

	view := model.View(tui.Frame{Width: 120, Height: 28})
	if !strings.Contains(view, "descrição sempre visível") {
		t.Fatalf("a descrição da capacidade não apareceu sem seleção:\n%s", view)
	}
	if !strings.Contains(view, "⇄") {
		t.Fatalf("o glifo da capacidade não apareceu:\n%s", view)
	}
}

type stubScreen struct{}

func (stubScreen) ID() tui.ScreenID                       { return "stub-settings-action" }
func (stubScreen) Title() string                          { return "stub" }
func (stubScreen) Init() tea.Cmd                          { return nil }
func (s stubScreen) Update(tea.Msg) (tui.Screen, tea.Cmd) { return s, nil }
func (stubScreen) View(tui.Frame) string                  { return "" }
func (stubScreen) Hints() []tui.Hint                      { return nil }

func update(t *testing.T, model *Model, message tea.Msg) (*Model, tea.Cmd) {
	t.Helper()
	next, command := model.Update(message)
	updated, ok := next.(*Model)
	if !ok {
		t.Fatalf("model = %T", next)
	}
	return updated, command
}
