package gitinsight

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	core "github.com/mateuslh/lealing/internal/core/gitinsight"
)

type scannerFake struct {
	report       core.Report
	updateReport core.UpdateReport
	calls        *[]string
}

func (f scannerFake) Scan(context.Context) (core.Report, error) { return f.report, nil }
func (f scannerFake) Fetch(_ context.Context, repo string) error {
	if f.calls != nil {
		*f.calls = append(*f.calls, "fetch:"+repo)
	}
	return nil
}
func (f scannerFake) Push(_ context.Context, repo string, branch core.Branch) error {
	if f.calls != nil {
		*f.calls = append(*f.calls, "push:"+repo+":"+branch.Name)
	}
	return nil
}
func (f scannerFake) DeleteLocalBranch(_ context.Context, repo, branch string) error {
	if f.calls != nil {
		*f.calls = append(*f.calls, "delete:"+repo+":"+branch)
	}
	return nil
}
func (f scannerFake) UpdateAll(context.Context) (core.UpdateReport, error) {
	if f.calls != nil {
		*f.calls = append(*f.calls, "update-all")
	}
	return f.updateReport, nil
}

func screenReport() core.Report {
	return core.Report{
		Root:      "/Users/teste/dev",
		ScannedAt: time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC),
		Repositories: []core.Repository{
			{
				Name: "api", Relative: "pix/api", Path: "/Users/teste/dev/pix/api", DirtyFiles: 2,
				Branches: []core.Branch{
					{Name: "main", Upstream: "origin/main", Remote: "origin", RemoteRef: "refs/heads/main", Current: true, Ahead: 2, Hash: "abc1234", Subject: "corrige o pix"},
					{Name: "pronta", Upstream: "origin/pronta", Remote: "origin", RemoteRef: "refs/heads/pronta", Hash: "def5678", Subject: "já publicada"},
					{Name: "rascunho", Hash: "fed4321", Subject: "sem upstream"},
				},
			},
			{
				Name: "config", Relative: "pix/config", Path: "/Users/teste/dev/pix/config",
				Branches: []core.Branch{{Name: "main", Upstream: "origin/main", Remote: "origin", RemoteRef: "refs/heads/main", Current: true}},
			},
		},
	}
}

func TestPushExigeEscolhaEConfirmacao(t *testing.T) {
	var calls []string
	m := New(tui.Deps{Theme: theme.Default()}, scannerFake{
		report: screenReport(),
		calls:  &calls,
	})
	next, _ := m.Update(m.Init()())
	m = next.(*Model)

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	if m.mode != modePick || !m.Capturing() {
		t.Fatalf("modo após p = %v, capturing = %v", m.mode, m.Capturing())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.mode != modeConfirm || len(calls) != 0 {
		t.Fatalf("confirmação = %v, chamadas = %v", m.mode, calls)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'s'}})
	if cmd == nil || m.mode != modeRunning {
		t.Fatalf("execução não iniciou: mode=%v cmd=%v", m.mode, cmd)
	}
	msg := cmd()
	m.Update(msg)
	if len(calls) != 1 || calls[0] != "push:/Users/teste/dev/pix/api:main" {
		t.Fatalf("chamadas = %v", calls)
	}
}

func TestRemocaoLocalEFetchAgemNoCloneSelecionado(t *testing.T) {
	var calls []string
	m := New(tui.Deps{Theme: theme.Default()}, scannerFake{
		report: screenReport(),
		calls:  &calls,
	})
	next, _ := m.Update(m.Init()())
	m = next.(*Model)

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.Update(cmd())

	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	m.Update(cmd())

	want := []string{
		"delete:/Users/teste/dev/pix/api:pronta",
		"fetch:/Users/teste/dev/pix/api",
	}
	if strings.Join(calls, "|") != strings.Join(want, "|") {
		t.Fatalf("chamadas = %v, quero %v", calls, want)
	}
}

func loadedModel(t *testing.T) *Model {
	t.Helper()
	m := New(tui.Deps{Theme: theme.Default()}, scannerFake{report: screenReport()})
	next, _ := m.Update(m.Init()())
	return next.(*Model)
}

func TestFiltrosSelecionamRepositoriosAcionaveis(t *testing.T) {
	m := loadedModel(t)
	if len(m.repositories()) != 2 {
		t.Fatalf("todos = %d", len(m.repositories()))
	}
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.filter != filterPush || len(m.repositories()) != 1 ||
		m.repositories()[0].Name != "api" {
		t.Fatalf("filtro push = %v, repos %#v", m.filter, m.repositories())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyTab})
	if m.filter != filterCleanup || len(m.repositories()) != 1 {
		t.Fatalf("filtro limpeza = %v, repos %#v", m.filter, m.repositories())
	}
}

