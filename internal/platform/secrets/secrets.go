// Package secrets guarda credenciais fora do alcance de um `grep` casual e,
// onde o sistema oferece um cofre de verdade, fora do disco.
//
// A escolha do cofre é por build tag, não por detecção em tempo de execução:
// o código do chaveiro do macOS não compila em lugar nenhum além dele, e
// manter o switch aqui é o que impede cada credencial nova de reinventar a
// própria gaveta.
package secrets

import (
	"context"
	"errors"
	"regexp"
)

// ErrNotFound indica que a chave não existe no cofre. É um estado esperado —
// o primeiro login não tem nada guardado —, então nunca deve ser tratado
// como falha.
var ErrNotFound = errors.New("segredo não encontrado")

// Store é o cofre. As implementações são por plataforma; o core recebe esta
// interface e não sabe se por trás há chaveiro ou arquivo.
type Store interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte) error
	Delete(ctx context.Context, key string) error
}

// validKey restringe a chave ao que serve tanto de conta no chaveiro quanto
// de campo em JSON, sem escapes.
var validKey = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$`)

// New devolve o cofre da plataforma. dir é usado apenas onde não há cofre do
// sistema; no macOS nada sensível chega ao disco.
func New(service, dir string) Store { return newStore(service, dir) }

func checkKey(key string) error {
	if !validKey.MatchString(key) {
		return errors.New("chave de segredo inválida: " + key)
	}
	return nil
}
