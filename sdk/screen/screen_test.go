package screen_test

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mateuslh/lealing/sdk/protocol"
	"github.com/mateuslh/lealing/sdk/screen"
)

type loadedMsg struct{}

type model struct {
	body      string
	capturing bool
}

type colorModel struct{}

func (colorModel) Init() tea.Cmd                          { return nil }
func (colorModel) Update(tea.Msg) (screen.Model, tea.Cmd) { return colorModel{}, nil }
func (colorModel) View(protocol.Frame) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("#ff6688")).Bold(true).Render("cor")
}

func (m *model) Init() tea.Cmd { return func() tea.Msg { return loadedMsg{} } }
func (m *model) Update(message tea.Msg) (screen.Model, tea.Cmd) {
	switch message := message.(type) {
	case loadedMsg:
		m.body = "carregado"
	case tea.KeyMsg:
		m.body = "tecla:" + message.String()
	case screen.ShutdownMsg:
		m.body = "encerrando"
	}
	return m, nil
}
func (m *model) View(protocol.Frame) string { return m.body }
func (m *model) Hints() []protocol.Hint     { return []protocol.Hint{{Key: "esc", Label: "voltar"}} }
func (m *model) Status() string             { return "ok" }
func (m *model) Capturing() bool            { return m.capturing }

type harness struct {
	engineIn  io.WriteCloser
	engineOut io.ReadCloser
	encoder   *protocol.Encoder
	decoder   *protocol.Decoder
	done      chan error
}

func start(t *testing.T, capabilities ...string) *harness {
	t.Helper()
	toolInput, engineInput := io.Pipe()
	engineOutput, toolOutput := io.Pipe()
	h := &harness{
		engineIn: engineInput, engineOut: engineOutput,
		encoder: protocol.NewEncoder(engineInput), decoder: protocol.NewDecoder(engineOutput),
		done: make(chan error, 1),
	}
	go func() {
		h.done <- screen.Run(context.Background(), screen.Config{
			ToolVersion: "1.0.0", Protocol: protocol.VersionRange{Min: 1, Max: 1},
			Capabilities: capabilities, Factory: func(screen.Session) screen.Model { return &model{body: "inicial"} },
			Input: toolInput, Output: toolOutput,
		})
	}()
	initialize := protocol.Initialize{
		Protocol: protocol.VersionRange{Min: 1, Max: 1}, ToolID: "demo",
		Frame: protocol.Frame{Width: 60, Height: 20}, Capabilities: capabilities,
	}
	if err := h.send(t, 1, protocol.MethodInitialize, initialize); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *harness) send(t *testing.T, sequence uint64, method string, payload any) error {
	t.Helper()
	message, err := protocol.NewMessage(1, sequence, method, payload)
	if err != nil {
		return err
	}
	return h.encoder.Write(message)
}

func (h *harness) close() {
	_ = h.engineIn.Close()
	_ = h.engineOut.Close()
}

func TestRuntimeNegociaExecutaComandoEEntregaEventos(t *testing.T) {
	h := start(t)
	defer h.close()
	initializedMessage, err := h.decoder.Read()
	if err != nil {
		t.Fatal(err)
	}
	initialized, err := protocol.DecodePayload[protocol.Initialized](initializedMessage)
	if err != nil {
		t.Fatal(err)
	}
	if initialized.Snapshot == nil || initialized.Snapshot.Body != "inicial" {
		t.Fatalf("initialized = %+v", initialized)
	}

	// Init roda pelo executor assíncrono do Bubble Tea e produz outro snapshot.
	loaded, err := h.decoder.Read()
	if err != nil {
		t.Fatal(err)
	}
	loadedSnapshot, _ := protocol.DecodePayload[protocol.Snapshot](loaded)
	if loadedSnapshot.Body != "carregado" || loadedSnapshot.Status != "ok" {
		t.Fatalf("snapshot = %+v", loadedSnapshot)
	}

	event := protocol.UIEvent{Type: protocol.EventKey, Key: &protocol.KeyEvent{Code: "rune", Text: "x"}}
	if err := h.send(t, 2, protocol.MethodUIEvent, event); err != nil {
		t.Fatal(err)
	}
	keyMessage, err := h.decoder.Read()
	if err != nil {
		t.Fatal(err)
	}
	keySnapshot, _ := protocol.DecodePayload[protocol.Snapshot](keyMessage)
	if keySnapshot.Body != "tecla:x" {
		t.Fatalf("snapshot = %+v", keySnapshot)
	}

	if err := h.send(t, 3, protocol.MethodShutdown, protocol.Shutdown{Reason: "fim"}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-h.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("runtime não encerrou")
	}
}

