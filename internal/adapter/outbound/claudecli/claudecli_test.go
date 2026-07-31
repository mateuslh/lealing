package claudecli

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/mateuslh/lealing/internal/core/ccaccount"
)

// memSecrets é o cofre em memória: o teste precisa exercitar o índice sem
// tocar no chaveiro da máquina nem deixar segredo em disco.
type memSecrets struct{ box map[string][]byte }

func newMemSecrets() *memSecrets { return &memSecrets{box: map[string][]byte{}} }

func (m *memSecrets) Get(_ context.Context, key string) ([]byte, error) {
	v, ok := m.box[key]
	if !ok {
		return nil, os.ErrNotExist
	}
	return v, nil
}

func (m *memSecrets) Set(_ context.Context, key string, value []byte) error {
	m.box[key] = value
	return nil
}

func (m *memSecrets) Delete(_ context.Context, key string) error {
	delete(m.box, key)
	return nil
}

func TestConfigPreservaOResto(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "claude.json")

	original := map[string]any{
		"numStartups":            23,
		"projects":               map[string]any{"/tmp/x": map[string]any{"allowedTools": []string{"Bash"}}},
		"oauthAccount":           map[string]any{"emailAddress": "antiga@exemplo.com"},
		"userID":                 "user-antigo",
		"cachedUsageUtilization": map[string]any{"fiveHour": 0.42},
	}
	raw, _ := json.MarshalIndent(original, "", "  ")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := &ConfigFile{Path: path, BackupPath: filepath.Join(dir, "backup.json")}
	novo := json.RawMessage(`{"emailAddress":"nova@exemplo.com"}`)
	if err := cfg.SetAccount(context.Background(), novo, "user-novo"); err != nil {
		t.Fatalf("SetAccount: %v", err)
	}

	after := map[string]json.RawMessage{}
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(out, &after); err != nil {
		t.Fatalf("arquivo ficou ilegível: %v", err)
	}

	if _, ok := after["projects"]; !ok {
		t.Error("a troca de conta apagou os projetos do usuário")
	}
	if got := string(after["numStartups"]); got != "23" {
		t.Errorf("campo desconhecido não sobreviveu: %q", got)
	}
	if _, ok := after["cachedUsageUtilization"]; ok {
		t.Error("o cache de cota da conta anterior continuou no arquivo")
	}

	account, userID, err := cfg.Account(context.Background())
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if userID != "user-novo" {
		t.Errorf("userID: %q", userID)
	}
	var parsed struct {
		Email string `json:"emailAddress"`
	}
	if err := json.Unmarshal(account, &parsed); err != nil || parsed.Email != "nova@exemplo.com" {
		t.Errorf("identidade gravada errada: %s (%v)", account, err)
	}

	backup, err := os.ReadFile(cfg.BackupPath)
	if err != nil {
		t.Fatalf("backup não foi escrito: %v", err)
	}
	var restored map[string]json.RawMessage
	if err := json.Unmarshal(backup, &restored); err != nil {
		t.Fatalf("backup ilegível: %v", err)
	}
	if got := string(restored["userID"]); got != `"user-antigo"` {
		t.Errorf("o backup devia ter o estado anterior à troca, tem %s", got)
	}
}

func TestConfigCriaArquivoQuandoNaoExiste(t *testing.T) {
	dir := t.TempDir()
	cfg := &ConfigFile{Path: filepath.Join(dir, "claude.json")}

	if err := cfg.SetAccount(context.Background(), json.RawMessage(`{"emailAddress":"a@b.c"}`), "u"); err != nil {
		t.Fatalf("SetAccount: %v", err)
	}
	if _, _, err := cfg.Account(context.Background()); err != nil {
		t.Fatalf("Account: %v", err)
	}
}

func TestFileVaultGuardaSomenteParaODono(t *testing.T) {
	dir := t.TempDir()
	vault := &FileVault{Path: filepath.Join(dir, ".claude", ".credentials.json")}
	ctx := context.Background()

	if _, err := vault.Read(ctx); err == nil {
		t.Fatal("ler um cofre inexistente devia falhar")
	}

	cred := json.RawMessage(`{"claudeAiOauth":{"accessToken":"t"}}`)
	if err := vault.Write(ctx, cred); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got, err := vault.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if string(got) != string(cred) {
		t.Errorf("credencial voltou diferente: %s", got)
	}

	info, err := os.Stat(vault.Path)
	if err != nil {
		t.Fatal(err)
	}
	// No Windows o modo é sintético e este bit não significa nada; a
	// verificação vale onde a permissão é real.
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("credencial gravada com permissão %v", info.Mode().Perm())
	}
}

func TestStoreSeparaMetadadoDeSegredo(t *testing.T) {
	dir := t.TempDir()
	index := filepath.Join(dir, "claude-accounts.json")
	secrets := newMemSecrets()
	store := newStore(index, secrets)
	ctx := context.Background()

	profile := ccaccount.Profile{
		Name:    "pessoal",
		SavedAt: time.Date(2026, 7, 30, 10, 0, 0, 0, time.UTC),
		Identity: ccaccount.Identity{
			Email: "eu@exemplo.com", Organization: "Casa",
			AccountUUID: "uuid-1", Plan: "pro",
		},
	}
	session := ccaccount.Session{
		Credential: json.RawMessage(`{"claudeAiOauth":{"accessToken":"segredo-do-token"}}`),
		Account:    json.RawMessage(`{"emailAddress":"eu@exemplo.com"}`),
		UserID:     "user-1",
	}
	if err := store.Save(ctx, profile, session); err != nil {
		t.Fatalf("Save: %v", err)
	}

	raw, err := os.ReadFile(index)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "segredo-do-token") {
		t.Fatal("o token vazou para o índice em disco")
	}

	profiles, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(profiles) != 1 || profiles[0].Identity.Email != "eu@exemplo.com" {
		t.Fatalf("índice não descreveu o perfil: %+v", profiles)
	}

	loaded, err := store.Load(ctx, "pessoal")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if string(loaded.Credential) != string(session.Credential) || loaded.UserID != "user-1" {
		t.Errorf("sessão voltou diferente: %+v", loaded)
	}

	// Salvar de novo atualiza em vez de duplicar.
	profile.Identity.Plan = "max"
	if err := store.Save(ctx, profile, session); err != nil {
		t.Fatalf("Save (atualização): %v", err)
	}
	profiles, _ = store.List(ctx)
	if len(profiles) != 1 || profiles[0].Identity.Plan != "max" {
		t.Errorf("atualização virou duplicata: %+v", profiles)
	}

	if err := store.Delete(ctx, "PESSOAL"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	profiles, _ = store.List(ctx)
	if len(profiles) != 0 {
		t.Errorf("perfil sobreviveu à remoção: %+v", profiles)
	}
	if _, err := secrets.Get(ctx, "pessoal"); err == nil {
		t.Error("o segredo continuou no cofre depois da remoção")
	}
}

func TestStoreVazioNaoEhErro(t *testing.T) {
	store := newStore(filepath.Join(t.TempDir(), "novo.json"), newMemSecrets())
	profiles, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(profiles) != 0 {
		t.Errorf("índice inexistente devia listar vazio, veio %+v", profiles)
	}
}
