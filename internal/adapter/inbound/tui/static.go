package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// perCmdTimeout é quanto esperamos por um comando antes de desistir dele.
// Blink de cursor e tickers nunca respondem a tempo, e um frame estático não
// tem para onde evoluir com eles.
const perCmdTimeout = 750 * time.Millisecond

// RenderStatic desenha um único frame fora do loop do Bubble Tea.
//
// Serve para capturar a interface em documentação e para conferir o layout
// em dimensões arbitrárias sem um terminal real. keys é uma sequência de
// teclas aplicada antes do render, no formato aceito por ParseKeys.
func RenderStatic(app *App, width, height int, keys []tea.KeyMsg, rounds int) string {
	var model tea.Model = app
	model, _ = model.Update(tea.WindowSizeMsg{Width: width, Height: height})

	model = settle(model, []tea.Cmd{app.Init()}, rounds)

	for _, key := range keys {
		next, cmd := model.Update(key)
		model = settle(next, []tea.Cmd{cmd}, rounds)
	}

	return model.View()
}

// settle processa a fila de comandos até esvaziar ou o orçamento acabar.
func settle(model tea.Model, pending []tea.Cmd, rounds int) tea.Model {
	for range rounds {
		if len(pending) == 0 {
			break
		}
		batch := pending
		pending = nil

		for _, cmd := range batch {
			for _, msg := range drain(cmd, perCmdTimeout) {
				var next tea.Cmd
				model, next = model.Update(msg)
				if next != nil {
					pending = append(pending, next)
				}
			}
		}
	}
	return model
}

// ParseKeys traduz uma descrição textual de teclas em eventos.
//
// Cada caractere vira uma tecla; nomes entre colchetes cobrem as especiais:
// "/git[down][enter]" abre a busca, digita "git", desce um item e executa.
func ParseKeys(s string) ([]tea.KeyMsg, error) {
	named := map[string]tea.KeyType{
		"enter": tea.KeyEnter, "esc": tea.KeyEsc, "tab": tea.KeyTab,
		"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
		"space": tea.KeySpace, "backspace": tea.KeyBackspace,
		"pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown,
		"home": tea.KeyHome, "end": tea.KeyEnd,
	}

	var out []tea.KeyMsg
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] != '[' {
			out = append(out, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{runes[i]}})
			continue
		}

		end := -1
		for j := i + 1; j < len(runes); j++ {
			if runes[j] == ']' {
				end = j
				break
			}
		}
		if end < 0 {
			return nil, fmt.Errorf("colchete sem fechamento na posição %d", i)
		}

		name := strings.ToLower(string(runes[i+1 : end]))
		kt, ok := named[name]
		if !ok {
			return nil, fmt.Errorf("tecla desconhecida %q", name)
		}
		out = append(out, tea.KeyMsg{Type: kt})
		i = end
	}
	return out, nil
}

// drain executa um comando e devolve as mensagens que ele produziu,
// desdobrando os agregadores do Bubble Tea (Batch e Sequence).
func drain(cmd tea.Cmd, timeout time.Duration) []tea.Msg {
	if cmd == nil {
		return nil
	}

	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()

	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(timeout):
		return nil // comando lento ou periódico: irrelevante para um frame
	}

	switch m := msg.(type) {
	case nil:
		return nil
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range m {
			out = append(out, drain(c, timeout)...)
		}
		return out
	case tea.QuitMsg:
		return nil
	default:
		return []tea.Msg{msg}
	}
}
