package gitinsight

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	core "github.com/mateuslh/lealing/internal/core/gitinsight"
)

func TestParseBranches(t *testing.T) {
	raw := []byte(
		"*\x00main\x00origin/main\x00origin\x00refs/heads/main\x00[ahead 2, behind 1]\x00abc1234\x001785500000\x00ajusta pagamentos\n" +
			" \x00feature/pronta\x00origin/feature/pronta\x00origin\x00refs/heads/feature/pronta\x00\x00def5678\x001785400000\x00feature concluída\n" +
			" \x00rascunho\x00\x00\x00\x00\x00fed4321\x001785300000\x00trabalho local\n" +
			" \x00antiga\x00origin/antiga\x00origin\x00refs/heads/antiga\x00[gone]\x001234abc\x001785200000\x00branch removida\n")

	got := ParseBranches(raw)
	if len(got) != 4 {
		t.Fatalf("branches = %d", len(got))
	}
	if !got[0].Current || got[0].Ahead != 2 || got[0].Behind != 1 {
		t.Fatalf("main = %#v", got[0])
	}
	if got[0].RemoteRef != "refs/heads/main" {
		t.Fatalf("remote ref = %q", got[0].RemoteRef)
	}
	if !got[1].CleanupCandidate() {
		t.Fatalf("feature pronta não foi candidata: %#v", got[1])
	}
	if !got[2].WithoutUpstream() || !got[3].Gone {
		t.Fatalf("branches incertas = %#v / %#v", got[2], got[3])
	}
}

func TestCountChangesNaoDuplicaRename(t *testing.T) {
	raw := []byte(" M arquivo.go\x00R  novo.go\x00antigo.go\x00?? outro.txt\x00")
	if got := CountChanges(raw); got != 3 {
		t.Fatalf("alterações = %d, quero 3", got)
	}
}

func TestFindRepositoriesNaoEntraDentroDeClone(t *testing.T) {
	root := t.TempDir()
	repoA := filepath.Join(root, "grupo", "api")
	repoB := filepath.Join(root, "grupo", "api-config")
	for _, repo := range []string{repoA, repoB} {
		if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// Um .git falso dentro do clone deve ser ignorado: o clone externo já
	// encerra a descida.
	if err := os.MkdirAll(filepath.Join(repoA, "vendor", "falso", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := FindRepositories(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != repoA || got[1] != repoB {
		t.Fatalf("repos = %v", got)
	}
}

func TestScannerLeRepositorioReal(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git não está no PATH")
	}
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		t.Skip("checkout de teste não contém .git")
	}

	report, err := New(root, nil).Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Repositories) != 1 || len(report.Repositories[0].Branches) == 0 {
		t.Fatalf("report = %#v", report)
	}
}

func TestAcoesUsamArgumentosSeguros(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "pix", "api")
	var calls [][]string
	scanner := &Scanner{
		root: root,
		run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, append([]string{name}, args...))
			return nil, nil
		},
	}

	if err := scanner.Fetch(context.Background(), repo); err != nil {
		t.Fatal(err)
	}
	if err := scanner.Push(context.Background(), repo, core.Branch{
		Name: "feature/pix", Remote: "origin", RemoteRef: "refs/heads/review/pix",
	}); err != nil {
		t.Fatal(err)
	}
	if err := scanner.DeleteLocalBranch(context.Background(), repo, "--force"); err != nil {
		t.Fatal(err)
	}

	want := [][]string{
		{"git", "-C", repo, "fetch", "--all", "--prune"},
		{"git", "-C", repo, "push", "--", "origin", "refs/heads/feature/pix:refs/heads/review/pix"},
		{"git", "-C", repo, "branch", "-d", "--", "--force"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("comandos = %#v, quero %#v", calls, want)
	}
}

func TestAcoesRecusamRepositorioForaDoDev(t *testing.T) {
	root := t.TempDir()
	scanner := &Scanner{
		root: root,
		run: func(context.Context, string, ...string) ([]byte, error) {
			t.Fatal("não deveria executar Git")
			return nil, nil
		},
	}

	if err := scanner.Fetch(context.Background(), filepath.Dir(root)); err == nil {
		t.Fatal("fetch fora da raiz deveria falhar")
	}
}

func TestOperacoesEmLoteUsamDezWorkers(t *testing.T) {
	tests := map[string]func(context.Context, *Scanner) error{
		"scan": func(ctx context.Context, scanner *Scanner) error {
			_, err := scanner.Scan(ctx)
			return err
		},
		"atualização": func(ctx context.Context, scanner *Scanner) error {
			_, err := scanner.UpdateAll(ctx)
			return err
		},
	}
	for name, operation := range tests {
		t.Run(name, func(t *testing.T) {
			assertTenWorkers(t, operation)
		})
	}
}

