package interactive_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/interactive"
)

type catalog struct{ tool domain.Tool }

func (c catalog) ByID(context.Context, domain.ToolID) (domain.Tool, error) { return c.tool, nil }

type runtime struct{ options interactive.StartOptions }

func (r *runtime) Start(_ context.Context, _ domain.Tool, options interactive.StartOptions) (interactive.Session, error) {
	r.options = options
	return session{}, nil
}

type session struct{}

func (session) Updates() <-chan interactive.Update                      { return make(chan interactive.Update) }
func (session) Send(context.Context, interactive.Event) error           { return nil }
func (session) Respond(context.Context, interactive.HostResponse) error { return nil }
func (session) Shutdown(context.Context) error                          { return nil }

func TestServiceEntregaSomenteCapabilitiesEPathsConcedidos(t *testing.T) {
	tool := domain.Tool{ID: "demo", Kind: domain.KindProcess, Runtime: &domain.ExternalRuntime{
		UIMode: "screen-v1", Capabilities: []string{"navigation.back", "browser.open"},
		Permissions: domain.ToolPermissions{ReadPaths: []string{"~/.demo/data"}, Network: true},
	}}
	runtime := &runtime{}
	service := interactive.NewService(catalog{tool: tool}, runtime, interactive.ServiceConfig{
		EngineVersion: "1.0.0", Platform: "darwin", Architecture: "arm64",
		DataRoot: "/data", CacheRoot: "/cache", UserHome: "/users/teste",
		Capabilities: []string{"navigation.back"},
	})
	if _, err := service.Open(context.Background(), "demo", interactive.OpenOptions{}); err != nil {
		t.Fatal(err)
	}
	if len(runtime.options.Capabilities) != 1 || runtime.options.Capabilities[0] != "navigation.back" {
		t.Fatalf("capabilities = %v", runtime.options.Capabilities)
	}
	want := filepath.Join("/users/teste", ".demo", "data")
	if len(runtime.options.Permissions.ReadPaths) != 1 || runtime.options.Permissions.ReadPaths[0] != want {
		t.Fatalf("paths = %v, quero %s", runtime.options.Permissions.ReadPaths, want)
	}
	if runtime.options.DataDir != filepath.Join("/data", "demo") || runtime.options.CacheDir != filepath.Join("/cache", "demo") {
		t.Fatalf("diretórios = %s / %s", runtime.options.DataDir, runtime.options.CacheDir)
	}
}
