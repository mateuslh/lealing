// Package pluginprocess executa tools screen-v1 como subprocessos isolados.
package pluginprocess

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mateuslh/lealing-sdk/protocol"
	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/interactive"
	"github.com/mateuslh/lealing/internal/core/port/outbound"
)

type Config struct {
	Protocol         protocol.VersionRange
	HandshakeTimeout time.Duration
	ShutdownTimeout  time.Duration
	MaxMessageSize   int
	Logger           outbound.Logger
}

type Runtime struct {
	config   Config
	mu       sync.Mutex
	sessions map[*processSession]bool
}

var _ interactive.Runtime = (*Runtime)(nil)

func New(config Config) *Runtime {
	if !config.Protocol.Valid() {
		config.Protocol = protocol.VersionRange{Min: protocol.Version1, Max: protocol.Version1}
	}
	if config.HandshakeTimeout <= 0 {
		config.HandshakeTimeout = 5 * time.Second
	}
	if config.ShutdownTimeout <= 0 {
		config.ShutdownTimeout = 2 * time.Second
	}
	if config.MaxMessageSize <= 0 {
		config.MaxMessageSize = protocol.MaxMessageSize
	}
	return &Runtime{config: config, sessions: make(map[*processSession]bool)}
}

func (r *Runtime) Start(ctx context.Context, tool domain.Tool, options interactive.StartOptions) (interactive.Session, error) {
	if tool.Runtime == nil {
		return nil, errors.New("tool sem runtime externo")
	}
	declaredProtocol := protocol.VersionRange{Min: tool.Runtime.ProtocolMin, Max: tool.Runtime.ProtocolMax}
	if _, ok := protocol.Negotiate(r.config.Protocol, declaredProtocol); !ok {
		return nil, domain.WrapTool(tool.ID, &interactive.IncompatibleError{
			EngineMin: r.config.Protocol.Min, EngineMax: r.config.Protocol.Max,
			ToolMin: declaredProtocol.Min, ToolMax: declaredProtocol.Max,
		})
	}
	if err := validateExecutable(tool.Runtime.InstallDir, tool.Runtime.Executable); err != nil {
		return nil, domain.WrapTool(tool.ID, err)
	}
	for _, directory := range []string{options.DataDir, options.CacheDir} {
		if directory == "" {
			continue
		}
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return nil, domain.WrapTool(tool.ID, fmt.Errorf("preparar diretório da tool: %w", err))
		}
	}

	runCtx, cancel := context.WithCancel(ctx)
	command := exec.CommandContext(runCtx, tool.Runtime.Executable)
	command.Dir = tool.Runtime.InstallDir
	stdin, err := command.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdout, err := command.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stderr, err := command.StderrPipe()
	if err != nil {
		cancel()
		return nil, err
	}

	session := &processSession{
		tool: tool, config: r.config, command: command, cancel: cancel,
		encoder: protocol.NewEncoderSize(stdin, r.config.MaxMessageSize),
		decoder: protocol.NewDecoderSize(stdout, r.config.MaxMessageSize),
		stdin:   stdin, updates: make(chan interactive.Update, 8),
		waitDone: make(chan error, 1), done: make(chan struct{}), state: interactive.StateStarting,
		protocol: declaredProtocol, version: r.config.Protocol.Max,
		capabilities: make(map[string]bool, len(options.Capabilities)), hostRequests: make(map[string]bool),
	}
	// CommandContext continua sendo o dono do cancelamento, mas seu Cancel
	// primeiro pede shutdown pelo protocolo. WaitDelay força o término se a
	// tool travar e não honrar a janela graciosa.
	command.Cancel = func() error {
		go func() {
			_ = session.write(protocol.MethodShutdown, protocol.Shutdown{Reason: "context canceled"})
		}()
		return nil
	}
	command.WaitDelay = r.config.ShutdownTimeout
	if err := command.Start(); err != nil {
		cancel()
		return nil, domain.WrapTool(tool.ID, fmt.Errorf("iniciar processo: %w", err))
	}
	for _, capability := range options.Capabilities {
		session.capabilities[capability] = true
	}
	go session.captureStderr(stderr)
	go func() { session.waitDone <- command.Wait() }()

	initialize := protocol.Initialize{
		Protocol: r.config.Protocol, ToolID: string(tool.ID), EngineVersion: options.EngineVersion,
		Platform: options.Platform, Architecture: options.Architecture,
		Frame: toProtocolFrame(options.Frame).Clamp(), Theme: toProtocolTheme(options.Theme),
		HomeDir: options.HomeDir,
		DataDir: options.DataDir, CacheDir: options.CacheDir,
		WorkingDir:   options.WorkingDir,
		Capabilities: append([]string(nil), options.Capabilities...),
		Permissions: protocol.Permissions{
			Filesystem: protocol.FilesystemPermissions{
				Read:  append([]string(nil), options.Permissions.ReadPaths...),
				Write: append([]string(nil), options.Permissions.WritePaths...),
			},
			Network: options.Permissions.Network, Subprocess: options.Permissions.Subprocess,
			WorkingDir: options.Permissions.WorkingDir,
		},
	}
	if err := session.write(protocol.MethodInitialize, initialize); err != nil {
		session.forceStop()
		return nil, err
	}
	if err := session.handshake(ctx); err != nil {
		session.forceStop()
		return nil, domain.WrapTool(tool.ID, err)
	}
	session.onTerminal = func() {
		r.mu.Lock()
		delete(r.sessions, session)
		r.mu.Unlock()
	}
	r.mu.Lock()
	r.sessions[session] = true
	r.mu.Unlock()
	go session.readLoop()
	return session, nil
}

