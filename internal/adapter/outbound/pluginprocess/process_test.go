package pluginprocess_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mateuslh/lealing/internal/adapter/outbound/pluginprocess"
	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/interactive"
	"github.com/mateuslh/lealing/sdk/protocol"
)

func TestMain(m *testing.M) {
	name := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	if strings.HasPrefix(name, "helper-") {
		os.Exit(runHelper(strings.TrimPrefix(name, "helper-")))
	}
	os.Exit(m.Run())
}

// runHelper transforma uma cópia do próprio binário de teste numa tool Go
// hermética. Não há shell, rede nem executável externo na suíte.
func runHelper(behavior string) int {
	if behavior == "timeout" {
		time.Sleep(30 * time.Second)
		return 0
	}
	decoder := protocol.NewDecoder(os.Stdin)
	encoder := protocol.NewEncoder(os.Stdout)
	initializeMessage, err := decoder.Read()
	if err != nil || initializeMessage.Method != protocol.MethodInitialize {
		return 20
	}
	initialize, err := protocol.DecodePayload[protocol.Initialize](initializeMessage)
	if err != nil {
		return 21
	}
	versions := protocol.VersionRange{Min: 1, Max: 1}
	if behavior == "incompatible" {
		versions = protocol.VersionRange{Min: 2, Max: 2}
	}
	first := protocol.Snapshot{Sequence: 1, Body: "primeiro", Hints: []protocol.Hint{{Key: "esc", Label: "voltar"}}}
	initialized := protocol.Initialized{
		Protocol: versions, ToolVersion: "1.0.0", UIMode: protocol.UIModeScreenV1,
		State: "ready", Snapshot: &first,
	}
	if (behavior == "event" || behavior == "host") && len(initialize.Capabilities) > 0 {
		initialized.Capabilities = []string{initialize.Capabilities[0]}
	}
	if err := writeHelper(encoder, 1, protocol.MethodInitialized, initialized); err != nil {
		return 22
	}
	if behavior == "incompatible" {
		return 0
	}
	if behavior == "crash" {
		return 7
	}
	if behavior == "partial" {
		io.WriteString(os.Stdout, "Content-Length: 100\r\n\r\n{\"version\":1")
		return 8
	}
	if behavior == "stale" {
		_ = writeHelper(encoder, 2, protocol.MethodUISnapshot, protocol.Snapshot{Sequence: 10, Body: "dez"})
		_ = writeHelper(encoder, 3, protocol.MethodUISnapshot, protocol.Snapshot{Sequence: 9, Body: "velho"})
		_ = writeHelper(encoder, 4, protocol.MethodUISnapshot, protocol.Snapshot{Sequence: 11, Body: "onze"})
	}
	if behavior == "denied" {
		request := protocol.HostRequest{ID: "request-1", Method: protocol.CapabilityClipboardWrite}
		if err := writeHelper(encoder, 2, protocol.MethodHostRequest, request); err != nil {
			return 23
		}
		responseMessage, err := decoder.Read()
		if err != nil {
			return 24
		}
		response, err := protocol.DecodePayload[protocol.HostResponse](responseMessage)
		if err != nil || response.Error == nil || response.Error.Code != "capability_denied" {
			return 25
		}
		_ = writeHelper(encoder, 3, protocol.MethodUISnapshot, protocol.Snapshot{Sequence: 2, Body: "negada"})
	}
	if behavior == "host" {
		request := protocol.HostRequest{ID: "request-1", Method: protocol.CapabilityNotificationShow, Params: []byte(`{"message":"olá"}`)}
		if err := writeHelper(encoder, 2, protocol.MethodHostRequest, request); err != nil {
			return 26
		}
		responseMessage, err := decoder.Read()
		if err != nil || responseMessage.Method != protocol.MethodHostResponse {
			return 27
		}
		response, err := protocol.DecodePayload[protocol.HostResponse](responseMessage)
		if err != nil || response.ID != request.ID || response.Error != nil {
			return 28
		}
		_ = writeHelper(encoder, 3, protocol.MethodUISnapshot, protocol.Snapshot{Sequence: 2, Body: "host respondeu"})
	}

	sequence := uint64(10)
	for {
		message, err := decoder.Read()
		if err != nil {
			return 0
		}
		switch message.Method {
		case protocol.MethodUIEvent:
			sequence++
			_ = writeHelper(encoder, sequence, protocol.MethodUISnapshot, protocol.Snapshot{Sequence: sequence, Body: "evento recebido"})
		case protocol.MethodShutdown:
			if behavior == "ignore-shutdown" {
				continue
			}
			return 0
		}
	}
}

func writeHelper(encoder *protocol.Encoder, sequence uint64, method string, payload any) error {
	message, err := protocol.NewMessage(1, sequence, method, payload)
	if err != nil {
		return err
	}
	return encoder.Write(message)
}

