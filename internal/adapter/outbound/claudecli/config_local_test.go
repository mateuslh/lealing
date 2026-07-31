package claudecli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestConfigRealDaMaquinaAtravessaIntacto roda contra uma CÓPIA do
// ~/.claude.json desta máquina — o arquivo de verdade, com as dezenas de
// chaves que a CLI cria ao longo do uso, que nenhuma fixture reproduz.
//
// Pula quando o arquivo não existe, que é o caso em CI. É um teste de
// segurança para o dado do usuário: o que precisa sobreviver à troca de
// conta é tudo, menos os caches presos à conta anterior.
func TestConfigRealDaMaquinaAtravessaIntacto(t *testing.T) {
	origem := ConfigPath()
	raw, err := os.ReadFile(origem)
	if err != nil {
		t.Skipf("sem ~/.claude.json nesta máquina: %v", err)
	}

	dir := t.TempDir()
	copia := filepath.Join(dir, "claude.json")
	if err := os.WriteFile(copia, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	var antes map[string]json.RawMessage
	if err := json.Unmarshal(raw, &antes); err != nil {
		t.Fatal(err)
	}
	t.Logf("arquivo real: %d bytes, %d chaves", len(raw), len(antes))

	cfg := &ConfigFile{Path: copia, BackupPath: filepath.Join(dir, "bak.json")}
	novo := json.RawMessage(`{"emailAddress":"outra@exemplo.com","accountUuid":"uuid-outro"}`)
	if err := cfg.SetAccount(context.Background(), novo, "user-outro"); err != nil {
		t.Fatalf("SetAccount: %v", err)
	}

	depoisRaw, _ := os.ReadFile(copia)
	var depois map[string]json.RawMessage
	if err := json.Unmarshal(depoisRaw, &depois); err != nil {
		t.Fatalf("arquivo ficou ilegível: %v", err)
	}

	caches := map[string]bool{}
	for _, k := range accountScopedCaches {
		caches[k] = true
	}
	for k, v := range antes {
		switch {
		case k == accountKey || k == userKey:
			continue
		case caches[k]:
			if _, ainda := depois[k]; ainda {
				t.Errorf("cache da conta anterior sobreviveu: %s", k)
			}
		default:
			// Comparação semântica: a reescrita normaliza indentação, e o
			// que precisa sobreviver é o valor, não os espaços.
			var a, b any
			_ = json.Unmarshal(v, &a)
			if err := json.Unmarshal(depois[k], &b); err != nil {
				t.Errorf("chave %s sumiu ou ficou ilegível", k)
				continue
			}
			if !reflect.DeepEqual(a, b) {
				t.Errorf("chave %s mudou de conteúdo", k)
			}
		}
	}
	t.Logf("chaves depois: %d (removidos %d caches de conta)", len(depois), len(antes)-len(depois))
}