// Close encerra todas as sessões que ainda estiverem vivas quando a engine
// sai por um caminho que não desempilhou as telas individualmente.
func (r *Runtime) Close() error {
	r.mu.Lock()
	sessions := make([]*processSession, 0, len(r.sessions))
	for session := range r.sessions {
		sessions = append(sessions, session)
	}
	r.mu.Unlock()

	var errs []error
	for _, session := range sessions {
		ctx, cancel := context.WithTimeout(context.Background(), r.config.ShutdownTimeout+time.Second)
		err := session.Shutdown(ctx)
		cancel()
		if err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

type processSession struct {
	tool            domain.Tool
	config          Config
	command         *exec.Cmd
	cancel          context.CancelFunc
	stdin           io.Closer
	encoder         *protocol.Encoder
	decoder         *protocol.Decoder
	updates         chan interactive.Update
	waitDone        chan error
	done            chan struct{}
	doneOnce        sync.Once
	mu              sync.Mutex
	state           interactive.State
	outSequence     uint64
	inSequence      uint64
	lastSnapshot    uint64
	capabilities    map[string]bool
	hostRequests    map[string]bool
	protocol        protocol.VersionRange
	version         int
	terminalSent    bool
	shutdownStarted bool
	onTerminal      func()
}

var _ interactive.Session = (*processSession)(nil)

type readResult struct {
	message protocol.Message
	err     error
}

func (s *processSession) handshake(ctx context.Context) error {
	result := make(chan readResult, 1)
	go func() {
		message, err := s.decoder.Read()
		result <- readResult{message: message, err: err}
	}()
	timer := time.NewTimer(s.config.HandshakeTimeout)
	defer timer.Stop()

	var incoming readResult
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return errors.New("timeout no handshake da tool")
	case incoming = <-result:
	}
	if incoming.err != nil {
		return fmt.Errorf("handshake interrompido: %w", incoming.err)
	}
	if incoming.message.Method != protocol.MethodInitialized {
		return fmt.Errorf("handshake esperava initialized, recebeu %s", incoming.message.Method)
	}
	initialized, err := protocol.DecodePayload[protocol.Initialized](incoming.message)
	if err != nil {
		return err
	}
	allowed := protocol.VersionRange{
		Min: max(s.config.Protocol.Min, s.protocol.Min),
		Max: min(s.config.Protocol.Max, s.protocol.Max),
	}
	version, ok := protocol.Negotiate(allowed, initialized.Protocol)
	if !ok {
		return &interactive.IncompatibleError{
			EngineMin: s.config.Protocol.Min, EngineMax: s.config.Protocol.Max,
			ToolMin: initialized.Protocol.Min, ToolMax: initialized.Protocol.Max,
		}
	}
	if incoming.message.Version != version {
		return fmt.Errorf("initialized usa versão %d no envelope; negociada %d", incoming.message.Version, version)
	}
	if initialized.UIMode != protocol.UIModeScreenV1 {
		return fmt.Errorf("modo de UI incompatível: %s", initialized.UIMode)
	}
	if initialized.ToolVersion == "" {
		return errors.New("tool não informou sua versão")
	}
	if s.tool.Version != "" && initialized.ToolVersion != s.tool.Version {
		return fmt.Errorf("binário informou versão %s; manifest declara %s", initialized.ToolVersion, s.tool.Version)
	}
	if initialized.State != "ready" {
		return fmt.Errorf("tool não ficou pronta: estado %q", initialized.State)
	}
	activeCapabilities := make(map[string]bool, len(initialized.Capabilities))
	for _, capability := range initialized.Capabilities {
		if !s.capabilities[capability] {
			return fmt.Errorf("tool tentou usar capability não oferecida: %s", capability)
		}
		activeCapabilities[capability] = true
	}
	s.mu.Lock()
	s.inSequence = incoming.message.Sequence
	s.state = interactive.StateRunning
	s.version = version
	s.capabilities = activeCapabilities
	s.mu.Unlock()
	update := interactive.Update{State: interactive.StateRunning}
	if initialized.Snapshot != nil {
		snapshot := fromProtocolSnapshot(*initialized.Snapshot)
		if snapshot.Sequence == 0 {
			return errors.New("snapshot inicial sem sequência")
		}
		s.lastSnapshot = snapshot.Sequence
		update.Snapshot = &snapshot
	}
	s.publish(update)
	return nil
}

func (s *processSession) Updates() <-chan interactive.Update { return s.updates }

func (s *processSession) Send(ctx context.Context, event interactive.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.currentState() != interactive.StateRunning {
		return errors.New("sessão não está em execução")
	}
	return s.writeContext(ctx, protocol.MethodUIEvent, toProtocolEvent(event))
}

func (s *processSession) Respond(ctx context.Context, response interactive.HostResponse) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !s.consumeHostRequest(response.ID) {
		return errors.New("host response sem request pendente")
	}
	result := protocol.HostResponse{ID: response.ID, Result: response.Result}
	if response.Error != nil {
		result.Error = &protocol.Error{Code: response.Error.Code, Message: response.Error.Message, Fatal: response.Error.Fatal}
	}
	return s.writeContext(ctx, protocol.MethodHostResponse, result)
}

