// Package selfupdate implementa as portas da tool de atualização: descobrir
// como este binário foi instalado, consultar as releases do GitHub e trocar o
// executável em disco.
package selfupdate

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	core "github.com/mateuslh/lealing/internal/core/selfupdate"
)

// maxWalkUp é quantos diretórios acima do executável procuramos a raiz do
// clone. `bin/lealing` precisa de um; a folga cobre layouts como
// `bin/darwin_arm64/lealing` que o Go produz em cross-compile.
const maxWalkUp = 4

// Locator implementa core.Locator inspecionando o disco.
type Locator struct {
	// module é a linha `module` esperada no go.mod da raiz. Confirmá-la
	// evita que um binário guardado dentro de outro projeto Go qualquer se
	// declare "instalação de fonte" e mande um git pull no repositório errado.
	module string
}

var _ core.Locator = (*Locator)(nil)

// NewLocator monta o localizador para o módulo informado.
func NewLocator(module string) *Locator { return &Locator{module: module} }

// Locate implementa core.Locator.
func (l *Locator) Locate(ctx context.Context) (core.Install, error) {
	exe, err := os.Executable()
	if err != nil {
		return core.Install{}, err
	}
	// Symlinks resolvidos: sem isso, um binário linkado de ~/.local/bin
	// reportaria um caminho que não é o que precisa ser sobrescrito.
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return l.locateFrom(ctx, exe)
}

// locateFrom é a decisão em si, separada da descoberta do executável: é o que
// permite testar os três modos sem depender de onde o binário de teste roda.
func (l *Locator) locateFrom(ctx context.Context, exe string) (core.Install, error) {
	in := core.Install{
		Mode:       core.ModeRelease,
		BinaryPath: exe,
		Writable:   writable(filepath.Dir(exe)),
	}

	if repo := l.findRepo(filepath.Dir(exe)); repo != "" {
		in.Mode = core.ModeSource
		in.RepoDir = repo
		in.Branch = currentBranch(ctx, repo)
	}
	return in, nil
}

// findRepo sobe do diretório do binário procurando a raiz do clone: um
// diretório com .git e com o go.mod deste módulo.
//
// Os dois juntos, e não um dos dois: só o .git acharia qualquer repositório
// que por acaso contenha o binário, e só o go.mod acharia um módulo baixado
// sem histórico — nenhum dos dois dá para atualizar com git pull.
func (l *Locator) findRepo(dir string) string {
	for range maxWalkUp {
		if l.isRepo(dir) {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func (l *Locator) isRepo(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		return false
	}
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return false
	}
	if l.module == "" {
		return true
	}
	for line := range strings.Lines(string(data)) {
		if strings.HasPrefix(strings.TrimSpace(line), "module ") {
			return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "module ")) == l.module
		}
	}
	return false
}

// currentBranch devolve a branch do clone, ou vazio quando o git não responde.
func currentBranch(ctx context.Context, repo string) string {
	out, err := exec.CommandContext(ctx, "git", "-C", repo, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// writable testa a escrita criando e apagando um arquivo no diretório.
//
// É o único teste confiável: os bits de permissão não contam a história toda
// (dono, ACL, volume montado somente-leitura), e descobrir isso depois do
// download seria descobrir tarde demais.
func writable(dir string) bool {
	f, err := os.CreateTemp(dir, ".lealing-write-*")
	if err != nil {
		return false
	}
	name := f.Name()
	_ = f.Close()
	_ = os.Remove(name)
	return true
}