func assertTenWorkers(
	t *testing.T,
	operation func(context.Context, *Scanner) error,
) {
	t.Helper()
	root := t.TempDir()
	for i := range 12 {
		path := filepath.Join(root, "repo-"+string(rune('a'+i)), ".git")
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	gate := make(chan struct{})
	entered := make(chan struct{}, 64)
	var current, peak atomic.Int32
	scanner := &Scanner{
		root: root,
		now:  time.Now,
		run: func(context.Context, string, ...string) ([]byte, error) {
			active := current.Add(1)
			for observed := peak.Load(); active > observed; observed = peak.Load() {
				if peak.CompareAndSwap(observed, active) {
					break
				}
			}
			entered <- struct{}{}
			<-gate
			current.Add(-1)
			return nil, nil
		},
	}

	done := make(chan error, 1)
	go func() {
		done <- operation(context.Background(), scanner)
	}()

	for range 10 {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("scanner não ocupou os dez workers")
		}
	}
	select {
	case <-entered:
		t.Fatal("scanner excedeu os dez workers")
	default:
	}
	close(gate)

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("scanner não concluiu após liberar os workers")
	}
	if got := peak.Load(); got != 10 {
		t.Fatalf("pico de workers = %d, quero 10", got)
	}
}

func TestUpdateRepositoryAvancaMainAtualSomenteComFastForward(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "pix", "api")
	var calls [][]string
	scanner := &Scanner{
		root: root,
		run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, append([]string{name}, args...))
			switch args[2] {
			case "for-each-ref":
				return branchOutput("*", "main", "origin/main", "origin",
					"refs/heads/main", "[behind 3]"), nil
			case "status":
				return nil, nil
			default:
				return nil, nil
			}
		},
	}

	result := scanner.updateRepository(context.Background(), repo)
	if result.State != core.UpdateUpdated || result.Branch != "main" ||
		result.Detail != "avançou 3 commits" {
		t.Fatalf("resultado = %#v", result)
	}
	wantLast := []string{"git", "-C", repo, "merge", "--ff-only", "--", "origin/main"}
	if !reflect.DeepEqual(calls[len(calls)-1], wantLast) {
		t.Fatalf("último comando = %#v, quero %#v", calls[len(calls)-1], wantLast)
	}
}

func TestUpdateRepositoryAvancaMainForaDaAtualSemCheckout(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "pix", "api")
	var calls [][]string
	scanner := &Scanner{
		root: root,
		run: func(_ context.Context, name string, args ...string) ([]byte, error) {
			calls = append(calls, append([]string{name}, args...))
			switch args[2] {
			case "for-each-ref":
				return append(
					branchOutput(" ", "main", "origin/main", "origin",
						"refs/heads/main", "[behind 2]"),
					branchOutput("*", "feature/pix", "origin/feature/pix", "origin",
						"refs/heads/feature/pix", "")...,
				), nil
			case "status":
				return nil, nil
			default:
				return nil, nil
			}
		},
	}

	result := scanner.updateRepository(context.Background(), repo)
	if result.State != core.UpdateUpdated {
		t.Fatalf("resultado = %#v", result)
	}
	wantLast := []string{
		"git", "-C", repo, "fetch", "--no-tags", "--", "origin",
		"refs/heads/main:refs/heads/main",
	}
	if !reflect.DeepEqual(calls[len(calls)-1], wantLast) {
		t.Fatalf("último comando = %#v, quero %#v", calls[len(calls)-1], wantLast)
	}
	for _, call := range calls {
		if len(call) > 3 && (call[3] == "checkout" || call[3] == "switch") {
			t.Fatalf("atualização trocou de branch: %#v", call)
		}
	}
}

func TestUpdateRepositoryIgnoraMainComCommitsLocais(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "pix", "api")
	var calls int
	scanner := &Scanner{
		root: root,
		run: func(_ context.Context, _ string, args ...string) ([]byte, error) {
			calls++
			switch args[2] {
			case "for-each-ref":
				return branchOutput("*", "main", "origin/main", "origin",
					"refs/heads/main", "[ahead 1, behind 2]"), nil
			default:
				return nil, nil
			}
		},
	}

	result := scanner.updateRepository(context.Background(), repo)
	if result.State != core.UpdateSkipped ||
		result.Detail != "1 commit local pendente" {
		t.Fatalf("resultado = %#v", result)
	}
	if calls != 3 {
		t.Fatalf("executou %d comandos; deveria parar após fetch, refs e status", calls)
	}
}

func TestUpdateRepositoryIgnoraWorkingTreeAlterada(t *testing.T) {
	root := t.TempDir()
	repo := filepath.Join(root, "pix", "api")
	var calls int
	scanner := &Scanner{
		root: root,
		run: func(_ context.Context, _ string, args ...string) ([]byte, error) {
			calls++
			switch args[2] {
			case "for-each-ref":
				return branchOutput("*", "main", "origin/main", "origin",
					"refs/heads/main", "[behind 2]"), nil
			case "status":
				return []byte(" M arquivo.go\x00"), nil
			default:
				return nil, nil
			}
		},
	}

	result := scanner.updateRepository(context.Background(), repo)
	if result.State != core.UpdateSkipped ||
		result.Detail != "working tree com 1 alteração" {
		t.Fatalf("resultado = %#v", result)
	}
	if calls != 3 {
		t.Fatalf("executou %d comandos; não deveria alterar o clone", calls)
	}
}

func branchOutput(
	head, name, upstream, remote, remoteRef, track string,
) []byte {
	return []byte(strings.Join([]string{
		head, name, upstream, remote, remoteRef, track,
		"abc1234", "1785500000", "commit",
	}, "\x00") + "\n")
}