func (s *processSession) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	if s.shutdownStarted {
		s.mu.Unlock()
		return nil
	}
	s.shutdownStarted = true
	s.mu.Unlock()

	if err := s.writeContext(ctx, protocol.MethodShutdown, protocol.Shutdown{Reason: "screen closed"}); err != nil {
		return err
	}
	timer := time.NewTimer(s.config.ShutdownTimeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		s.forceStop()
		return ctx.Err()
	case <-timer.C:
		s.forceStop()
		return errors.New("timeout no shutdown da tool")
	case err := <-s.waitDone:
		s.markTerminal(interactive.StateStopped, normalizeExit(err, true))
		return nil
	}
}

func (s *processSession) readLoop() {
	for {
		message, err := s.decoder.Read()
		if err != nil {
			if s.isShuttingDown() {
				s.markTerminal(interactive.StateStopped, nil)
				return
			}
			s.markTerminal(interactive.StateFailed, fmt.Errorf("tool encerrou inesperadamente: %w", err))
			return
		}
		if !s.acceptSequence(message.Sequence) {
			continue
		}
		if message.Version != s.negotiatedVersion() {
			s.protocolFailure(fmt.Errorf("mensagem usa versão %d; sessão negociou %d", message.Version, s.negotiatedVersion()))
			return
		}
		switch message.Method {
		case protocol.MethodUISnapshot:
			snapshot, err := protocol.DecodePayload[protocol.Snapshot](message)
			if err != nil {
				s.protocolFailure(err)
				return
			}
			if snapshot.Sequence == 0 {
				s.protocolFailure(errors.New("snapshot sem sequência"))
				return
			}
			if !s.acceptSnapshot(snapshot.Sequence) {
				continue
			}
			converted := fromProtocolSnapshot(snapshot)
			s.publish(interactive.Update{State: interactive.StateRunning, Snapshot: &converted})

		case protocol.MethodHostRequest:
			request, err := protocol.DecodePayload[protocol.HostRequest](message)
			if err != nil {
				s.protocolFailure(err)
				return
			}
			if request.ID == "" || request.Method == "" {
				s.protocolFailure(errors.New("host request sem ID ou método"))
				return
			}
			if !s.capabilities[request.Method] {
				_ = s.write(protocol.MethodHostResponse, protocol.HostResponse{
					ID:    request.ID,
					Error: &protocol.Error{Code: "capability_denied", Message: "capability não negociada: " + request.Method},
				})
				continue
			}
			if !s.registerHostRequest(request.ID) {
				s.protocolFailure(fmt.Errorf("host request duplicada: %s", request.ID))
				return
			}
			s.publish(interactive.Update{State: interactive.StateRunning, HostRequest: &interactive.HostRequest{
				ID: request.ID, Method: request.Method, Params: request.Params,
			}})

		case protocol.MethodError:
			remote, decodeErr := protocol.DecodePayload[protocol.Error](message)
			if decodeErr != nil {
				s.protocolFailure(decodeErr)
				return
			}
			err := errors.New(remote.Message)
			s.publish(interactive.Update{State: interactive.StateRunning, Err: err})
			if remote.Fatal {
				s.forceStop()
				s.markTerminal(interactive.StateFailed, err)
				return
			}
		default:
			s.protocolFailure(fmt.Errorf("método inesperado da tool: %s", message.Method))
			return
		}
	}
}

