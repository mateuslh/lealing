package githubclone

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/mateuslh/lealing/internal/core/repoclone"
)

type recentSpy struct{ paths []string }

func (s *recentSpy) Add(_ context.Context, paths []string) error {
	s.paths = append([]string(nil), paths...)
	return nil
}

func TestDiscoverMontaDestinoEConsultaOwner(t *testing.T) {
	home := t.TempDir()
	manager := New(home, nil)
	manager.run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "gh" {
			t.Fatalf("comando = %q", name)
		}
		if !slices.Contains(args, "banco-bradesco") {
			t.Fatalf("args não contêm owner: %v", args)
		}
		return []byte(`[
			{"name":"pix","url":"https://github.com/banco-bradesco/pix","sshUrl":"git@github.com:banco-bradesco/pix.git"},
			{"name":"pix-config","url":"https://github.com/banco-bradesco/pix-config","sshUrl":"git@github.com:banco-bradesco/pix-config.git"}
		]`), nil
	}

	plan, err := manager.Discover(context.Background(),
		"https://github.com/banco-bradesco/pix")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Destination != filepath.Join(home, "dev", "pix") {
		t.Fatalf("destino = %q", plan.Destination)
	}
	if len(plan.Repositories) != 2 {
		t.Fatalf("repositórios = %d", len(plan.Repositories))
	}
}

func TestResolveConsultaDetalhesDoRepoAdicionado(t *testing.T) {
	manager := New(t.TempDir(), nil)
	manager.run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "gh" || !slices.Contains(args, "banco-bradesco/pix-extra") {
			t.Fatalf("comando inesperado: %s %v", name, args)
		}
		return []byte(`{
			"name":"pix-extra",
			"url":"https://github.com/banco-bradesco/pix-extra",
			"sshUrl":"git@github.com:banco-bradesco/pix-extra.git",
			"description":"integração adicional",
			"visibility":"INTERNAL",
			"primaryLanguage":{"name":"Kotlin"},
			"defaultBranchRef":{"name":"develop"},
			"updatedAt":"2026-07-31T12:00:00Z",
			"diskUsage":4096
		}`), nil
	}

	repo, err := manager.Resolve(context.Background(), repoclone.Source{
		Owner: "banco-bradesco", Protocol: repoclone.ProtocolSSH,
	}, "pix-extra")
	if err != nil {
		t.Fatal(err)
	}
	if repo.CloneURL != "git@github.com:banco-bradesco/pix-extra.git" ||
		repo.Visibility != "INTERNAL" || repo.Language != "Kotlin" ||
		repo.DefaultBranch != "develop" {
		t.Fatalf("repo = %#v", repo)
	}
}

func TestClonePreservaCloneExistenteERecusaPastaComum(t *testing.T) {
	root := filepath.Join(t.TempDir(), "dev", "pix")
	existing := filepath.Join(root, "pix")
	blocked := filepath.Join(root, "pix-config")
	if err := os.MkdirAll(filepath.Join(existing, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(blocked, 0o755); err != nil {
		t.Fatal(err)
	}

	spy := &recentSpy{}
	manager := New(t.TempDir(), spy)
	manager.run = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name != "git" || len(args) < 2 {
			t.Fatalf("comando inesperado: %s %v", name, args)
		}
		dest := args[len(args)-1]
		if err := os.MkdirAll(filepath.Join(dest, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
		return nil, nil
	}

	plan := repoclone.Plan{
		Destination: root,
		Repositories: []repoclone.Repository{
			{Name: "pix", CloneURL: "https://github.com/banco-bradesco/pix"},
			{Name: "pix-config", CloneURL: "https://github.com/banco-bradesco/pix-config"},
			{Name: "pix-worker", CloneURL: "https://github.com/banco-bradesco/pix-worker"},
		},
	}
	result, err := manager.Clone(context.Background(), plan)
	if err == nil {
		t.Fatal("pasta comum deveria produzir falha parcial")
	}
	if len(result.Outcomes) != 3 || !result.Outcomes[0].Existing ||
		result.Outcomes[1].Err == nil || result.Outcomes[2].Err != nil {
		t.Fatalf("resultados = %#v", result.Outcomes)
	}
	if want := []string{existing, filepath.Join(root, "pix-worker")}; !slices.Equal(spy.paths, want) {
		t.Fatalf("recentes = %v, quero %v", spy.paths, want)
	}
}