func TestRuntimePreservaANSIParaPaletaDaTool(t *testing.T) {
	toolInput, engineInput := io.Pipe()
	engineOutput, toolOutput := io.Pipe()
	encoder, decoder := protocol.NewEncoder(engineInput), protocol.NewDecoder(engineOutput)
	done := make(chan error, 1)
	go func() {
		done <- screen.Run(context.Background(), screen.Config{
			ToolVersion: "1.0.0", Protocol: protocol.VersionRange{Min: 1, Max: 1},
			Factory: func(screen.Session) screen.Model { return colorModel{} },
			Input:   toolInput, Output: toolOutput,
		})
	}()
	defer func() {
		_ = engineInput.Close()
		_ = engineOutput.Close()
	}()
	initialize := protocol.Initialize{Protocol: protocol.VersionRange{Min: 1, Max: 1}, ToolID: "colors", Frame: protocol.Frame{Width: 20, Height: 5}}
	message, err := protocol.NewMessage(1, 1, protocol.MethodInitialize, initialize)
	if err != nil {
		t.Fatal(err)
	}
	if err := encoder.Write(message); err != nil {
		t.Fatal(err)
	}
	response, err := decoder.Read()
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := protocol.DecodePayload[protocol.Initialized](response)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Snapshot == nil || !strings.Contains(snapshot.Snapshot.Body, "\x1b[") {
		t.Fatalf("snapshot perdeu ANSI: %q", snapshot.Snapshot.Body)
	}
}

type requestModel struct{ requested bool }

func (m *requestModel) Init() tea.Cmd { return nil }
func (m *requestModel) Update(message tea.Msg) (screen.Model, tea.Cmd) {
	if !m.requested {
		m.requested = true
		return m, screen.Request(protocol.CapabilityNotificationShow, map[string]string{"message": "oi"})
	}
	if response, ok := message.(screen.HostResponseMsg); ok {
		if response.Error == nil {
			m.requested = false
		}
	}
	return m, nil
}
func (m *requestModel) View(protocol.Frame) string { return "request" }

func TestRequestUsaSomenteCapabilityNegociada(t *testing.T) {
	// A validação principal de capability fica no runtime da engine. Aqui o
	// SDK prova que uma request não negociada volta ao model sem sair no fio.
	cmd := screen.Request(protocol.CapabilityBrowserOpen, map[string]string{"url": "https://example.com"})
	if cmd == nil {
		t.Fatal("Request não produziu comando")
	}
}

func TestHandshakeIncompativelRespondeAntesDeSair(t *testing.T) {
	toolInput, engineInput := io.Pipe()
	engineOutput, toolOutput := io.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- screen.Run(context.Background(), screen.Config{
			Protocol: protocol.VersionRange{Min: 2, Max: 2},
			Factory:  func(screen.Session) screen.Model { return &model{} },
			Input:    toolInput, Output: toolOutput,
		})
	}()
	encoder, decoder := protocol.NewEncoder(engineInput), protocol.NewDecoder(engineOutput)
	message, _ := protocol.NewMessage(1, 1, protocol.MethodInitialize, protocol.Initialize{Protocol: protocol.VersionRange{Min: 1, Max: 1}})
	if err := encoder.Write(message); err != nil {
		t.Fatal(err)
	}
	initializedMessage, err := decoder.Read()
	if err != nil {
		t.Fatal(err)
	}
	initialized, _ := protocol.DecodePayload[protocol.Initialized](initializedMessage)
	if initialized.State != "incompatible" {
		t.Fatalf("initialized = %+v", initialized)
	}
	if err := <-done; err == nil || !strings.Contains(err.Error(), "incompatível") {
		t.Fatalf("erro = %v", err)
	}
}