func (s *processSession) protocolFailure(err error) {
	_ = s.write(protocol.MethodError, protocol.Error{Code: "protocol_error", Message: err.Error(), Fatal: true})
	s.forceStop()
	s.markTerminal(interactive.StateFailed, fmt.Errorf("erro de protocolo: %w", err))
}

func (s *processSession) write(method string, payload any) error {
	s.mu.Lock()
	s.outSequence++
	sequence := s.outSequence
	version := s.version
	s.mu.Unlock()
	message, err := protocol.NewMessage(version, sequence, method, payload)
	if err != nil {
		return err
	}
	return s.encoder.Write(message)
}

func (s *processSession) writeContext(ctx context.Context, method string, payload any) error {
	result := make(chan error, 1)
	go func() { result <- s.write(method, payload) }()
	select {
	case err := <-result:
		return err
	case <-ctx.Done():
		s.forceStop()
		return ctx.Err()
	case <-s.done:
		return errors.New("sessão encerrada")
	}
}

func (s *processSession) registerHostRequest(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hostRequests[id] {
		return false
	}
	s.hostRequests[id] = true
	return true
}

func (s *processSession) consumeHostRequest(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.hostRequests[id] {
		return false
	}
	delete(s.hostRequests, id)
	return true
}

func (s *processSession) negotiatedVersion() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.version
}

func (s *processSession) acceptSequence(sequence uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sequence <= s.inSequence {
		return false
	}
	s.inSequence = sequence
	return true
}

func (s *processSession) acceptSnapshot(sequence uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sequence <= s.lastSnapshot {
		return false
	}
	s.lastSnapshot = sequence
	return true
}

func (s *processSession) currentState() interactive.State {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

func (s *processSession) isShuttingDown() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.shutdownStarted
}

func (s *processSession) markTerminal(state interactive.State, err error) {
	s.mu.Lock()
	if s.terminalSent {
		s.mu.Unlock()
		return
	}
	s.terminalSent = true
	s.state = state
	onTerminal := s.onTerminal
	s.mu.Unlock()
	terminal := interactive.Update{State: state, Err: err}
	select {
	case s.updates <- terminal:
	default:
		// O estado terminal substitui um snapshot que a tela ainda não consumiu.
		select {
		case <-s.updates:
		default:
		}
		select {
		case s.updates <- terminal:
		default:
		}
	}
	s.stopPublishing()
	if onTerminal != nil {
		onTerminal()
	}
}

func (s *processSession) publish(update interactive.Update) {
	select {
	case s.updates <- update:
	case <-s.done:
	}
}

func (s *processSession) forceStop() {
	s.stopPublishing()
	s.cancel()
	_ = s.stdin.Close()
	if s.command.Process != nil {
		_ = s.command.Process.Kill()
	}
}

