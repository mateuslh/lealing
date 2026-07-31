package tokens

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	coretokens "github.com/mateuslh/lealing/cmd/tools/token-usage/internal/tokens"
	"github.com/mateuslh/lealing/sdk/component"
	"github.com/mateuslh/lealing/sdk/protocol"
)

type fakeGenerator struct{ report coretokens.Report }

func (f fakeGenerator) Generate(context.Context) (coretokens.Report, error) { return f.report, nil }

var geometryFrames = []protocol.Frame{
	{Width: 200, Height: 60}, {Width: 150, Height: 42}, {Width: 120, Height: 36},
	{Width: 100, Height: 30}, {Width: 84, Height: 26}, {Width: 70, Height: 22},
	{Width: 50, Height: 16}, {Width: 34, Height: 12}, {Width: 26, Height: 8},
}

func fixedNow() time.Time { return time.Date(2026, 7, 30, 15, 4, 5, 0, time.UTC) }

func reportFixture() coretokens.Report {
	slice := func(label string, cost float64) coretokens.Slice {
		return coretokens.Slice{Label: label, Totals: coretokens.Totals{Cost: cost, Messages: 10}}
	}
	days := make([]coretokens.DayPoint, 30)
	for i := range days {
		date := fixedNow().AddDate(0, 0, i-29)
		days[i] = coretokens.DayPoint{Date: date, Day: date.Format(time.DateOnly), Totals: coretokens.Totals{Cost: float64(i) * 3.7}}
	}
	return coretokens.Report{
		Overall:    coretokens.Totals{Input: 1_000_000, Output: 500_000, Messages: 3393, Cost: 330.68},
		ByProvider: []coretokens.Slice{slice("Claude Code", 328.81), slice("Codex", 1.87)},
		ByModel: []coretokens.Slice{
			slice("claude-sonnet-5", 184.52), slice("claude-opus-4-8", 91.21),
			slice("claude-opus-5", 52.57), slice("gpt-5.5", 1.39),
		},
		ByProject: []coretokens.Slice{slice("leanling", 120), slice("projeto-com-nome-muito-longo", 80)},
		ByDay:     days,
		Windows: []coretokens.WindowUsage{
			{Label: "Últimas 5h", Totals: coretokens.Totals{Cost: 48.62}},
			{Label: "Hoje", Totals: coretokens.Totals{Cost: 52.57}},
			{Label: "7 dias", Totals: coretokens.Totals{Cost: 144.26}},
			{Label: "Este mês", Totals: coretokens.Totals{Cost: 329.53}},
		},
		RateWindows: []coretokens.RateWindow{
			{Provider: "Claude Code", Label: "Sessão 5h", UsedPercent: 53, WindowMinutes: 300, ResetsAt: fixedNow().Add(90 * time.Minute), ObservedAt: fixedNow(), Source: "conta"},
			{Provider: "Codex", Label: "Semana", UsedPercent: 31, WindowMinutes: 10080, ResetsAt: fixedNow().AddDate(0, 0, 2), ObservedAt: fixedNow(), Source: "log local"},
		},
		Credits: []coretokens.Credits{{Provider: "Claude Code", Used: 83.89, Limit: 275, Currency: "BRL", Enabled: true}},
	}
}

func loadedModel(t *testing.T) *Model {
	t.Helper()
	model := New(component.DefaultTheme(), fakeGenerator{report: reportFixture()}, fixedNow)
	message := model.Init()()
	next, _ := model.Update(message)
	return next.(*Model)
}

func TestGeometriaNosNoveTamanhos(t *testing.T) {
	for _, frame := range geometryFrames {
		model := loadedModel(t)
		next, _ := model.Update(tea.WindowSizeMsg{Width: frame.Width, Height: frame.Height})
		model = next.(*Model)
		for _, key := range []tea.KeyType{tea.KeyTab, tea.KeyDown, tea.KeyDown} {
			next, _ = model.Update(tea.KeyMsg{Type: key})
			model = next.(*Model)
		}
		output := model.View(frame)
		lines := strings.Split(output, "\n")
		if len(lines) > frame.Height {
			t.Errorf("%dx%d: %d linhas", frame.Width, frame.Height, len(lines))
		}
		for line, text := range lines {
			if width := lipgloss.Width(text); width > frame.Width {
				t.Errorf("%dx%d: linha %d tem %d colunas", frame.Width, frame.Height, line, width)
			}
		}
	}
}

func TestVerticalMantemIdentidadeStatusEHints(t *testing.T) {
	model := loadedModel(t)
	for _, frame := range []protocol.Frame{{Width: 150, Height: 42}, {Width: 60, Height: 20}} {
		output := model.View(frame)
		if !strings.Contains(output, "$331") {
			t.Errorf("%dx%d não preservou o total no layout", frame.Width, frame.Height)
		}
	}
	if model.ID() != "tool/token-usage" || model.Title() != "uso de tokens" {
		t.Fatalf("identidade mudou: %s / %s", model.ID(), model.Title())
	}
	if !strings.Contains(model.Status(), "mensagens") {
		t.Fatalf("status = %q", model.Status())
	}
	var hasEscape bool
	for _, hint := range model.Hints() {
		hasEscape = hasEscape || strings.Contains(hint.Key, "esc")
	}
	if !hasEscape {
		t.Fatal("hints não anunciam esc")
	}
}
