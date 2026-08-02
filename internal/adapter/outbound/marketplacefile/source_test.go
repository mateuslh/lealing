package marketplacefile

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mateuslh/lealing/internal/core/marketplace"
)

func repository(t *testing.T, index string) (string, marketplace.Origin) {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, IndexName), []byte(index), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, marketplace.Origin{
		Name: "meu-repo", Kind: marketplace.OriginLocal, Ref: root, Enabled: true,
	}
}

func TestFetchLeIndiceDoDiretorioEDoArquivo(t *testing.T) {
	body := `{"apiVersion":"` + marketplace.APIVersion + `","tools":[]}`
	root, origin := repository(t, body)
	source := New(Config{})

	index, err := source.Fetch(context.Background(), origin)
	if err != nil {
		t.Fatal(err)
	}
	if index.APIVersion != marketplace.APIVersion {
		t.Fatalf("apiVersion = %q", index.APIVersion)
	}

	// Apontar direto para o arquivo é o que permite manter vários índices no
	// mesmo repositório de trabalho.
	origin.Ref = filepath.Join(root, IndexName)
	if _, err := source.Fetch(context.Background(), origin); err != nil {
		t.Fatalf("índice apontado por arquivo: %v", err)
	}
}

func TestFetchRecusaCaminhoRelativoEJSONInvalido(t *testing.T) {
	_, origin := repository(t, `{"apiVersion":`)
	source := New(Config{})
	if _, err := source.Fetch(context.Background(), origin); err == nil {
		t.Fatal("JSON inválido foi aceito")
	}

	origin.Ref = "tools/index.json"
	if _, err := source.Fetch(context.Background(), origin); err == nil ||
		!strings.Contains(err.Error(), "absoluto") {
		t.Fatalf("caminho relativo = %v", err)
	}
}

func TestPrepareEntregaODiretorioDoBuildSemCopiar(t *testing.T) {
	root, origin := repository(t, `{"apiVersion":"`+marketplace.APIVersion+`","tools":[]}`)
	build := filepath.Join(root, "dist", "darwin-arm64")
	if err := os.MkdirAll(build, 0o700); err != nil {
		t.Fatal(err)
	}

	prepared, err := New(Config{}).Prepare(context.Background(), origin,
		marketplace.Artifact{Platform: "darwin-arm64", URL: "dist/darwin-arm64"})
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(build)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.Directory != resolved {
		t.Fatalf("diretório = %q, quero %q", prepared.Directory, resolved)
	}
	if err := prepared.Cleanup(); err != nil {
		t.Fatal(err)
	}
	// Cleanup não pode remover o projeto do usuário.
	if _, err := os.Stat(build); err != nil {
		t.Fatalf("o diretório do usuário foi apagado: %v", err)
	}
}

func TestPrepareRecusaTravessiaEArquivoAvulso(t *testing.T) {
	root, origin := repository(t, `{"apiVersion":"`+marketplace.APIVersion+`","tools":[]}`)
	outside := filepath.Join(filepath.Dir(root), "fora")
	if err := os.MkdirAll(outside, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "solto.bin"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	source := New(Config{})

	for name, reference := range map[string]string{
		"travessia": "../fora",
		"symlink":   "atalho",
		"arquivo":   "solto.bin",
	} {
		t.Run(name, func(t *testing.T) {
			if name == "symlink" {
				if err := os.Symlink(outside, filepath.Join(root, "atalho")); err != nil {
					t.Skipf("symlink indisponível: %v", err)
				}
			}
			_, err := source.Prepare(context.Background(), origin,
				marketplace.Artifact{Platform: "darwin-arm64", URL: reference})
			if err == nil {
				t.Fatalf("artefato %q foi aceito", reference)
			}
		})
	}
}
