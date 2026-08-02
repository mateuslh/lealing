package usersyncstore

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mateuslh/lealing/internal/core/usersync"
	"github.com/mateuslh/lealing/internal/platform/secrets"
)

// memoryVault troca o cofre da plataforma por um mapa: o teste não pode
// depender do chaveiro da máquina que roda a suíte.
type memoryVault struct {
	values map[string][]byte
	failOn string
}

func (m *memoryVault) Get(_ context.Context, key string) ([]byte, error) {
	if value, ok := m.values[key]; ok {
		return value, nil
	}
	return nil, secrets.ErrNotFound
}

func (m *memoryVault) Set(_ context.Context, key string, value []byte) error {
	if m.failOn == key {
		return errors.New("cofre recusou")
	}
	if m.values == nil {
		m.values = map[string][]byte{}
	}
	m.values[key] = value
	return nil
}

func (m *memoryVault) Delete(_ context.Context, key string) error {
	delete(m.values, key)
	return nil
}

func TestCredencialAusenteNaoEErro(t *testing.T) {
	credential, err := NewTokens(&memoryVault{}).Load(context.Background())
	if err != nil {
		t.Fatalf("Load = %v", err)
	}
	if !credential.Empty() {
		t.Fatalf("credencial = %+v", credential)
	}
}

func TestCredencialVaiEVoltaDoCofre(t *testing.T) {
	vault := &memoryVault{}
	tokens := NewTokens(vault)
	ctx := context.Background()

	want := usersync.Credential{Token: "t0ken", Scope: "repo"}
	if err := tokens.Save(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := tokens.Load(ctx)
	if err != nil || got.Token != want.Token || got.Scope != want.Scope {
		t.Fatalf("credencial = %+v, err = %v", got, err)
	}

	if err := tokens.Delete(ctx); err != nil {
		t.Fatal(err)
	}
	if after, _ := tokens.Load(ctx); !after.Empty() {
		t.Fatalf("credencial sobreviveu ao delete: %+v", after)
	}
}

// Um cofre com lixo dentro leva ao mesmo caminho de quem nunca entrou: fazer
// login de novo. Devolver erro deixaria a tela sem saída.
func TestCredencialCorrompidaValeComoAusente(t *testing.T) {
	vault := &memoryVault{values: map[string][]byte{credentialKey: []byte("{{{")}}
	credential, err := NewTokens(vault).Load(context.Background())
	if err != nil || !credential.Empty() {
		t.Fatalf("credencial = %+v, err = %v", credential, err)
	}
}

func TestAjustesAusentesValemComoPadrao(t *testing.T) {
	settings, err := NewSettings(filepath.Join(t.TempDir(), "sync.json")).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !settings.Identity.Empty() || settings.Repository != "" {
		t.Fatalf("ajustes = %+v", settings)
	}
}

func TestAjustesVaoEVoltamDoDisco(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "sync.json")
	store := NewSettings(path)
	ctx := context.Background()

	want := usersync.Settings{
		Identity: usersync.Identity{Login: "alguem"}, Repository: "lealing-state",
		Revision: "abc",
	}.WithSelection(usersync.Selection{usersync.SectionUsage: true})

	if err := store.Save(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Identity.Login != "alguem" || got.Revision != "abc" {
		t.Fatalf("ajustes = %+v", got)
	}
	if !got.Selection().Enabled(usersync.SectionUsage) ||
		got.Selection().Enabled(usersync.SectionTools) {
		t.Fatalf("seções = %+v", got.Sections)
	}

	// O arquivo é configuração comum: nada de segredo pode ter vazado nele.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "token") {
		t.Fatalf("o arquivo de ajustes carrega credencial: %s", raw)
	}
}

func TestAjustesDeVersaoFuturaSaoRecusados(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sync.json")
	if err := os.WriteFile(path, []byte(`{"version":999}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewSettings(path).Load(context.Background()); err == nil {
		t.Fatal("ajustes de versão futura foram aceitos")
	}
}
