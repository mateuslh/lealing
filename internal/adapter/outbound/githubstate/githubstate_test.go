package githubstate

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mateuslh/lealing/internal/core/usersync"
)

const login = "alguem"

type remote struct {
	*httptest.Server
	// repository nil significa que ele ainda não existe.
	repository map[string]any
	content    string
	sha        string
	created    bool
	lastBody   map[string]any
}

func newRemote(t *testing.T) *remote {
	t.Helper()
	r := &remote{}
	handler := http.NewServeMux()

	handler.HandleFunc("/user", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"login":"` + login + `"}`))
	})
	handler.HandleFunc("/user/repos", func(w http.ResponseWriter, req *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(req.Body).Decode(&body)
		r.created = true
		r.repository = map[string]any{
			"name": body["name"], "private": body["private"],
			"owner": map[string]any{"login": login}, "html_url": "https://github.test/x",
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(r.repository)
	})
	handler.HandleFunc("/repos/"+login+"/lealing-state", func(w http.ResponseWriter, _ *http.Request) {
		if r.repository == nil {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(r.repository)
	})
	handler.HandleFunc("/repos/"+login+"/lealing-state/contents/state.json",
		func(w http.ResponseWriter, req *http.Request) {
			if req.Method == http.MethodPut {
				var body map[string]any
				_ = json.NewDecoder(req.Body).Decode(&body)
				r.lastBody = body
				if sha, ok := body["sha"].(string); ok && sha != r.sha {
					w.WriteHeader(http.StatusConflict)
					return
				}
				r.content, r.sha = body["content"].(string), "sha-nova"
				_ = json.NewEncoder(w).Encode(map[string]any{
					"content": map[string]any{"sha": r.sha},
				})
				return
			}
			if r.content == "" {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"content": r.content, "sha": r.sha, "encoding": "base64",
			})
		})

	r.Server = httptest.NewServer(handler)
	t.Cleanup(r.Close)
	return r
}

func store(r *remote) *Store {
	return New(Config{Client: r.Client(), BaseURL: r.URL})
}

func credential() usersync.Credential { return usersync.Credential{Token: "t0ken"} }

func TestEnsureCriaORepositorioPrivadoNaPrimeiraVez(t *testing.T) {
	r := newRemote(t)
	repository, err := store(r).Ensure(context.Background(), credential(), usersync.DefaultRepository)
	if err != nil {
		t.Fatal(err)
	}
	if !r.created || !repository.Created || !repository.Private {
		t.Fatalf("repositório = %+v (criado=%v)", repository, r.created)
	}
	if repository.FullName() != login+"/"+usersync.DefaultRepository {
		t.Fatalf("nome completo = %s", repository.FullName())
	}
}

// Preferências em repositório público seriam um vazamento silencioso.
func TestEnsureRecusaRepositorioPublico(t *testing.T) {
	r := newRemote(t)
	r.repository = map[string]any{
		"name": usersync.DefaultRepository, "private": false,
		"owner": map[string]any{"login": login},
	}
	_, err := store(r).Ensure(context.Background(), credential(), usersync.DefaultRepository)
	if err == nil || !strings.Contains(err.Error(), "público") {
		t.Fatalf("Ensure = %v", err)
	}
}

func TestReadSemArquivoNaoEErro(t *testing.T) {
	r := newRemote(t)
	r.repository = map[string]any{"name": usersync.DefaultRepository, "private": true}

	snapshot, err := store(r).Read(context.Background(), credential(), usersync.DefaultRepository)
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Missing {
		t.Fatalf("snapshot = %+v, quero Missing", snapshot)
	}
}

func TestWriteEDepoisReadDevolvemOMesmoDocumento(t *testing.T) {
	r := newRemote(t)
	r.repository = map[string]any{"name": usersync.DefaultRepository, "private": true}
	target := store(r)

	state := usersync.State{
		Version: usersync.StateVersion, UpdatedAt: time.Now().UTC(), Device: "mac-de-teste",
		Usage: []usersync.ToolUsage{{Host: "lealing", ID: "example-tool", Runs: 2, Favorite: true}},
	}
	revision, err := target.Write(context.Background(), credential(), usersync.DefaultRepository, state, "")
	if err != nil {
		t.Fatal(err)
	}
	if revision == "" {
		t.Fatal("Write não devolveu revisão")
	}

	snapshot, err := target.Read(context.Background(), credential(), usersync.DefaultRepository)
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.State.Usage) != 1 || snapshot.State.Usage[0].ID != "example-tool" {
		t.Fatalf("estado lido = %+v", snapshot.State)
	}
	if snapshot.Revision != revision {
		t.Fatalf("revisão = %q, quero %q", snapshot.Revision, revision)
	}
}

func TestWriteRecusaQuandoARevisaoEsperadaEnvelheceu(t *testing.T) {
	r := newRemote(t)
	r.repository = map[string]any{"name": usersync.DefaultRepository, "private": true}
	r.content = base64.StdEncoding.EncodeToString([]byte(`{"version":3,"updatedAt":"2026-08-02T12:00:00Z"}`))
	r.sha = "atual"

	_, err := store(r).Write(context.Background(), credential(), usersync.DefaultRepository,
		usersync.State{Version: usersync.StateVersion, UpdatedAt: time.Now().UTC()}, "antiga")
	if !errors.Is(err, usersync.ErrConflict) {
		t.Fatalf("Write = %v, quero conflito", err)
	}
}

func TestReadRecusaBase64Corrompido(t *testing.T) {
	r := newRemote(t)
	r.repository = map[string]any{"name": usersync.DefaultRepository, "private": true}
	r.content, r.sha = "não é base64 %%%", "sha"

	if _, err := store(r).Read(context.Background(), credential(), usersync.DefaultRepository); err == nil {
		t.Fatal("conteúdo corrompido foi aceito")
	}
}

func TestReadRecusaVersaoAntigaECampoDesconhecido(t *testing.T) {
	for name, document := range map[string]string{
		"versão antiga":      `{"version":1,"updatedAt":"2026-08-02T12:00:00Z"}`,
		"campo desconhecido": `{"version":3,"updatedAt":"2026-08-02T12:00:00Z","extra":true}`,
		"campo duplicado":    `{"version":3,"version":3,"updatedAt":"2026-08-02T12:00:00Z"}`,
	} {
		t.Run(name, func(t *testing.T) {
			r := newRemote(t)
			r.repository = map[string]any{"name": usersync.DefaultRepository, "private": true}
			r.content = base64.StdEncoding.EncodeToString([]byte(document))
			if _, err := store(r).Read(context.Background(), credential(), usersync.DefaultRepository); err == nil {
				t.Fatal("documento incompatível foi aceito")
			}
		})
	}
}

func TestTokenInvalidoViraFaltaDeConta(t *testing.T) {
	handler := http.NewServeMux()
	handler.HandleFunc("/user", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	server := httptest.NewServer(handler)
	defer server.Close()

	target := New(Config{Client: server.Client(), BaseURL: server.URL})
	_, err := target.Read(context.Background(), credential(), usersync.DefaultRepository)
	if !errors.Is(err, usersync.ErrNotAuthenticated) {
		t.Fatalf("Read = %v", err)
	}
}