func helperExecutable(t testing.TB, behavior string) (string, string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	name := "helper-" + behavior
	if strings.HasSuffix(strings.ToLower(self), ".exe") {
		name += ".exe"
	}
	destination := filepath.Join(dir, name)
	in, err := os.Open(self)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o700)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	return dir, destination
}

func runtime(t testing.TB, behavior string, capabilities ...string) (*pluginprocess.Runtime, domain.Tool, interactive.StartOptions) {
	t.Helper()
	dir, executable := helperExecutable(t, behavior)
	tool := domain.Tool{
		ID: "helper", Name: "Helper", Category: "ai", Kind: domain.KindProcess,
		Runtime: &domain.ExternalRuntime{
			InstallDir: dir, Executable: executable,
			ProtocolMin: 1, ProtocolMax: 1, UIMode: protocol.UIModeScreenV1,
		},
	}
	handshakeTimeout := 2 * time.Second
	shutdownTimeout := 2 * time.Second
	if behavior == "timeout" {
		handshakeTimeout = 200 * time.Millisecond
	}
	if behavior == "ignore-shutdown" {
		shutdownTimeout = 200 * time.Millisecond
	}
	r := pluginprocess.New(pluginprocess.Config{
		Protocol:         protocol.VersionRange{Min: 1, Max: 1},
		HandshakeTimeout: handshakeTimeout,
		ShutdownTimeout:  shutdownTimeout,
	})
	options := interactive.StartOptions{
		EngineVersion: "test", Platform: "darwin", Architecture: "arm64",
		Frame: interactive.Frame{Width: 80, Height: 20}, Capabilities: capabilities,
		DataDir: t.TempDir(), CacheDir: t.TempDir(),
	}
	return r, tool, options
}

func nextUpdate(t *testing.T, session interactive.Session) interactive.Update {
	t.Helper()
	select {
	case update := <-session.Updates():
		return update
	case <-time.After(3 * time.Second):
		t.Fatal("sessão não publicou update")
		return interactive.Update{}
	}
}

func TestHandshakeCompativelEEventoAteSnapshot(t *testing.T) {
	r, tool, options := runtime(t, "event", interactive.CapabilityNavigationBack)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session, err := r.Start(ctx, tool, options)
	if err != nil {
		t.Fatal(err)
	}
	first := nextUpdate(t, session)
	if first.State != interactive.StateRunning || first.Snapshot == nil || first.Snapshot.Body != "primeiro" {
		t.Fatalf("primeiro update = %+v", first)
	}
	if err := session.Send(context.Background(), interactive.Event{Kind: interactive.EventKey, Key: &interactive.KeyEvent{Code: "enter"}}); err != nil {
		t.Fatal(err)
	}
	got := nextUpdate(t, session)
	if got.Snapshot == nil || got.Snapshot.Body != "evento recebido" {
		t.Fatalf("snapshot = %+v", got)
	}
	if err := session.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestHandshakeIncompativelTrazAsDuasVersoes(t *testing.T) {
	r, tool, options := runtime(t, "incompatible")
	_, err := r.Start(context.Background(), tool, options)
	var incompatible *interactive.IncompatibleError
	if !errors.As(err, &incompatible) {
		t.Fatalf("erro = %v", err)
	}
	if !strings.Contains(err.Error(), "engine suporta 1–1") || !strings.Contains(err.Error(), "tool suporta 2–2") {
		t.Errorf("mensagem não é humana: %v", err)
	}
}

func TestTimeoutDeHandshake(t *testing.T) {
	r, tool, options := runtime(t, "timeout")
	_, err := r.Start(context.Background(), tool, options)
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("erro = %v", err)
	}
}

func TestSnapshotForaDeOrdemEIgnorado(t *testing.T) {
	r, tool, options := runtime(t, "stale")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session, err := r.Start(ctx, tool, options)
	if err != nil {
		t.Fatal(err)
	}
	_ = nextUpdate(t, session) // inicial
	first := nextUpdate(t, session)
	second := nextUpdate(t, session)
	if first.Snapshot == nil || second.Snapshot == nil || first.Snapshot.Sequence != 10 || second.Snapshot.Sequence != 11 {
		t.Fatalf("snapshots = %+v / %+v", first, second)
	}
	if first.Snapshot.Body == "velho" || second.Snapshot.Body == "velho" {
		t.Fatal("snapshot obsoleto chegou à engine")
	}
}

func TestCrashEFrameParcialNaoDerrubamEngine(t *testing.T) {
	for _, behavior := range []string{"crash", "partial"} {
		t.Run(behavior, func(t *testing.T) {
			r, tool, options := runtime(t, behavior)
			session, err := r.Start(context.Background(), tool, options)
			if err != nil {
				t.Fatal(err)
			}
			_ = nextUpdate(t, session)
			failed := nextUpdate(t, session)
			if failed.State != interactive.StateFailed || failed.Err == nil {
				t.Fatalf("update = %+v", failed)
			}
		})
	}
}

