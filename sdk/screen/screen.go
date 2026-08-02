// Package screen adapta models Bubble Tea ao protocolo screen-v1. A tool
// continua dona de Init, Update, View e comandos assíncronos; a engine continua
// dona do terminal.
package screen

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/mateuslh/lealing/sdk/protocol"
)

// Model é o contrato público mínimo. Interfaces opcionais abaixo enriquecem
// status, hints, captura e cursor sem obrigar models simples a implementá-las.
type Model interface {
	Init() tea.Cmd
	Update(tea.Msg) (Model, tea.Cmd)
	View(protocol.Frame) string
}

type Hinter interface{ Hints() []protocol.Hint }
type Statuser interface{ Status() string }
type Capturer interface{ Capturing() bool }
type CursorProvider interface{ Cursor() *protocol.Cursor }

// Session contém tudo que foi negociado antes de construir o model.
type Session struct {
	Initialize   protocol.Initialize
	Version      int
	Capabilities []string
}

type Factory func(Session) Model

type Config struct {
	ToolVersion    string
	Protocol       protocol.VersionRange
	Capabilities   []string
	Factory        Factory
	Input          io.Reader
	Output         io.Writer
	MaxMessageSize int
}

// HostResponseMsg é entregue ao Update quando a engine conclui uma ação
// solicitada por Request.
type HostResponseMsg struct {
	ID, Method string
	Result     json.RawMessage
	Error      *protocol.Error
}

type ThemeChangedMsg struct{ Theme protocol.Theme }
type TickMsg time.Time
type ShutdownMsg struct{ Reason string }

type hostRequestMsg struct {
	method string
	params json.RawMessage
	err    error
}

// Request cria um comando Bubble Tea para uma capability do host.
func Request(method string, params any) tea.Cmd {
	return func() tea.Msg {
		raw, err := json.Marshal(params)
		return hostRequestMsg{method: method, params: raw, err: err}
	}
}

