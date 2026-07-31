// Package xdg resolve os diretórios do lealing seguindo a XDG Base
// Directory Specification, com fallback para os caminhos nativos de cada
// sistema quando as variáveis não estão definidas.
package xdg

import (
	"os"
	"path/filepath"
)

const appName = "lealing"

// Directories são os caminhos de infraestrutura resolvidos no composition
// root. Guardá-los como valor evita que adapters consultem implicitamente a
// plataforma toda vez que precisam persistir algo.
type Directories struct {
	Config string
	Data   string
	State  string
	Cache  string
	Tools  string
}

// Resolve devolve os diretórios XDG com fallback nativo.
//
// windows é decidido no composition root. Este pacote não detecta sistema
// operacional por conta própria, o que mantém toda seleção de plataforma em
// um único lugar.
func Resolve(windows bool) Directories {
	directories := Directories{
		Config: resolve(windows, "XDG_CONFIG_HOME", "APPDATA", filepath.Join(".config")),
		Data:   resolve(windows, "XDG_DATA_HOME", "LOCALAPPDATA", filepath.Join(".local", "share")),
		State:  resolve(windows, "XDG_STATE_HOME", "LOCALAPPDATA", filepath.Join(".local", "state")),
		Cache:  resolve(windows, "XDG_CACHE_HOME", "LOCALAPPDATA", filepath.Join(".cache")),
	}
	directories.Tools = filepath.Join(directories.Data, "tools")
	return directories
}

// resolve procura, nesta ordem: a variável XDG (que o usuário pode definir em
// qualquer sistema), a variável nativa do Windows e, por fim, o caminho
// relativo ao diretório do usuário.
//
// O Windows entra pelo meio de propósito: um ~/.local/share no perfil do
// usuário funciona, mas espalha estado onde nenhum outro programa procura —
// e some quando o perfil é redirecionado para a rede, que é comum em máquina
// corporativa.
func resolve(windows bool, xdgEnv, winEnv, fallback string) string {
	if dir := os.Getenv(xdgEnv); dir != "" {
		return filepath.Join(dir, appName)
	}
	if windows {
		if dir := os.Getenv(winEnv); dir != "" {
			return filepath.Join(dir, appName)
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), appName)
	}
	return filepath.Join(home, fallback, appName)
}
