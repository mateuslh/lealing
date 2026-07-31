package ccaccount_test

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	screen "github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/ccaccount"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	core "github.com/mateuslh/lealing/internal/core/ccaccount"
)

func now() time.Time { return time.Date(2026, 7, 30, 15, 4, 5, 0, time.UTC) }

// spy registra o que a tela pediu, que é o que estes testes verificam: a
// tela não decide nada sozinha, ela traduz teclas em chamadas.
type spy struct {
	state core.State
	// saveErr é devolvido na primeira chamada de Save, para exercitar o
	// caminho de confirmação.
	saveErr    error
	saved      []string
	overwrote  []string
	activated  []string
	forced     []string
	forgotten  []string
	activateEr error
}

func (s *spy) State(context.Context) (core.State, error) { return s.state, nil }

func (s *spy) Save(_ context.Context, name string) (core.Profile, error) {
	s.saved = append(s.saved, name)
	if err := s.saveErr; err != nil {
		s.saveErr = nil
		return core.Profile{}, err
	}
	return core.Profile{Name: name}, nil
}

func (s *spy) SaveOverwriting(_ context.Context, name string) (core.Profile, error) {
	s.overwrote = append(s.overwrote, name)
	return core.Profile{Name: name}, nil
}

func (s *spy) Activate(_ context.Context, name string) error {
	s.activated = append(s.activated, name)
	if err := s.activateEr; err != nil {
		s.activateEr = nil
		return err
	}
	return nil
}

func (s *spy) ActivateOverwriting(_ context.Context, name string) error {
	s.forced = append(s.forced, name)
	return nil
}

func (s *spy) Forget(_ context.Context, name string) error {
	s.forgotten = append(s.forgotten, name)
	return nil
}

func fixture() core.State {
	pessoal := core.Identity{Email: "eu@exemplo.com", AccountUUID: "uuid-1", Plan: "pro",
		ExpiresAt: now().Add(2 * time.Hour), RenewsUntil: now().AddDate(0, 0, 20)}
	trabalho := core.Identity{Email: "eu@empresa.com", AccountUUID: "uuid-2", Plan: "max",
		ExpiresAt: now().AddDate(0, 0, -2), RenewsUntil: now().AddDate(0, 0, -1)}
	return core.State{
		Active: pessoal, HasActive: true, ActiveProfile: "pessoal", Origin: "chaveiro do macOS",
		Profiles: []core.Profile{
			{Name: "pessoal", Identity: pessoal, SavedAt: now().AddDate(0, 0, -3)},
			{Name: "trabalho", Identity: trabalho, SavedAt: now().AddDate(0, 0, -8)},
		},
	}
}

