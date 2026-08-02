package githubauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mateuslh/lealing/internal/core/usersync"
)

// server monta os três endpoints do fluxo com respostas roteirizadas. O
// token nunca sai de aqui: nenhum teste toca a rede de verdade.
func server(t *testing.T, device string, tokens []string, identity string) *httptest.Server {
	t.Helper()
	attempt := 0
	handler := http.NewServeMux()
	handler.HandleFunc("/device", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(device))
	})
	handler.HandleFunc("/token", func(w http.ResponseWriter, _ *http.Request) {
		body := tokens[min(attempt, len(tokens)-1)]
		attempt++
		_, _ = w.Write([]byte(body))
	})
	handler.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t0ken" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(identity))
	})
	return httptest.NewServer(handler)
}

func client(t *testing.T, base string) *Client {
	t.Helper()
	return New(Config{
		ClientID: "Iv1.teste", DeviceURL: base + "/device",
		TokenURL: base + "/token", IdentityURL: base + "/user",
		MinInterval: time.Millisecond,
	})
}

func TestSemClientIDORecursoFicaDesligado(t *testing.T) {
	empty := New(Config{})
	if _, err := empty.RequestDevice(context.Background()); err != ErrNotConfigured {
		t.Fatalf("RequestDevice = %v", err)
	}
	if _, err := empty.Wait(context.Background(), usersync.DeviceCode{}); err != ErrNotConfigured {
		t.Fatalf("Wait = %v", err)
	}
}

func TestFluxoCompletoEsperaAprovacao(t *testing.T) {
	remote := server(t,
		`{"device_code":"dev","user_code":"ABCD-1234","verification_uri":"https://github.com/login/device","expires_in":900,"interval":1}`,
		[]string{
			`{"error":"authorization_pending"}`,
			`{"error":"slow_down","interval":1}`,
			`{"access_token":"t0ken","scope":"repo"}`,
		},
		`{"login":"alguem","name":"Alguém"}`)
	defer remote.Close()

	auth := client(t, remote.URL)
	code, err := auth.RequestDevice(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if code.UserCode != "ABCD-1234" || code.Code != "dev" {
		t.Fatalf("código = %+v", code)
	}
	// O intervalo do servidor é respeitado, mas o piso do teste evita que a
	// suíte espere segundos de verdade.
	code.Interval = time.Millisecond

	credential, err := auth.Wait(context.Background(), code)
	if err != nil {
		t.Fatal(err)
	}
	if credential.Token != "t0ken" || credential.Scope != "repo" {
		t.Fatalf("credencial = %+v", credential)
	}

	identity, err := auth.Identity(context.Background(), credential)
	if err != nil {
		t.Fatal(err)
	}
	if identity.Login != "alguem" || identity.Name != "Alguém" {
		t.Fatalf("identidade = %+v", identity)
	}
}

func TestErrosDoFluxoViramMensagemAcionavel(t *testing.T) {
	for name, expected := range map[string]string{
		`{"error":"access_denied"}`:        "negada",
		`{"error":"expired_token"}`:        "expirou",
		`{"error":"device_flow_disabled"}`: "device flow",
	} {
		t.Run(expected, func(t *testing.T) {
			remote := server(t, `{"device_code":"d","user_code":"U","interval":1}`, []string{name}, "{}")
			defer remote.Close()

			auth := client(t, remote.URL)
			_, err := auth.Wait(context.Background(), usersync.DeviceCode{
				Code: "d", Interval: time.Millisecond, ExpiresAt: time.Now().Add(time.Minute),
			})
			if err == nil || !strings.Contains(err.Error(), expected) {
				t.Fatalf("Wait = %v, quero mencionar %q", err, expected)
			}
		})
	}
}

func TestCodigoExpiradoNaoFicaEmPollingEterno(t *testing.T) {
	remote := server(t, "{}", []string{`{"error":"authorization_pending"}`}, "{}")
	defer remote.Close()

	_, err := client(t, remote.URL).Wait(context.Background(), usersync.DeviceCode{
		Code: "d", Interval: time.Millisecond, ExpiresAt: time.Now().Add(-time.Second),
	})
	if err == nil || !strings.Contains(err.Error(), "expirou") {
		t.Fatalf("Wait = %v", err)
	}
}

func TestCancelamentoInterrompeOPolling(t *testing.T) {
	remote := server(t, "{}", []string{`{"error":"authorization_pending"}`}, "{}")
	defer remote.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := client(t, remote.URL).Wait(ctx, usersync.DeviceCode{
		Code: "d", Interval: time.Millisecond, ExpiresAt: time.Now().Add(time.Minute),
	})
	if err == nil {
		t.Fatal("Wait ignorou o cancelamento")
	}
}
