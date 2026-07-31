package home

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	"github.com/mateuslh/lealing/internal/adapter/outbound/persistence"
	"github.com/mateuslh/lealing/internal/adapter/outbound/registry"
	"github.com/mateuslh/lealing/internal/adapter/outbound/search"
	"github.com/mateuslh/lealing/internal/catalog"
	"github.com/mateuslh/lealing/internal/core/port"
	"github.com/mateuslh/lealing/internal/core/service"
)

// Estes testes montam o caminho real — registry, buscador fuzzy e serviço —
// e só trocam a persistência por memória. É a integração que interessa: os
// bugs de layout aparecem com o catálogo cheio, não com três tools de
// mentira.

func newTestModel(t *testing.T) *Model {
	t.Helper()

	clock := port.ClockFunc(func() time.Time {
		return time.Date(2026, 7, 30, 15, 0, 0, 0, time.UTC)
	})

	repo := registry.New(catalog.Providers(), registry.WithStrict(true))
	svc := service.NewCatalog(repo, search.NewFuzzy(clock, nil), persistence.NewMemoryUsage(), clock)

	m := New(Config{
		Deps:    tui.Deps{Theme: theme.Default()},
		Catalog: svc,
		Prefs:   svc,
		Clock:   clock,
	})

	// Executa a carga inicial de forma síncrona.
	next, _ := m.Update(m.loadCatalog()())
	m = next.(*Model)

	if m.err != nil {
		t.Fatalf("carga do catálogo falhou: %v", m.err)
	}
	if m.highlights.TotalTools == 0 {
		t.Fatal("catálogo embutido veio vazio")
	}
	return m
}

// press envia uma tecla e devolve o comando resultante.
func press(t *testing.T, m *Model, key string) (*Model, tea.Cmd) {
	t.Helper()
	var msg tea.KeyMsg
	switch key {
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "tab":
		msg = tea.KeyMsg{Type: tea.KeyTab}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	next, cmd := m.Update(msg)
	return next.(*Model), cmd
}

// cmdTimeout descarta comandos temporizados. Cada tecla na busca devolve
// Batch(textinput.Blink, runQuery) e o Blink só responde após o intervalo de
// piscada — esperá-lo somaria meio segundo por caractere digitado.
const cmdTimeout = 100 * time.Millisecond

// apply executa um comando e alimenta o model com o que ele produziu.
//
// Desdobra tea.BatchMsg: entregar o Batch cru ao Update descartaria
// silenciosamente a consulta que vem dentro dele.
func apply(t *testing.T, m *Model, cmd tea.Cmd) *Model {
	t.Helper()
	if cmd == nil {
		return m
	}

	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()

	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(cmdTimeout):
		return m
	}

	switch v := msg.(type) {
	case nil:
		return m
	case tea.BatchMsg:
		for _, c := range v {
			m = apply(t, m, c)
		}
		return m
	default:
		// Não segue comandos encadeados: tick() se reagendaria para sempre.
		next, _ := m.Update(msg)
		return next.(*Model)
	}
}

// typeText digita caractere a caractere, drenando a busca a cada tecla —
// que é exatamente o que acontece na interação real.
func typeText(t *testing.T, m *Model, text string) *Model {
	t.Helper()
	for _, r := range text {
		var cmd tea.Cmd
		m, cmd = press(t, m, string(r))
		m = apply(t, m, cmd)
	}
	return m
}

func TestCatalogoEmbutidoValida(t *testing.T) {
	// registry.WithStrict(true) rejeita tool inválida, ID duplicado e
	// categoria não declarada. newTestModel falha se algo disso ocorrer.
	m := newTestModel(t)

	// Toda tool declarada precisa ter uma tela ou um runner; a contagem
	// aqui trava o acervo contra remoção acidental.
	if m.highlights.TotalTools != 5 {
		t.Errorf("tools = %d, quero 5", m.highlights.TotalTools)
	}
	if m.highlights.TotalCategories != 3 {
		t.Errorf("categorias povoadas = %d, quero 3", m.highlights.TotalCategories)
	}
}

func TestBuscaFiltraEExecutaAtalhos(t *testing.T) {
	m := newTestModel(t)

	m, _ = press(t, m, "/")
	if !m.searching {
		t.Fatal("“/” não abriu a busca")
	}
	if !m.Capturing() {
		t.Fatal("Capturing = false durante a busca; “q” fecharia o programa")
	}

	// "energia" casa pelo nome; "pmset" só casa pelas keywords, que é o
	// caminho que a busca precisa cobrir.
	m = typeText(t, m, "pmset")
	if m.results.Total == 0 {
		t.Fatal("busca por “pmset” não achou nada")
	}
	if got := m.results.Items[0].Tool.ID; got != "power-control" {
		t.Errorf("primeiro resultado = %s, quero power-control", got)
	}

	m, _ = press(t, m, "esc")
	if m.searching || m.input.Value() != "" {
		t.Error("esc não saiu da busca nem limpou o campo")
	}
}

func TestBuscaAceitaFiltroInline(t *testing.T) {
	m := newTestModel(t)
	m, _ = press(t, m, "/")
	m = typeText(t, m, "cat:ai")

	// A categoria de IA tem o uso de tokens e a troca de contas; o filtro
	// precisa trazer as duas e nada de outra categoria.
	if m.results.Total != 2 {
		t.Fatalf("filtro cat:ai devolveu %d, quero 2", m.results.Total)
	}
	got := map[string]bool{}
	for _, item := range m.results.Items {
		got[string(item.Tool.ID)] = true
	}
	for _, want := range []string{"token-usage", "claude-accounts"} {
		if !got[want] {
			t.Errorf("cat:ai não trouxe %s (veio %v)", want, got)
		}
	}
}

