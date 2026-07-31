package protocol_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/mateuslh/lealing/sdk/protocol"
)

type oneByteReader struct{ r io.Reader }

func (r oneByteReader) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return r.r.Read(p)
}

func message(t *testing.T, seq uint64, method string, payload any) protocol.Message {
	t.Helper()
	m, err := protocol.NewMessage(protocol.Version1, seq, method, payload)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestEncodeDecodeDeTodasAsMensagens(t *testing.T) {
	snapshot := protocol.Snapshot{Sequence: 4, Body: "\x1b[1mhello\x1b[0m", Hints: []protocol.Hint{{Key: "esc", Label: "voltar"}}}
	cases := []struct {
		method  string
		payload any
	}{
		{protocol.MethodInitialize, protocol.Initialize{Protocol: protocol.VersionRange{Min: 1, Max: 1}, ToolID: "demo"}},
		{protocol.MethodInitialized, protocol.Initialized{Protocol: protocol.VersionRange{Min: 1, Max: 1}, UIMode: protocol.UIModeScreenV1}},
		{protocol.MethodUIEvent, protocol.UIEvent{Type: protocol.EventKey, Key: &protocol.KeyEvent{Code: "keyA", Text: "a"}}},
		{protocol.MethodUISnapshot, snapshot},
		{protocol.MethodHostRequest, protocol.HostRequest{ID: "r1", Method: protocol.CapabilityNotificationShow}},
		{protocol.MethodHostResponse, protocol.HostResponse{ID: "r1", Result: []byte(`{"ok":true}`)}},
		{protocol.MethodShutdown, protocol.Shutdown{Reason: "teste"}},
		{protocol.MethodError, protocol.Error{Code: "boom", Message: "falhou"}},
	}

	var stream bytes.Buffer
	enc := protocol.NewEncoder(&stream)
	for i, tc := range cases {
		if err := enc.Write(message(t, uint64(i+1), tc.method, tc.payload)); err != nil {
			t.Fatalf("%s: %v", tc.method, err)
		}
	}

	dec := protocol.NewDecoder(&stream)
	for i, want := range cases {
		got, err := dec.Read()
		if err != nil {
			t.Fatalf("mensagem %d: %v", i, err)
		}
		if got.Method != want.method || got.Sequence != uint64(i+1) {
			t.Errorf("mensagem %d = %+v", i, got)
		}
	}
}

func TestFramingAceitaMensagemParcial(t *testing.T) {
	var stream bytes.Buffer
	if err := protocol.NewEncoder(&stream).Write(message(t, 1, protocol.MethodShutdown, protocol.Shutdown{})); err != nil {
		t.Fatal(err)
	}
	got, err := protocol.NewDecoder(oneByteReader{r: &stream}).Read()
	if err != nil {
		t.Fatal(err)
	}
	if got.Method != protocol.MethodShutdown {
		t.Errorf("método = %q", got.Method)
	}
}

func TestFramingLeMultiplasMensagensDoMesmoBuffer(t *testing.T) {
	var stream bytes.Buffer
	enc := protocol.NewEncoder(&stream)
	for i := 1; i <= 3; i++ {
		if err := enc.Write(message(t, uint64(i), protocol.MethodUIEvent, protocol.UIEvent{Type: protocol.EventTick})); err != nil {
			t.Fatal(err)
		}
	}
	dec := protocol.NewDecoder(&stream)
	for i := 1; i <= 3; i++ {
		got, err := dec.Read()
		if err != nil || got.Sequence != uint64(i) {
			t.Fatalf("mensagem %d: %+v, %v", i, got, err)
		}
	}
}

func TestFramingRecusaPayloadAcimaDoLimite(t *testing.T) {
	var stream bytes.Buffer
	enc := protocol.NewEncoderSize(&stream, 64)
	err := enc.Write(message(t, 1, protocol.MethodUISnapshot, protocol.Snapshot{Body: strings.Repeat("x", 100)}))
	if !errors.Is(err, protocol.ErrFrameTooLarge) {
		t.Fatalf("erro = %v", err)
	}

	raw := "Content-Length: 100\r\n\r\n"
	_, err = protocol.NewDecoderSize(strings.NewReader(raw), 64).Read()
	if !errors.Is(err, protocol.ErrFrameTooLarge) {
		t.Fatalf("decoder: %v", err)
	}
}

func TestFramingRecusaJSONInvalido(t *testing.T) {
	raw := "{não-json}"
	frame := "Content-Length: 10\r\n\r\n" + raw
	_, err := protocol.NewDecoder(strings.NewReader(frame)).Read()
	if !errors.Is(err, protocol.ErrInvalidFrame) {
		t.Fatalf("erro = %v", err)
	}
}

func TestFramingRecusaJSONAdicionalNoMesmoPayload(t *testing.T) {
	body := `{"version":1,"sequence":1,"method":"shutdown"}{}`
	frame := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
	_, err := protocol.NewDecoder(strings.NewReader(frame)).Read()
	if !errors.Is(err, protocol.ErrInvalidFrame) {
		t.Fatalf("erro = %v", err)
	}
}

func TestFramingReportaProcessoEncerradoNoMeioDaMensagem(t *testing.T) {
	frame := "Content-Length: 100\r\n\r\n{\"version\":1}"
	_, err := protocol.NewDecoder(strings.NewReader(frame)).Read()
	if err == nil || !strings.Contains(err.Error(), "corpo incompleto") {
		t.Fatalf("erro = %v", err)
	}
}

func TestNegociacaoCompativelEIncompativel(t *testing.T) {
	if got, ok := protocol.Negotiate(protocol.VersionRange{Min: 1, Max: 3}, protocol.VersionRange{Min: 2, Max: 4}); !ok || got != 3 {
		t.Fatalf("negociação = %d, %v", got, ok)
	}
	if _, ok := protocol.Negotiate(protocol.VersionRange{Min: 1, Max: 1}, protocol.VersionRange{Min: 2, Max: 2}); ok {
		t.Fatal("intervalos incompatíveis foram aceitos")
	}
}

func TestEventosCobremContratoV1(t *testing.T) {
	events := []protocol.UIEvent{
		{Type: protocol.EventKey, Key: &protocol.KeyEvent{Code: "enter"}},
		{Type: protocol.EventPaste, Paste: &protocol.PasteEvent{Text: "abc"}},
		{Type: protocol.EventMouse, Mouse: &protocol.MouseEvent{X: 1, Y: 2, Button: "left"}},
		{Type: protocol.EventResize, Resize: &protocol.ResizeEvent{Frame: protocol.Frame{Width: 80, Height: 20}}},
		{Type: protocol.EventThemeChanged, ThemeChanged: &protocol.ThemeChangedEvent{}},
		{Type: protocol.EventFocus, Focus: &protocol.FocusEvent{}},
		{Type: protocol.EventBlur, Blur: &protocol.BlurEvent{}},
		{Type: protocol.EventTick, Tick: &protocol.TickEvent{UnixMilli: 1}},
		{Type: protocol.EventShutdown, Shutdown: &protocol.ShutdownEvent{Reason: "fim"}},
	}
	for i, event := range events {
		var stream bytes.Buffer
		if err := protocol.NewEncoder(&stream).Write(message(t, uint64(i+1), protocol.MethodUIEvent, event)); err != nil {
			t.Fatal(err)
		}
		got, err := protocol.NewDecoder(&stream).Read()
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := protocol.DecodePayload[protocol.UIEvent](got)
		if err != nil || decoded.Type != event.Type {
			t.Errorf("evento %s: %+v, %v", event.Type, decoded, err)
		}
	}
}
