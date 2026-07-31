package repoclone

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	core "github.com/mateuslh/lealing/internal/core/repoclone"
)

type managerFake struct {
	resolved core.Repository
	cloned   core.Plan
}

func (f *managerFake) Discover(context.Context, string) (core.Plan, error) {
	return testPlan(), nil
}

func (f *managerFake) Resolve(context.Context, core.Source, string) (core.Repository, error) {
	return f.resolved, nil
}

func (f *managerFake) Clone(_ context.Context, plan core.Plan) (core.Result, error) {
	f.cloned = plan
	return core.Result{}, nil
}

func testPlan() core.Plan {
	return core.Plan{
		Source:      core.Source{Owner: "bradesco", Repository: "pix", Prefix: "pix"},
		Destination: "/Users/teste/dev/pix",
		Repositories: []core.Repository{
			{
				Owner: "bradesco", Name: "pix",
				Description: "API principal de pagamentos instantâneos.",
				Visibility:  "PRIVATE", Language: "Java", DefaultBranch: "main",
				UpdatedAt:   time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC),
				DiskUsageKB: 2048,
			},
			{Owner: "bradesco", Name: "pix-config", Visibility: "INTERNAL"},
			{Owner: "bradesco", Name: "pix-worker", Visibility: "PUBLIC"},
		},
	}
}

func reviewModel(fake *managerFake) *Model {
	m := New(tui.Deps{Theme: theme.Default()}, fake)
	next, _ := m.Update(discoveredMsg{plan: testPlan()})
	return next.(*Model)
}

func TestRevisaoIncluiRemoveEAdiciona(t *testing.T) {
	fake := &managerFake{resolved: core.Repository{
		Owner: "bradesco", Name: "pix-extra", Visibility: "PRIVATE",
	}}
	m := reviewModel(fake)

	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}})
	if m.included[1] {
		t.Fatal("espaço não excluiu pix-config")
	}

	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if len(m.plan.Repositories) != 2 || strings.Contains(m.feedback, "GitHub") == false {
		t.Fatalf("remoção = %d repos, feedback %q", len(m.plan.Repositories), m.feedback)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	for _, r := range "pix-extra" {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("adicionar não consultou o manager")
	}
	m.Update(cmd())
	if got := m.plan.Repositories[len(m.plan.Repositories)-1].Name; got != "pix-extra" {
		t.Fatalf("último repo = %q", got)
	}
}

func TestCloneRecebeSomenteSelecionados(t *testing.T) {
	fake := &managerFake{}
	m := reviewModel(fake)
	m.cursor = 1
	m.included[1] = false

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("clonar não devolveu comando")
	}
	m.Update(cmd())

	if len(fake.cloned.Repositories) != 2 ||
		fake.cloned.Repositories[0].Name != "pix" ||
		fake.cloned.Repositories[1].Name != "pix-worker" {
		t.Fatalf("plano clonado = %#v", fake.cloned.Repositories)
	}
}

func TestDetalhesAparecemNaRevisao(t *testing.T) {
	m := reviewModel(&managerFake{})
	out := m.View(tui.Frame{Width: 150, Height: 38})
	for _, want := range []string{
		"API principal", "private", "Java", "main", "2.0 MB",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render não contém %q\n%s", want, out)
		}
	}
}
