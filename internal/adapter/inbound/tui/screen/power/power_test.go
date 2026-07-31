package power

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	corepower "github.com/mateuslh/lealing/internal/core/power"
)

// loaded monta a tela já com configurações lidas, sem passar pelo pmset.
func loaded() *Model {
	settings := corepower.Settings{
		Battery: corepower.Profile{Sleep: 10, DisplaySleep: 10, HibernateMode: 3},
		AC:      corepower.Profile{Sleep: 30, DisplaySleep: 30, HibernateMode: 3},
	}
	return &Model{
		deps: tui.Deps{Theme: theme.Default()},
		// O painel completo é o caso do macOS; a cobertura de uma plataforma
		// parcial fica em TestPlataformaParcialEscondeCampos.
		features: corepower.AllFeatures,
		current:  settings,
		saved:    settings,
	}
}

func press(m *Model, keys ...tea.KeyType) *Model {
	for _, k := range keys {
		next, _ := m.Update(tea.KeyMsg{Type: k})
		m = next.(*Model)
	}
	return m
}

// O gesto principal da tela é mudar um valor, e ele fica nas setas puras: é o
// que o usuário alcança sem tirar a mão do lugar.
func TestSetasAjustamOCampoFocado(t *testing.T) {
	m := press(loaded(), tea.KeyRight)
	if got := m.current.Battery.Sleep; got != 15 {
		t.Errorf("→ levou Dormir para %d, queria o degrau seguinte (15)", got)
	}

	m = press(m, tea.KeyLeft, tea.KeyLeft)
	if got := m.current.Battery.Sleep; got != 5 {
		t.Errorf("←← levou Dormir para %d, queria dois degraus abaixo (5)", got)
	}
}

func TestSetasVerticaisAndamEntreCampos(t *testing.T) {
	m := press(loaded(), tea.KeyDown, tea.KeyRight)

	if m.current.Battery.Sleep != 10 {
		t.Error("o ajuste caiu no campo anterior: o cursor não desceu")
	}
	if got := m.current.Battery.DisplaySleep; got != 15 {
		t.Errorf("Desligar a tela = %d, queria 15", got)
	}
}

func TestShiftTrocaAFonteDeAlimentacao(t *testing.T) {
	m := press(loaded(), tea.KeyShiftRight, tea.KeyRight)

	if m.source != corepower.AC {
		t.Fatalf("shift+→ deveria focar o carregador, ficou em %v", m.source)
	}
	if m.current.Battery.Sleep != 10 {
		t.Error("o ajuste vazou para o perfil da bateria")
	}
	if got := m.current.AC.Sleep; got != 45 {
		t.Errorf("Dormir no carregador = %d, queria 45", got)
	}

	m = press(m, tea.KeyShiftLeft)
	if m.source != corepower.Battery {
		t.Errorf("shift+← deveria voltar para a bateria, ficou em %v", m.source)
	}
}

// O tab continua alternando, e a coluna do carregador tem um campo a menos:
// sem o clamp, o cursor herdaria um índice fora da lista.
func TestTabAlternaSemDeixarOCursorForaDaLista(t *testing.T) {
	m := loaded()
	for range len(m.fields(corepower.Battery)) {
		m = press(m, tea.KeyDown)
	}
	last := m.cursor[corepower.Battery]

	m = press(m, tea.KeyTab)
	if m.source != corepower.AC {
		t.Fatal("tab não trocou de fonte")
	}
	if n := len(m.fields(corepower.AC)); m.cursor[corepower.AC] >= n {
		t.Errorf("cursor em %d com %d campos visíveis", m.cursor[corepower.AC], n)
	}
	if last >= len(m.fields(corepower.Battery)) {
		t.Errorf("cursor da bateria em %d, além do último campo", last)
	}
}

// Uma plataforma que só grava os três tempos de inatividade — o caso do
// powercfg — não pode mostrar interruptores que nunca chegariam ao sistema,
// nem ficar com um diff pendente que aplicar não fecha.
func TestPlataformaParcialEscondeCampos(t *testing.T) {
	m := loaded()
	m.features = corepower.FeatureSleep | corepower.FeatureDisplaySleep | corepower.FeatureDiskSleep

	visible := m.fields(corepower.Battery)
	if len(visible) != 3 {
		t.Fatalf("%d campos visíveis, queria 3", len(visible))
	}
	for _, f := range visible {
		if f.Control == corepower.ControlToggle {
			t.Errorf("campo %q apareceu sem a plataforma suportá-lo", f.Label)
		}
	}
	if m.supportsPasswordless() {
		t.Error("a dispensa de senha foi oferecida sem a plataforma suportá-la")
	}
	for _, h := range m.Hints() {
		if h.Key == "p" {
			t.Error("o atalho da dispensa de senha continuou anunciado")
		}
	}

	// O preset mexe em Power Nap e companhia; nada disso pode virar diff.
	m.loadPreset(corepower.PresetNeverSleep(), "teste")
	if m.current.Battery.TCPKeepAlive {
		t.Error("o preset escreveu um campo que a plataforma não grava")
	}
	m.saved = m.current
	m.loadPreset(corepower.PresetNeverSleep(), "teste")
	if m.HasUnsavedChanges() {
		t.Error("repetir o preset marcou alteração pendente em campo invisível")
	}
}

func TestEspacoAlternaInterruptor(t *testing.T) {
	m := loaded()
	// Power Nap é o quarto campo da lista.
	m = press(m, tea.KeyDown, tea.KeyDown, tea.KeyDown, tea.KeySpace)

	if !m.current.Battery.PowerNap {
		t.Error("espaço não ligou o Power Nap")
	}
	if !m.HasUnsavedChanges() {
		t.Error("alternar um interruptor não marcou alteração pendente")
	}
}
