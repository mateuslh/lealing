package secrets

import (
	"context"
	"strings"
	"testing"
)

// Uma chave livre viraria argumento de linha de comando no macOS e campo de
// JSON no resto: validar antes é o que impede as duas coisas de darem errado
// de formas diferentes.
func TestChaveInvalidaEhRecusadaAntesDeTocarNoCofre(t *testing.T) {
	store := New("lealing-teste", t.TempDir())
	ctx := context.Background()

	for _, key := range []string{
		"", " ", "-comeca-com-hifen", "com espaço", "com/barra", "com\nquebra",
		strings.Repeat("x", 65),
	} {
		if _, err := store.Get(ctx, key); err == nil || err == ErrNotFound {
			t.Errorf("Get aceitou a chave %q (err = %v)", key, err)
		}
		if err := store.Set(ctx, key, []byte("x")); err == nil {
			t.Errorf("Set aceitou a chave %q", key)
		}
		if err := store.Delete(ctx, key); err == nil {
			t.Errorf("Delete aceitou a chave %q", key)
		}
	}
}

func TestChaveValidaPassaNaValidacao(t *testing.T) {
	for _, key := range []string{"github", "github-2", "conta.principal", "a"} {
		if err := checkKey(key); err != nil {
			t.Errorf("chave %q recusada: %v", key, err)
		}
	}
}