func TestCancelamentoEncerraProcesso(t *testing.T) {
	r, tool, options := runtime(t, "event")
	ctx, cancel := context.WithCancel(context.Background())
	session, err := r.Start(ctx, tool, options)
	if err != nil {
		t.Fatal(err)
	}
	_ = nextUpdate(t, session)
	cancel()
	failed := nextUpdate(t, session)
	if failed.State != interactive.StateFailed {
		t.Fatalf("estado = %s", failed.State)
	}
}

func TestShutdownGracioso(t *testing.T) {
	r, tool, options := runtime(t, "event")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session, err := r.Start(ctx, tool, options)
	if err != nil {
		t.Fatal(err)
	}
	_ = nextUpdate(t, session)
	if err := session.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	stopped := nextUpdate(t, session)
	if stopped.State != interactive.StateStopped {
		t.Fatalf("estado = %s (%v)", stopped.State, stopped.Err)
	}
}

func TestShutdownForcaToolQueNaoCoopera(t *testing.T) {
	r, tool, options := runtime(t, "ignore-shutdown")
	session, err := r.Start(context.Background(), tool, options)
	if err != nil {
		t.Fatal(err)
	}
	_ = nextUpdate(t, session)
	if err := session.Shutdown(context.Background()); err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("shutdown = %v", err)
	}
}

func TestCapabilityNaoNegociadaERecusadaNoRuntime(t *testing.T) {
	r, tool, options := runtime(t, "denied", interactive.CapabilityNavigationBack)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session, err := r.Start(ctx, tool, options)
	if err != nil {
		t.Fatal(err)
	}
	_ = nextUpdate(t, session)
	update := nextUpdate(t, session)
	if update.HostRequest != nil {
		t.Fatal("request sem capability chegou à TUI")
	}
	if update.Snapshot == nil || update.Snapshot.Body != "negada" {
		t.Fatalf("helper não recebeu a recusa: %+v", update)
	}
}

func TestHostRequestEHostResponseNegociados(t *testing.T) {
	r, tool, options := runtime(t, "host", interactive.CapabilityNotificationShow)
	session, err := r.Start(context.Background(), tool, options)
	if err != nil {
		t.Fatal(err)
	}
	_ = nextUpdate(t, session)
	request := nextUpdate(t, session)
	if request.HostRequest == nil || request.HostRequest.ID != "request-1" {
		t.Fatalf("request = %+v", request)
	}
	if err := session.Respond(context.Background(), interactive.HostResponse{ID: request.HostRequest.ID, Result: []byte(`{"ok":true}`)}); err != nil {
		t.Fatal(err)
	}
	snapshot := nextUpdate(t, session)
	if snapshot.Snapshot == nil || snapshot.Snapshot.Body != "host respondeu" {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	_ = session.Shutdown(context.Background())
}

func TestManifestIncompativelNaoIniciaExecutavel(t *testing.T) {
	r := pluginprocess.New(pluginprocess.Config{Protocol: protocol.VersionRange{Min: 1, Max: 1}})
	tool := domain.Tool{ID: "future", Kind: domain.KindProcess, Runtime: &domain.ExternalRuntime{
		InstallDir: t.TempDir(), Executable: filepath.Join(t.TempDir(), "inexistente"),
		ProtocolMin: 2, ProtocolMax: 2, UIMode: protocol.UIModeScreenV1,
	}}
	_, err := r.Start(context.Background(), tool, interactive.StartOptions{})
	var incompatible *interactive.IncompatibleError
	if !errors.As(err, &incompatible) {
		t.Fatalf("erro = %v", err)
	}
}

func TestExecutablePrecisaFicarDentroDaInstalacao(t *testing.T) {
	r := pluginprocess.New(pluginprocess.Config{})
	tool := domain.Tool{
		ID: "escape", Kind: domain.KindProcess,
		Runtime: &domain.ExternalRuntime{InstallDir: t.TempDir(), Executable: os.Args[0], UIMode: protocol.UIModeScreenV1, ProtocolMin: 1, ProtocolMax: 1},
	}
	if _, err := r.Start(context.Background(), tool, interactive.StartOptions{}); err == nil {
		t.Fatal("runtime aceitou executable fora da instalação")
	}
}

func BenchmarkTeclaAteSnapshot(b *testing.B) {
	if testing.Short() {
		b.Skip("benchmark de processo")
	}
	// O custo inclui framing e a ida pelo processo auxiliar; não há limite de
	// CI, apenas uma linha de base reproduzível para decisões futuras.
	r, tool, options := runtime(b, "event")
	session, err := r.Start(context.Background(), tool, options)
	if err != nil {
		b.Fatal(err)
	}
	<-session.Updates()
	b.Cleanup(func() { _ = session.Shutdown(context.Background()) })
	b.ResetTimer()
	for b.Loop() {
		if err := session.Send(context.Background(), interactive.Event{Kind: interactive.EventKey, Key: &interactive.KeyEvent{Code: "x", Text: "x"}}); err != nil {
			b.Fatal(err)
		}
		<-session.Updates()
	}
}