func TestDashboardMostraResumoEDetalhes(t *testing.T) {
	m := loadedModel(t)
	out := m.View(tui.Frame{Width: 150, Height: 38})
	for _, want := range []string{
		"PARA PUSH", "PUBLICADAS", "SEM UPSTREAM",
		"COMMITS PARA ENVIAR", "LOCAL JÁ PUBLICADA", "corrige o pix",
		"local publicada", "sem upstream", "alterações",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render não contém %q\n%s", want, out)
		}
	}
}

func TestNavegaPorTipoEMostraSomenteAClassificacaoAtiva(t *testing.T) {
	m := loadedModel(t)

	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if m.filter != filterPush {
		t.Fatalf("filtro após direita = %v", m.filter)
	}
	out := m.View(tui.Frame{Width: 150, Height: 38})
	if !strings.Contains(out, "COMMITS PARA ENVIAR") ||
		strings.Contains(out, "LOCAL JÁ PUBLICADA") {
		t.Fatalf("visão push misturou tipos:\n%s", out)
	}

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	if m.filter != filterCleanup {
		t.Fatalf("atalho 3 abriu filtro %v", m.filter)
	}
	out = m.View(tui.Frame{Width: 150, Height: 38})
	if !strings.Contains(out, "LOCAL JÁ PUBLICADA") ||
		strings.Contains(out, "COMMITS PARA ENVIAR") {
		t.Fatalf("visão de publicadas misturou tipos:\n%s", out)
	}
}

func TestConfirmacaoAbrePopupSobreDashboard(t *testing.T) {
	m := loadedModel(t)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	out := m.View(tui.Frame{Width: 120, Height: 30})
	for _, want := range []string{
		"CLONES", "CONFIRMAR PUSH", "PUBLICAR COMMITS NO UPSTREAM",
		"origin/main", "[ s / enter ] confirmar", "[ n ] cancelar",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("popup não contém %q:\n%s", want, out)
		}
	}
}

func TestAtualizarTodosExigeConfirmacaoEMostraRelatorio(t *testing.T) {
	var calls []string
	updateReport := core.UpdateReport{Results: []core.UpdateResult{
		{Repository: "pix/api", Branch: "main", State: core.UpdateUpdated, Detail: "avançou 2 commits"},
		{Repository: "pix/config", Branch: "main", State: core.UpdateCurrent, Detail: "já estava em dia"},
		{Repository: "pix/worker", Branch: "master", State: core.UpdateSkipped, Detail: "working tree alterada"},
		{Repository: "pix/legado", State: core.UpdateFailed, Detail: "fetch falhou"},
	}}
	m := New(tui.Deps{Theme: theme.Default()}, scannerFake{
		report:       screenReport(),
		updateReport: updateReport,
		calls:        &calls,
	})
	next, _ := m.Update(m.Init()())
	m = next.(*Model)

	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	if m.mode != modeConfirm || len(calls) != 0 {
		t.Fatalf("u não abriu confirmação: modo=%v chamadas=%v", m.mode, calls)
	}
	confirmation := m.View(tui.Frame{Width: 120, Height: 30})
	for _, want := range []string{
		"CONFIRMAR ATUALIZAÇÃO GERAL", "2", "MAIN/MASTER",
		"somente fast-forward", "serão ignorados",
	} {
		if !strings.Contains(confirmation, want) {
			t.Errorf("confirmação geral não contém %q:\n%s", want, confirmation)
		}
	}

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || m.mode != modeRunning {
		t.Fatalf("atualização não iniciou: modo=%v cmd=%v", m.mode, cmd)
	}
	m.Update(cmd())
	if m.mode != modeResults || len(calls) != 1 || calls[0] != "update-all" {
		t.Fatalf("resultado não abriu: modo=%v chamadas=%v", m.mode, calls)
	}

	result := m.View(tui.Frame{Width: 120, Height: 30})
	for _, want := range []string{
		"ATUALIZAÇÃO CONCLUÍDA COM FALHAS",
		"1 atualizados", "1 em dia", "1 ignorados", "1 falhas",
		"pix/api", "avançou 2 commits",
	} {
		if !strings.Contains(result, want) {
			t.Errorf("relatório não contém %q:\n%s", want, result)
		}
	}

	_, reload := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if reload == nil || m.mode != modeBrowse || !m.loading {
		t.Fatalf("fechar relatório não recarregou: modo=%v loading=%v", m.mode, m.loading)
	}
}