// Run ocupa stdin/stdout com o protocolo até shutdown ou cancelamento.
func Run(ctx context.Context, config Config) error {
	if config.Factory == nil {
		return errors.New("screen.Factory é obrigatória")
	}
	if !config.Protocol.Valid() {
		config.Protocol = protocol.VersionRange{Min: protocol.Version1, Max: protocol.Version1}
	}
	if config.Input == nil {
		config.Input = os.Stdin
	}
	if config.Output == nil {
		config.Output = os.Stdout
	}
	if config.MaxMessageSize <= 0 {
		config.MaxMessageSize = protocol.MaxMessageSize
	}
	// A tool escreve em um pipe, não em um TTY; sem este perfil o Lip Gloss
	// detecta saída não interativa e remove todas as cores antes do snapshot.
	// A engine continua sanitizando SGR e convertendo o frame para o terminal
	// do usuário, portanto forçar ANSI aqui não libera sequências perigosas.
	lipgloss.SetColorProfile(termenv.TrueColor)
	decoder := protocol.NewDecoderSize(config.Input, config.MaxMessageSize)
	encoder := protocol.NewEncoderSize(config.Output, config.MaxMessageSize)

	first, err := decoder.Read()
	if err != nil {
		return fmt.Errorf("ler initialize: %w", err)
	}
	if first.Method != protocol.MethodInitialize {
		return fmt.Errorf("primeira mensagem deve ser initialize, recebeu %s", first.Method)
	}
	initialize, err := protocol.DecodePayload[protocol.Initialize](first)
	if err != nil {
		return err
	}
	if first.Version < initialize.Protocol.Min || first.Version > initialize.Protocol.Max {
		return fmt.Errorf("initialize usa versão %d fora do intervalo oferecido", first.Version)
	}
	version, compatible := protocol.Negotiate(initialize.Protocol, config.Protocol)
	if !compatible {
		initialized := protocol.Initialized{
			Protocol: config.Protocol, ToolVersion: config.ToolVersion,
			UIMode: protocol.UIModeScreenV1, State: "incompatible",
		}
		_ = write(encoder, versionOrOne(config.Protocol.Max), 1, protocol.MethodInitialized, initialized)
		return fmt.Errorf("protocolo incompatível: engine %d–%d; tool %d–%d", initialize.Protocol.Min, initialize.Protocol.Max, config.Protocol.Min, config.Protocol.Max)
	}
	capabilities := capabilityIntersection(config.Capabilities, initialize.Capabilities)
	model := config.Factory(Session{Initialize: initialize, Version: version, Capabilities: capabilities})
	if model == nil {
		return errors.New("screen.Factory devolveu model nil")
	}

	adapter := newAdapter(model, initialize.Frame.Clamp())
	initialSnapshot := adapter.snapshot()
	if err := write(encoder, version, 1, protocol.MethodInitialized, protocol.Initialized{
		Protocol: config.Protocol, ToolVersion: config.ToolVersion,
		Capabilities: capabilities, UIMode: protocol.UIModeScreenV1,
		State: "ready", Snapshot: &initialSnapshot,
	}); err != nil {
		return err
	}

	program := tea.NewProgram(adapter,
		tea.WithContext(ctx), tea.WithInput(nil), tea.WithOutput(io.Discard), tea.WithoutRenderer(),
	)
	programDone := make(chan error, 1)
	go func() {
		_, err := program.Run()
		programDone <- err
	}()

	incoming := make(chan readResult, 1)
	go readMessages(decoder, incoming)
	outSequence := uint64(1)
	inSequence := first.Sequence
	requestSequence := uint64(0)
	pending := map[string]string{}
	shuttingDown := false

	for {
		select {
		case <-ctx.Done():
			program.Quit()
			return ctx.Err()

		case err := <-programDone:
			if shuttingDown || err == nil {
				return nil
			}
			return fmt.Errorf("model screen-v1 encerrou: %w", err)

		case snapshot := <-adapter.snapshots:
			outSequence++
			if err := write(encoder, version, outSequence, protocol.MethodUISnapshot, snapshot); err != nil {
				program.Quit()
				return err
			}

		case request := <-adapter.requests:
			if request.err != nil {
				program.Send(HostResponseMsg{Method: request.method, Error: &protocol.Error{Code: "invalid_request", Message: request.err.Error()}})
				continue
			}
			if !contains(capabilities, request.method) {
				program.Send(HostResponseMsg{Method: request.method, Error: &protocol.Error{Code: "capability_denied", Message: "capability não negociada"}})
				continue
			}
			requestSequence++
			id := fmt.Sprintf("host-%d", requestSequence)
			pending[id] = request.method
			outSequence++
			if err := write(encoder, version, outSequence, protocol.MethodHostRequest, protocol.HostRequest{ID: id, Method: request.method, Params: request.params}); err != nil {
				program.Quit()
				return err
			}

		case result := <-incoming:
			if result.err != nil {
				program.Quit()
				if shuttingDown && errors.Is(result.err, io.EOF) {
					return nil
				}
				return fmt.Errorf("ler engine: %w", result.err)
			}
			message := result.message
			if message.Sequence <= inSequence {
				continue
			}
			inSequence = message.Sequence
			if message.Version != version {
				program.Quit()
				return fmt.Errorf("mensagem usa versão %d; sessão negociou %d", message.Version, version)
			}
			switch message.Method {
			case protocol.MethodUIEvent:
				event, err := protocol.DecodePayload[protocol.UIEvent](message)
				if err != nil {
					return err
				}
				if teaMessage := eventMessage(event); teaMessage != nil {
					program.Send(teaMessage)
				}

			case protocol.MethodHostResponse:
				response, err := protocol.DecodePayload[protocol.HostResponse](message)
				if err != nil {
					return err
				}
				method := pending[response.ID]
				delete(pending, response.ID)
				program.Send(HostResponseMsg{ID: response.ID, Method: method, Result: response.Result, Error: response.Error})

			case protocol.MethodShutdown:
				shutdown, _ := protocol.DecodePayload[protocol.Shutdown](message)
				shuttingDown = true
				program.Send(ShutdownMsg{Reason: shutdown.Reason})
				// Send bloqueia até o loop aceitar ShutdownMsg, então o Quit
				// seguinte preserva a oportunidade do model limpar seu estado.
				program.Quit()

			case protocol.MethodError:
				remote, decodeErr := protocol.DecodePayload[protocol.Error](message)
				if decodeErr != nil {
					return decodeErr
				}
				if remote.Fatal {
					program.Quit()
					return errors.New(remote.Message)
				}
			default:
				return fmt.Errorf("método inesperado da engine: %s", message.Method)
			}
		}
	}
}

type adapterModel struct {
	model     Model
	frame     protocol.Frame
	sequence  uint64
	snapshots chan protocol.Snapshot
	requests  chan hostRequestMsg
	mu        sync.Mutex
}

func newAdapter(model Model, frame protocol.Frame) *adapterModel {
	return &adapterModel{model: model, frame: frame, snapshots: make(chan protocol.Snapshot, 4), requests: make(chan hostRequestMsg, 16)}
}

func (m *adapterModel) Init() tea.Cmd { return m.model.Init() }

func (m *adapterModel) Update(message tea.Msg) (tea.Model, tea.Cmd) {
	if request, ok := message.(hostRequestMsg); ok {
		m.requests <- request
		return m, nil
	}
	if resize, ok := message.(tea.WindowSizeMsg); ok {
		m.frame = protocol.Frame{Width: resize.Width, Height: resize.Height}.Clamp()
	}
	next, command := m.model.Update(message)
	if next != nil {
		m.model = next
	}
	if _, shutdown := message.(ShutdownMsg); shutdown {
		return m, tea.Quit
	}
	m.snapshots <- m.snapshot()
	return m, command
}

func (m *adapterModel) View() string { return "" }

func (m *adapterModel) snapshot() protocol.Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sequence++
	snapshot := protocol.Snapshot{Sequence: m.sequence, Body: m.model.View(m.frame)}
	if provider, ok := m.model.(Hinter); ok {
		snapshot.Hints = provider.Hints()
	}
	if provider, ok := m.model.(Statuser); ok {
		snapshot.Status = provider.Status()
	}
	if provider, ok := m.model.(Capturer); ok {
		snapshot.Capturing = provider.Capturing()
	}
	if provider, ok := m.model.(CursorProvider); ok {
		snapshot.Cursor = provider.Cursor()
	}
	return snapshot
}

