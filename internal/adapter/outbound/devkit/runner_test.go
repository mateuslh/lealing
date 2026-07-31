package devkit

import (
	"bytes"
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	core "github.com/mateuslh/lealing/internal/core/devkit"
)

func TestFormatJSONFormataECompacta(t *testing.T) {
	formatted, err := FormatJSON("format", `{"n":9007199254740993,"ok":true}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(formatted.Body, "\n") || !strings.Contains(formatted.Body, "9007199254740993") {
		t.Errorf("formatação inesperada:\n%s", formatted.Body)
	}

	minified, err := FormatJSON("minify", formatted.Body)
	if err != nil {
		t.Fatal(err)
	}
	if minified.Body != `{"n":9007199254740993,"ok":true}` {
		t.Errorf("compactado = %s", minified.Body)
	}
}

func TestInspectJWTDecodificaClaimsSemValidarAssinatura(t *testing.T) {
	part := func(value string) string {
		return base64.RawURLEncoding.EncodeToString([]byte(value))
	}
	token := part(`{"alg":"RS256","typ":"JWT"}`) + "." +
		part(`{"iss":"https://id.example","sub":"user-42","exp":1800000000}`) + ".assinatura"

	result, err := InspectJWT("claims", token, time.Unix(1_700_000_000, 0))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Summary, "expira em") {
		t.Errorf("summary = %q", result.Summary)
	}
	if !strings.Contains(result.Warning, "não foi verificada") {
		t.Errorf("aviso ausente: %q", result.Warning)
	}
}

func TestCalculateCIDRIPv4(t *testing.T) {
	result, err := CalculateCIDR("10.42.16.25/20")
	if err != nil {
		t.Fatal(err)
	}
	assertRow(t, result, "Rede", "10.42.16.0/20")
	assertRow(t, result, "Broadcast", "10.42.31.255")
	assertRow(t, result, "Hosts úteis", "4094")
}

func TestCodecsFazemRoundTrip(t *testing.T) {
	encoded, err := TransformCodec("b64url-encode", "arquitetura/ação")
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := TransformCodec("b64url-decode", encoded.Body)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.Body != "arquitetura/ação" {
		t.Errorf("round trip = %q", decoded.Body)
	}
}

func TestChecksumConhecido(t *testing.T) {
	result, err := Checksum("sha256", "abc")
	if err != nil {
		t.Fatal(err)
	}
	const want = "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"
	if result.Body != want {
		t.Errorf("SHA-256 = %s", result.Body)
	}
}

func TestGenerateUUIDsMarcaVersaoEVariante(t *testing.T) {
	random := bytes.NewReader(make([]byte, 64))
	result, err := GenerateUUIDs("v7", "2", time.UnixMilli(1_700_000_000_123), random)
	if err != nil {
		t.Fatal(err)
	}
	values := strings.Split(result.Body, "\n")
	if len(values) != 2 {
		t.Fatalf("UUIDs = %d, quero 2", len(values))
	}
	for _, value := range values {
		if len(value) != 36 || value[14] != '7' || value[19] != '8' {
			t.Errorf("UUID v7 inválido: %s", value)
		}
	}
}

func TestHTTPProbeLimitaCorpoERemoveControles(t *testing.T) {
	body := "\x1b[31m" + strings.Repeat("x", maxHTTPBody+100)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(body))
	}))
	defer server.Close()

	runner := New()
	result, err := runner.Run(context.Background(), core.Request{
		Tool: core.ToolHTTP, Mode: http.MethodGet, Input: server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(result.Body, "\x1b") {
		t.Fatal("resposta preservou sequência de controle")
	}
	if result.Warning == "" || len(result.Body) > maxHTTPBody {
		t.Errorf("limite não aplicado: len=%d aviso=%q", len(result.Body), result.Warning)
	}
}

func TestTLSInspectorLeCertificadoMesmoAutoassinado(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()

	runner := New()
	result, err := runner.Run(context.Background(), core.Request{
		Tool: core.ToolNetwork, Mode: "tls", Input: server.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Title != "TLS inspecionado" {
		t.Errorf("title = %q", result.Title)
	}
	assertRow(t, result, "Versão", "TLS 1.3")
}

func assertRow(t *testing.T, result core.Result, label, want string) {
	t.Helper()
	for _, row := range result.Rows {
		if row.Label == label {
			if row.Value != want {
				t.Errorf("%s = %q, quero %q", label, row.Value, want)
			}
			return
		}
	}
	t.Errorf("linha %q ausente em %+v", label, result.Rows)
}