func (s *processSession) stopPublishing() {
	s.doneOnce.Do(func() { close(s.done) })
}

func (s *processSession) captureStderr(stderr io.Reader) {
	scanner := bufio.NewScanner(stderr)
	scanner.Buffer(make([]byte, 0, 8<<10), 256<<10)
	for scanner.Scan() {
		if s.config.Logger != nil {
			s.config.Logger.Info("log de tool", "tool", s.tool.ID, "message", scanner.Text())
		}
	}
}

func validateExecutable(installDir, executable string) error {
	if installDir == "" || executable == "" || !filepath.IsAbs(executable) {
		return errors.New("caminho do executável não foi resolvido pela instalação")
	}
	rel, err := filepath.Rel(installDir, executable)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return errors.New("executável escapa do diretório da instalação")
	}
	info, err := os.Stat(executable)
	if err != nil {
		return fmt.Errorf("executável instalado não encontrado: %w", err)
	}
	if !info.Mode().IsRegular() {
		return errors.New("executável instalado não é arquivo regular")
	}
	return nil
}

func normalizeExit(err error, graceful bool) error {
	if err == nil || graceful {
		return nil
	}
	return err
}

func toProtocolFrame(frame interactive.Frame) protocol.Frame {
	return protocol.Frame{Width: frame.Width, Height: frame.Height}
}

func toProtocolTheme(theme interactive.Theme) protocol.Theme {
	return protocol.Theme{
		Primary: theme.Primary, Secondary: theme.Secondary, Accent: theme.Accent,
		Success: theme.Success, Warning: theme.Warning, Danger: theme.Danger,
		Text: theme.Text, Muted: theme.Muted, Faint: theme.Faint,
		Border: theme.Border, Surface: theme.Surface,
	}
}

func toProtocolEvent(event interactive.Event) protocol.UIEvent {
	out := protocol.UIEvent{Type: string(event.Kind)}
	if event.Key != nil {
		out.Key = &protocol.KeyEvent{Code: event.Key.Code, Text: event.Key.Text, Ctrl: event.Key.Ctrl, Alt: event.Key.Alt, Shift: event.Key.Shift, Repeated: event.Key.Repeated}
	}
	if event.Paste != nil {
		out.Paste = &protocol.PasteEvent{Text: event.Paste.Text}
	}
	if event.Mouse != nil {
		out.Mouse = &protocol.MouseEvent{X: event.Mouse.X, Y: event.Mouse.Y, Button: event.Mouse.Button, Action: event.Mouse.Action, Ctrl: event.Mouse.Ctrl, Alt: event.Mouse.Alt, Shift: event.Mouse.Shift}
	}
	if event.Resize != nil {
		out.Resize = &protocol.ResizeEvent{Frame: toProtocolFrame(event.Resize.Frame).Clamp()}
	}
	if event.ThemeChanged != nil {
		out.ThemeChanged = &protocol.ThemeChangedEvent{Theme: toProtocolTheme(event.ThemeChanged.Theme)}
	}
	if event.Kind == interactive.EventFocus {
		out.Focus = &protocol.FocusEvent{}
	}
	if event.Kind == interactive.EventBlur {
		out.Blur = &protocol.BlurEvent{}
	}
	if event.Tick != nil {
		out.Tick = &protocol.TickEvent{UnixMilli: event.Tick.UnixMilli}
	}
	if event.Shutdown != nil {
		out.Shutdown = &protocol.ShutdownEvent{Reason: event.Shutdown.Reason}
	}
	return out
}

func fromProtocolSnapshot(snapshot protocol.Snapshot) interactive.Snapshot {
	hints := make([]interactive.Hint, len(snapshot.Hints))
	for i, hint := range snapshot.Hints {
		hints[i] = interactive.Hint{Key: hint.Key, Label: hint.Label}
	}
	out := interactive.Snapshot{
		Sequence: snapshot.Sequence, Body: snapshot.Body, Hints: hints,
		Status: snapshot.Status, Capturing: snapshot.Capturing,
	}
	if snapshot.Cursor != nil {
		out.Cursor = &interactive.Cursor{X: snapshot.Cursor.X, Y: snapshot.Cursor.Y, Visible: snapshot.Cursor.Visible, Shape: snapshot.Cursor.Shape}
	}
	return out
}
