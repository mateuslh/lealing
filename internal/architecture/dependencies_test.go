// Package architecture_test protege as fronteiras do hexágono.
//
// Estes testes não substituem revisão de design, mas transformam as regras
// de dependência mais importantes em falhas de CI, antes que uma violação se
// espalhe por novas tools.
package architecture_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const module = "github.com/mateuslh/lealing"

type sourceFile struct {
	rel     string
	imports []string
}

func TestDependenciasApontamParaDentro(t *testing.T) {
	files := projectSources(t)

	for _, file := range files {
		for _, imported := range file.imports {
			if strings.HasPrefix(imported, module+"/cmd/tools/") {
				t.Errorf("%s: engine importa implementação concreta de tool %q", file.rel, imported)
			}
		}
		// Testes de integração podem montar o grafo real e, portanto, importar
		// os dois lados. A regra de produção continua protegida; duplos
		// arquiteturais ficam cobertos pelos testes dos serviços.
		if strings.HasSuffix(file.rel, "_test.go") {
			continue
		}
		switch {
		// O SDK público (protocol/screen/component/machine) não vive mais
		// neste módulo — mora em github.com/mateuslh/lealing-sdk, um
		// repositório independente que define e testa seu próprio contrato.
		// Este arquivo só protege as fronteiras do que ainda está aqui.
		case strings.HasPrefix(file.rel, "internal/core/"):
			for _, imported := range file.imports {
				if strings.HasPrefix(imported, module+"/internal/") &&
					!strings.HasPrefix(imported, module+"/internal/core/") {
					t.Errorf("%s: core importa camada externa %q", file.rel, imported)
				}
				if isThirdParty(imported) &&
					!strings.HasPrefix(imported, module+"/internal/core/") {
					t.Errorf("%s: core importa dependência de terceiro %q", file.rel, imported)
				}
			}

		case strings.HasPrefix(file.rel, "internal/adapter/inbound/"):
			for _, imported := range file.imports {
				if strings.HasPrefix(imported, module+"/internal/adapter/outbound/") {
					t.Errorf("%s: driving adapter importa driven adapter %q", file.rel, imported)
				}
				if imported == module+"/internal/core/port/outbound" {
					t.Errorf("%s: driving adapter chama porta de saída %q", file.rel, imported)
				}
				switch imported {
				case "os", "os/exec", "net/http":
					t.Errorf("%s: tela importa I/O direto %q; injete a dependência e use tea.Cmd", file.rel, imported)
				}
			}

		case strings.HasPrefix(file.rel, "internal/adapter/outbound/"):
			for _, imported := range file.imports {
				if strings.HasPrefix(imported, module+"/internal/adapter/") {
					t.Errorf("%s: driven adapter compõe outro adapter %q", file.rel, imported)
				}
				if strings.HasPrefix(imported, module+"/internal/bootstrap") ||
					strings.HasPrefix(imported, module+"/internal/catalog") {
					t.Errorf("%s: driven adapter importa composition root %q", file.rel, imported)
				}
			}

		case strings.HasPrefix(file.rel, "internal/catalog/"):
			for _, imported := range file.imports {
				if strings.HasPrefix(imported, module+"/internal/") &&
					!strings.HasPrefix(imported, module+"/internal/core/") {
					t.Errorf("%s: catálogo declarativo importa detalhe externo %q", file.rel, imported)
				}
			}
		}
	}
}

func isThirdParty(imported string) bool {
	first, _, _ := strings.Cut(imported, "/")
	return strings.Contains(first, ".")
}

func TestSelecaoDePlataformaFicaNoCompositionRoot(t *testing.T) {
	for _, file := range projectSources(t) {
		for _, imported := range file.imports {
			if imported == "runtime" && file.rel != "internal/bootstrap/platform.go" {
				t.Errorf("%s: importa runtime; seleção de GOOS/GOARCH pertence a internal/bootstrap/platform.go", file.rel)
			}
		}
	}
}

func TestEngineNaoHospedaImplementacaoConcretaDeToolExterna(t *testing.T) {
	path := filepath.Join(projectRoot(t), "cmd", "tools")
	if _, err := os.Stat(path); err == nil {
		t.Fatalf("%s existe: tools externas pertencem a repositórios próprios", path)
	} else if !os.IsNotExist(err) {
		t.Fatal(err)
	}
}

func projectSources(t *testing.T) []sourceFile {
	t.Helper()
	root := projectRoot(t)
	var files []sourceFile

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			switch entry.Name() {
			case ".git", "vendor", "dist", "bin":
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" {
			return nil
		}

		parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		imports := make([]string, 0, len(parsed.Imports))
		for _, spec := range parsed.Imports {
			value, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatalf("import em %s: %v", path, err)
			}
			imports = append(imports, value)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, sourceFile{
			rel:     filepath.ToSlash(rel),
			imports: imports,
		})
		return nil
	})
	if err != nil {
		t.Fatalf("listar fontes: %v", err)
	}
	return files
}

func projectRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod não encontrado")
		}
		dir = parent
	}
}