func TestBuscaDescartaRespostaObsoleta(t *testing.T) {
	m := newTestModel(t)
	m, _ = press(t, m, "/")

	// Guarda o comando da primeira tecla e só o entrega depois de mais
	// teclas: simula a resposta lenta de uma busca já superada.
	m, stale := press(t, m, "g")
	m = typeText(t, m, "it")
	after := m.results.Total

	m = apply(t, m, stale)
	if m.results.Total != after {
		t.Errorf("resposta obsoleta sobrescreveu os resultados: %d → %d", after, m.results.Total)
	}
}

func TestNavegacaoEntrePaineis(t *testing.T) {
	m := newTestModel(t)
	start := m.focus

	m, _ = press(t, m, "tab")
	if m.focus == start {
		t.Error("tab não trocou o painel focado")
	}

	// O cursor de cada zona é independente: mover em uma não mexe na outra.
	m.focus = zoneSuggested
	m, _ = press(t, m, "down")
	if m.cursor[zoneSuggested] != 1 {
		t.Errorf("cursor de sugeridas = %d, quero 1", m.cursor[zoneSuggested])
	}
	if m.cursor[zoneFavorites] != 0 {
		t.Errorf("cursor de favoritas mudou junto: %d", m.cursor[zoneFavorites])
	}
}

func TestCursorNaoEstouraEmPainelVazio(t *testing.T) {
	m := newTestModel(t)
	m.focus = zoneFavorites // vazio na primeira execução

	for range 5 {
		m, _ = press(t, m, "down")
	}
	if m.cursor[zoneFavorites] != 0 {
		t.Errorf("cursor = %d em painel vazio, quero 0", m.cursor[zoneFavorites])
	}
	if _, ok := m.selectedTool(); ok {
		t.Error("selectedTool devolveu tool em painel vazio")
	}
}

func TestViewNuncaEstouraOFrame(t *testing.T) {
	sizes := []tui.Frame{
		{Width: 200, Height: 60},
		{Width: 150, Height: 44},
		{Width: 120, Height: 36},
		{Width: 100, Height: 30},
		{Width: 94, Height: 26},
		{Width: 80, Height: 24},
		{Width: 60, Height: 20},
		{Width: 40, Height: 14},
		{Width: 30, Height: 10},
		{Width: 25, Height: 7},
	}

	modes := map[string]func(*testing.T, *Model) *Model{
		"navegação": func(_ *testing.T, m *Model) *Model { return m },
		"busca": func(t *testing.T, m *Model) *Model {
			m, _ = press(t, m, "/")
			return typeText(t, m, "sistema")
		},
	}

	for name, setup := range modes {
		for _, f := range sizes {
			t.Run(name, func(t *testing.T) {
				m := setup(t, newTestModel(t))
				next, _ := m.Update(tea.WindowSizeMsg{Width: f.Width, Height: f.Height})
				m = next.(*Model)

				out := m.View(f)
				lines := strings.Split(out, "\n")

				if len(lines) > f.Height {
					t.Errorf("%dx%d [%s]: %d linhas excedem a altura",
						f.Width, f.Height, name, len(lines))
				}
				for i, line := range lines {
					if got := lipgloss.Width(line); got > f.Width {
						t.Errorf("%dx%d [%s]: linha %d tem %d colunas",
							f.Width, f.Height, name, i, got)
					}
				}
			})
		}
	}
}

func TestFavoritarRecarregaOsDestaques(t *testing.T) {
	m := newTestModel(t)
	m.focus = zoneSuggested

	tool, ok := m.selectedTool()
	if !ok {
		t.Fatal("nenhuma tool selecionada em sugeridas")
	}

	_, cmd := press(t, m, "f")
	if cmd == nil {
		t.Fatal("“f” não emitiu comando")
	}
	msg, isFav := cmd().(favoriteMsg)
	if !isFav {
		t.Fatalf("comando devolveu %T, quero favoriteMsg", cmd())
	}
	if msg.err != nil {
		t.Fatalf("favoritar falhou: %v", msg.err)
	}
	if msg.id != tool.ID || !msg.on {
		t.Errorf("favoriteMsg = {%s, %v}, quero {%s, true}", msg.id, msg.on, tool.ID)
	}

	// A confirmação precisa disparar a recarga, senão o painel de favoritas
	// continua mostrando o estado anterior.
	next, reload := m.Update(msg)
	m = next.(*Model)
	if reload == nil {
		t.Error("favoriteMsg não disparou recarga dos destaques")
	}
	if m.toast.text == "" {
		t.Error("favoritar não publicou mensagem de status")
	}
}

func TestSaudacaoSegueAHora(t *testing.T) {
	base := time.Date(2026, 7, 30, 0, 0, 0, 0, time.UTC)
	tests := map[int]string{3: "Boa madrugada", 9: "Bom dia", 15: "Boa tarde", 21: "Boa noite"}

	for hour, want := range tests {
		got := greeting(base.Add(time.Duration(hour)*time.Hour), "mateuslh")
		if !strings.HasPrefix(got, want) {
			t.Errorf("às %dh a saudação foi %q, quero prefixo %q", hour, got, want)
		}
		if !strings.HasSuffix(got, "mateuslh") {
			t.Errorf("saudação %q não inclui o usuário", got)
		}
	}
}