// open monta a tela com o estado já carregado.
func open(t *testing.T, s *spy) tui.Screen {
	t.Helper()
	m := screen.New(tui.Deps{Theme: theme.Default()}, s, now)
	var sc tui.Screen = m
	if cmd := sc.Init(); cmd != nil {
		sc, _ = sc.Update(cmd())
	}
	sc, _ = sc.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	return sc
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

// press aplica as teclas, entregando à tela a mensagem de cada comando —
// que é o que o loop do Bubble Tea faria.
func press(t *testing.T, s tui.Screen, keys ...string) tui.Screen {
	t.Helper()
	for _, k := range keys {
		var cmd tea.Cmd
		s, cmd = s.Update(key(k))
		for cmd != nil {
			msg := cmd()
			if msg == nil {
				break
			}
			s, cmd = s.Update(msg)
		}
	}
	return s
}

func TestListaMostraOsPerfisEMarcaOAtivo(t *testing.T) {
	s := open(t, &spy{state: fixture()})
	out := stripANSI(s.View(tui.Frame{Width: 100, Height: 30}))

	for _, want := range []string{"pessoal", "trabalho", "eu@exemplo.com", "eu@empresa.com", "chaveiro do macOS"} {
		if !strings.Contains(out, want) {
			t.Errorf("a tela não mostra %q:\n%s", want, out)
		}
	}
	if !strings.Contains(out, "● pessoal") {
		t.Errorf("o perfil ativo não está marcado:\n%s", out)
	}
	if !strings.Contains(out, "○ trabalho") {
		t.Errorf("o perfil inativo não está marcado:\n%s", out)
	}
}

func TestEnterAtivaOPerfilSobOCursor(t *testing.T) {
	sp := &spy{state: fixture()}
	press(t, open(t, sp), "down", "enter")

	if len(sp.activated) != 1 || sp.activated[0] != "trabalho" {
		t.Fatalf("ativou %v, queria [trabalho]", sp.activated)
	}
}

func TestEnterNoPerfilAtivoNaoRefazATroca(t *testing.T) {
	sp := &spy{state: fixture()}
	press(t, open(t, sp), "enter")

	if len(sp.activated) != 0 {
		t.Errorf("reativou a conta que já está em uso: %v", sp.activated)
	}
}

func TestSalvarPedeONomeESugereOAtual(t *testing.T) {
	sp := &spy{state: fixture()}
	s := press(t, open(t, sp), "s")

	capturer, ok := s.(interface{ Capturing() bool })
	if !ok || !capturer.Capturing() {
		t.Fatal("com o campo aberto, a tela precisa capturar o teclado — senão “q” fecha o programa")
	}

	out := stripANSI(s.View(tui.Frame{Width: 100, Height: 30}))
	if !strings.Contains(out, "nome do perfil") || !strings.Contains(out, "pessoal") {
		t.Errorf("o campo não veio preenchido com a sugestão:\n%s", out)
	}

	s = press(t, s, "backspace", "backspace", "enter")
	if len(sp.saved) != 1 || sp.saved[0] != "pesso" {
		t.Errorf("salvou %v, queria [pesso]", sp.saved)
	}
}

func TestNomeRecusaCaractereQueOCofreNaoAceita(t *testing.T) {
	sp := &spy{state: fixture()}
	press(t, open(t, sp), "s", "/", ":", "-", "2", "enter")

	if len(sp.saved) != 1 || sp.saved[0] != "pessoal-2" {
		t.Errorf("salvou %v, queria [pessoal-2] — barra e dois-pontos deviam ser ignorados", sp.saved)
	}
}

func TestConfirmacaoAntesDeRemover(t *testing.T) {
	sp := &spy{state: fixture()}
	s := press(t, open(t, sp), "down", "d")

	out := stripANSI(s.View(tui.Frame{Width: 100, Height: 30}))
	if !strings.Contains(out, "remover o perfil") {
		t.Fatalf("a tela removeu sem perguntar:\n%s", out)
	}
	if len(sp.forgotten) != 0 {
		t.Fatalf("removeu antes da resposta: %v", sp.forgotten)
	}

	s = press(t, s, "n")
	if len(sp.forgotten) != 0 {
		t.Fatalf("“n” removeu assim mesmo: %v", sp.forgotten)
	}

	press(t, s, "d", "s")
	if len(sp.forgotten) != 1 || sp.forgotten[0] != "trabalho" {
		t.Errorf("removeu %v, queria [trabalho]", sp.forgotten)
	}
}

func TestSessaoNaoGuardadaViraPerguntaAntesDaTroca(t *testing.T) {
	sp := &spy{state: fixture(), activateEr: core.ErrActiveUnsaved}
	s := press(t, open(t, sp), "down", "enter")

	out := stripANSI(s.View(tui.Frame{Width: 100, Height: 30}))
	if !strings.Contains(out, "será perdida") {
		t.Fatalf("o aviso de perda não apareceu:\n%s", out)
	}
	if len(sp.forced) != 0 {
		t.Fatalf("trocou antes de confirmar: %v", sp.forced)
	}

	press(t, s, "s")
	if len(sp.forced) != 1 || sp.forced[0] != "trabalho" {
		t.Errorf("forçou %v, queria [trabalho]", sp.forced)
	}
}

func TestNomeExistenteDeOutraContaViraPergunta(t *testing.T) {
	sp := &spy{state: fixture(), saveErr: core.ErrProfileExists}
	s := press(t, open(t, sp), "s", "enter")

	out := stripANSI(s.View(tui.Frame{Width: 100, Height: 30}))
	if !strings.Contains(out, "Substituir?") {
		t.Fatalf("substituiu sem perguntar:\n%s", out)
	}

	press(t, s, "s")
	if len(sp.overwrote) != 1 || sp.overwrote[0] != "pessoal" {
		t.Errorf("substituiu %v, queria [pessoal]", sp.overwrote)
	}
}

func TestSemSessaoAtivaNaoDeixaSalvar(t *testing.T) {
	sp := &spy{state: core.State{Origin: "chaveiro do macOS"}}
	s := press(t, open(t, sp), "s")

	if len(sp.saved) != 0 {
		t.Fatalf("salvou o nada: %v", sp.saved)
	}
	out := stripANSI(s.View(tui.Frame{Width: 100, Height: 30}))
	if !strings.Contains(out, "rode `claude`") {
		t.Errorf("a tela não diz como entrar:\n%s", out)
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
