// Package xdg resolve os diretórios do lealing seguindo a XDG Base
// Directory Specification, com fallback para os caminhos nativos de cada
// sistema quando as variáveis não estão definidas.
package xdg

import (
	"os"
	"path/filepath"
	"runtime"
)

const appName = "lealing"

// ConfigDir devolve o diretório de configuração ($XDG_CONFIG_HOME/lealing).
func ConfigDir() string {
	return resolve("XDG_CONFIG_HOME", "APPDATA", filepath.Join(".config"))
}

// DataDir devolve o diretório de dados ($XDG_DATA_HOME/lealing), onde vive
// o estado de uso.
func DataDir() string {
	return resolve("XDG_DATA_HOME", "LOCALAPPDATA", filepath.Join(".local", "share"))
}

// StateDir devolve o diretório de estado ($XDG_STATE_HOME/lealing), onde
// vivem os logs.
func StateDir() string {
	return resolve("XDG_STATE_HOME", "LOCALAPPDATA", filepath.Join(".local", "state"))
}

// resolve procura, nesta ordem: a variável XDG (que o usuário pode definir em
// qualquer sistema), a variável nativa do Windows e, por fim, o caminho
// relativo ao diretório do usuário.
//
// O Windows entra pelo meio de propósito: um ~/.local/share no perfil do
// usuário funciona, mas espalha estado onde nenhum outro programa procura —
// e some quando o perfil é redirecionado para a rede, que é comum em máquina
// corporativa.
func resolve(xdgEnv, winEnv, fallback string) string {
	if dir := os.Getenv(xdgEnv); dir != "" {
		return filepath.Join(dir, appName)
	}
	if runtime.GOOS == "windows" {
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
