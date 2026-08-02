package marketplacesources

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mateuslh/lealing/internal/core/marketplace"
)

func origin(name string) marketplace.Origin {
	return marketplace.Origin{
		Name: name, Kind: marketplace.OriginRemote,
		Ref: "https://" + name + ".test/index.json", Enabled: true,
	}
}

func TestArquivoAusenteValeComoNenhumaPersonalizacao(t *testing.T) {
	store := New(filepath.Join(t.TempDir(), "nao-existe.json"))
	state, err := store.Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Custom) != 0 || len(state.DisabledBuiltins) != 0 {
		t.Fatalf("estado = %+v", state)
	}
}

func TestSalvaELeDeVolta(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "marketplace-sources.json")
	store := New(path)
	ctx := context.Background()

	want := marketplace.SourceState{
		Custom:           []marketplace.Origin{origin("parceiro")},
		DisabledBuiltins: []string{"lealing"},
	}
	if err := store.Save(ctx, want); err != nil {
		t.Fatal(err)
	}

	got, err := store.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Custom) != 1 || got.Custom[0].Ref != want.Custom[0].Ref ||
		len(got.DisabledBuiltins) != 1 || got.DisabledBuiltins[0] != "lealing" {
		t.Fatalf("estado = %+v", got)
	}

	// Confiança e origem embutida não vão para o disco: reescrevê-las à mão
	// não pode promover um índice de terceiro.
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "Trusted") || strings.Contains(string(raw), "trusted") {
		t.Fatalf("arquivo carrega confiança: %s", raw)
	}
}

func TestEntradaCorrompidaSomeSemDerrubarOResto(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sources.json")
	raw, err := json.Marshal(document{
		Version: Version,
		Custom: []marketplace.Origin{
			origin("boa"),
			{Name: "SEM NOME VÁLIDO", Kind: marketplace.OriginRemote, Ref: "https://x.test/i.json"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	state, err := New(path).Load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(state.Custom) != 1 || state.Custom[0].Name != "boa" {
		t.Fatalf("origens = %+v", state.Custom)
	}
}

func TestFormatoMaisNovoERecusadoEmVezDeInterpretadoPelaMetade(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sources.json")
	if err := os.WriteFile(path, []byte(`{"version":999,"custom":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(path).Load(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "versão mais nova") {
		t.Fatalf("Load = %v", err)
	}
}