type readResult struct {
	message protocol.Message
	err     error
}

func readMessages(decoder *protocol.Decoder, output chan<- readResult) {
	for {
		message, err := decoder.Read()
		output <- readResult{message: message, err: err}
		if err != nil {
			return
		}
	}
}

func write(encoder *protocol.Encoder, version int, sequence uint64, method string, payload any) error {
	message, err := protocol.NewMessage(version, sequence, method, payload)
	if err != nil {
		return err
	}
	return encoder.Write(message)
}

func capabilityIntersection(requested, offered []string) []string {
	result := make([]string, 0, len(requested))
	for _, capability := range requested {
		if contains(offered, capability) {
			result = append(result, capability)
		}
	}
	return result
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func versionOrOne(version int) int {
	if version > 0 {
		return version
	}
	return protocol.Version1
}

func eventMessage(event protocol.UIEvent) tea.Msg {
	switch event.Type {
	case protocol.EventKey:
		if event.Key == nil {
			return nil
		}
		return keyMessage(*event.Key)
	case protocol.EventPaste:
		if event.Paste == nil {
			return nil
		}
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(event.Paste.Text), Paste: true}
	case protocol.EventMouse:
		if event.Mouse == nil {
			return nil
		}
		return mouseMessage(*event.Mouse)
	case protocol.EventResize:
		if event.Resize == nil {
			return nil
		}
		frame := event.Resize.Frame.Clamp()
		return tea.WindowSizeMsg{Width: frame.Width, Height: frame.Height}
	case protocol.EventThemeChanged:
		if event.ThemeChanged != nil {
			return ThemeChangedMsg{Theme: event.ThemeChanged.Theme}
		}
	case protocol.EventFocus:
		return tea.FocusMsg{}
	case protocol.EventBlur:
		return tea.BlurMsg{}
	case protocol.EventTick:
		if event.Tick != nil {
			return TickMsg(time.UnixMilli(event.Tick.UnixMilli))
		}
	case protocol.EventShutdown:
		reason := ""
		if event.Shutdown != nil {
			reason = event.Shutdown.Reason
		}
		return ShutdownMsg{Reason: reason}
	}
	return nil
}

func keyMessage(key protocol.KeyEvent) tea.KeyMsg {
	if key.Text != "" && !key.Ctrl {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key.Text), Alt: key.Alt}
	}
	keyTypes := map[string]tea.KeyType{
		"enter": tea.KeyEnter, "esc": tea.KeyEsc, "escape": tea.KeyEsc,
		"tab": tea.KeyTab, "shift+tab": tea.KeyShiftTab, "backspace": tea.KeyBackspace,
		"space": tea.KeySpace, "up": tea.KeyUp, "down": tea.KeyDown,
		"left": tea.KeyLeft, "right": tea.KeyRight, "home": tea.KeyHome,
		"end": tea.KeyEnd, "pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown,
		"delete": tea.KeyDelete, "insert": tea.KeyInsert,
		"ctrl+a": tea.KeyCtrlA, "ctrl+b": tea.KeyCtrlB, "ctrl+c": tea.KeyCtrlC,
		"ctrl+d": tea.KeyCtrlD, "ctrl+e": tea.KeyCtrlE, "ctrl+f": tea.KeyCtrlF,
		"ctrl+k": tea.KeyCtrlK, "ctrl+n": tea.KeyCtrlN, "ctrl+p": tea.KeyCtrlP,
		"ctrl+r": tea.KeyCtrlR, "ctrl+u": tea.KeyCtrlU,
	}
	code := strings.ToLower(key.Code)
	if keyType, ok := keyTypes[code]; ok {
		return tea.KeyMsg{Type: keyType, Alt: key.Alt}
	}
	if key.Text != "" {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key.Text), Alt: key.Alt}
	}
	return tea.KeyMsg{}
}

func mouseMessage(mouse protocol.MouseEvent) tea.MouseMsg {
	buttons := map[string]tea.MouseButton{
		"left": tea.MouseButtonLeft, "middle": tea.MouseButtonMiddle, "right": tea.MouseButtonRight,
		"wheelUp": tea.MouseButtonWheelUp, "wheelDown": tea.MouseButtonWheelDown,
		"wheelLeft": tea.MouseButtonWheelLeft, "wheelRight": tea.MouseButtonWheelRight,
		"backward": tea.MouseButtonBackward, "forward": tea.MouseButtonForward,
	}
	actions := map[string]tea.MouseAction{"press": tea.MouseActionPress, "release": tea.MouseActionRelease, "motion": tea.MouseActionMotion}
	return tea.MouseMsg(tea.MouseEvent{
		X: mouse.X, Y: mouse.Y, Button: buttons[mouse.Button], Action: actions[mouse.Action],
		Ctrl: mouse.Ctrl, Alt: mouse.Alt, Shift: mouse.Shift,
	})
}
