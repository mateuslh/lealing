package xdg

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
)

// Adopt entrega path ao usuário real por trás de um `sudo`.
//
// Sob sudo o HOME continua sendo o do usuário original, então os diretórios
// resolvidos seguem apontando para dentro dele — mas tudo que
// gravamos nasce com dono root, e a execução seguinte, sem sudo, não consegue
// nem ler o próprio estado. Foi assim que um `sudo lealing` deixou o
// usage.json inacessível com "permission denied".
//
// Fora do sudo é um no-op, e o mesmo vale no Windows, onde as variáveis não
// existem. Arquivo que sumiu entre a criação e a adoção também não é erro: o
// objetivo é não deixar nada com dono root para trás.
func Adopt(path string) error {
	uid, gid, ok := realUser()
	if !ok {
		return nil
	}
	if err := os.Lchown(path, uid, gid); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// MkdirAll é os.MkdirAll com os diretórios recém-criados entregues ao usuário
// real. Adotar só o arquivo final não bastaria: o primeiro `sudo lealing` de
// uma máquina nova cria a árvore inteira, e um ~/.local/share de dono root
// tranca todo programa que venha depois.
func MkdirAll(dir string, perm fs.FileMode) error {
	// Precisa ser calculado antes: depois do MkdirAll todos existem, e não há
	// como distinguir os que criamos dos que já estavam lá.
	missing := missingAncestors(dir)

	if err := os.MkdirAll(dir, perm); err != nil {
		return err
	}
	for _, p := range missing {
		if err := Adopt(p); err != nil {
			return err
		}
	}
	return nil
}

// missingAncestors lista dir e seus ancestrais que ainda não existem, do mais
// fundo para o mais raso.
func missingAncestors(dir string) []string {
	var missing []string
	for p := filepath.Clean(dir); ; {
		if _, err := os.Lstat(p); err == nil {
			break
		}
		missing = append(missing, p)
		parent := filepath.Dir(p)
		if parent == p {
			break // chegamos à raiz
		}
		p = parent
	}
	return missing
}

// realUser devolve o uid/gid de quem chamou o sudo. O terceiro retorno é
// falso quando não há sudo em jogo — inclusive quando o próprio root é o
// usuário original, caso em que não há nada a corrigir.
//
// A checagem de euid importa: `sudo -u outro` também define SUDO_UID, e ali
// não temos privilégio para chown nenhum. No Windows Geteuid devolve -1, o
// que já cai fora por si só.
func realUser() (uid, gid int, ok bool) {
	if os.Geteuid() != 0 {
		return 0, 0, false
	}
	uid, err := strconv.Atoi(os.Getenv("SUDO_UID"))
	if err != nil || uid == 0 {
		return 0, 0, false
	}
	gid, err = strconv.Atoi(os.Getenv("SUDO_GID"))
	if err != nil {
		return 0, 0, false
	}
	return uid, gid, true
}
