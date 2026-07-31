package ccaccount_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/mateuslh/lealing/internal/core/ccaccount"
)

func now() time.Time { return time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC) }

// credential monta a credencial como a CLI a grava, com as duas validades em
// milissegundos.
func credential(plan string, expires, renews time.Time) json.RawMessage {
	payload := map[string]any{"claudeAiOauth": map[string]any{
		"accessToken":           "tok-" + plan,
		"refreshToken":          "ref-" + plan,
		"subscriptionType":      plan,
		"expiresAt":             expires.UnixMilli(),
		"refreshTokenExpiresAt": renews.UnixMilli(),
	}}
	raw, _ := json.Marshal(payload)
	return raw
}

func account(email, org, uuid string) json.RawMessage {
	raw, _ := json.Marshal(map[string]any{
		"emailAddress": email, "organizationName": org,
		"accountUuid": uuid, "displayName": "Alguém",
	})
	return raw
}

// --- Duplos ------------------------------------------------------------

type fakeVault struct {
	cred json.RawMessage
	err  error
}

func (f *fakeVault) Read(context.Context) (json.RawMessage, error) {
	if f.err != nil {
		return nil, f.err
	}
	if len(f.cred) == 0 {
		return nil, ccaccount.ErrNoActiveSession
	}
	return f.cred, nil
}

func (f *fakeVault) Write(_ context.Context, c json.RawMessage) error { f.cred = c; return nil }
func (f *fakeVault) Origin() string                                   { return "cofre de teste" }

type fakeConfig struct {
	account json.RawMessage
	userID  string
}

func (f *fakeConfig) Account(context.Context) (json.RawMessage, string, error) {
	return f.account, f.userID, nil
}

func (f *fakeConfig) SetAccount(_ context.Context, a json.RawMessage, id string) error {
	f.account, f.userID = a, id
	return nil
}

type fakeStore struct {
	profiles map[string]ccaccount.Profile
	sessions map[string]ccaccount.Session
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		profiles: map[string]ccaccount.Profile{},
		sessions: map[string]ccaccount.Session{},
	}
}

func (f *fakeStore) List(context.Context) ([]ccaccount.Profile, error) {
	out := make([]ccaccount.Profile, 0, len(f.profiles))
	for _, p := range f.profiles {
		out = append(out, p)
	}
	return out, nil
}

func (f *fakeStore) Load(_ context.Context, name string) (ccaccount.Session, error) {
	s, ok := f.sessions[name]
	if !ok {
		return ccaccount.Session{}, ccaccount.ErrEmptyProfile
	}
	return s, nil
}

func (f *fakeStore) Save(_ context.Context, p ccaccount.Profile, s ccaccount.Session) error {
	f.profiles[p.Name], f.sessions[p.Name] = p, s
	return nil
}

func (f *fakeStore) Delete(_ context.Context, name string) error {
	delete(f.profiles, name)
	delete(f.sessions, name)
	return nil
}

// setup monta o gerente com uma sessão pessoal ativa.
func setup(t *testing.T) (*ccaccount.Manager, *fakeVault, *fakeConfig, *fakeStore) {
	t.Helper()
	vault := &fakeVault{cred: credential("pro", now().Add(time.Hour), now().AddDate(0, 0, 30))}
	config := &fakeConfig{account: account("eu@exemplo.com", "Pessoal", "uuid-pessoal"), userID: "user-pessoal"}
	store := newFakeStore()
	return ccaccount.NewManager(vault, config, store, now), vault, config, store
}

// --- Testes ------------------------------------------------------------

func TestEstadoDescreveContaAtivaEPerfilQueACobre(t *testing.T) {
	m, _, _, _ := setup(t)
	ctx := context.Background()

	st, err := m.State(ctx)
	if err != nil {
		t.Fatalf("State: %v", err)
	}
	if !st.HasActive {
		t.Fatal("sessão ativa não foi reconhecida")
	}
	if st.Active.Email != "eu@exemplo.com" || st.Active.Plan != "pro" {
		t.Errorf("identidade lida errada: %+v", st.Active)
	}
	if st.ActiveProfile != "" {
		t.Errorf("sessão ainda não salva foi dada como guardada em %q", st.ActiveProfile)
	}

	if _, err := m.Save(ctx, "pessoal"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	st, _ = m.State(ctx)
	if st.ActiveProfile != "pessoal" {
		t.Errorf("depois de salvar, o perfil ativo devia ser “pessoal”, veio %q", st.ActiveProfile)
	}
}

func TestSemSessaoAtivaOEstadoAindaAbre(t *testing.T) {
	m := ccaccount.NewManager(&fakeVault{}, &fakeConfig{}, newFakeStore(), now)

	st, err := m.State(context.Background())
	if err != nil {
		t.Fatalf("faltar sessão não pode ser erro: %v", err)
	}
	if st.HasActive {
		t.Error("nenhuma credencial, mas o estado diz que há sessão")
	}
}

func TestSalvarRecusaSobrescreverOutraContaSemConfirmacao(t *testing.T) {
	m, vault, config, _ := setup(t)
	ctx := context.Background()

	if _, err := m.Save(ctx, "trabalho"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// A CLI passa a apontar para outra conta, e o usuário tenta guardá-la
	// sob o mesmo nome.
	vault.cred = credential("max", now().Add(time.Hour), now().AddDate(0, 0, 30))
	config.account = account("eu@empresa.com", "Empresa", "uuid-empresa")

	if _, err := m.Save(ctx, "trabalho"); !errors.Is(err, ccaccount.ErrProfileExists) {
		t.Fatalf("esperava ErrProfileExists, veio %v", err)
	}
	if _, err := m.SaveOverwriting(ctx, "trabalho"); err != nil {
		t.Fatalf("SaveOverwriting: %v", err)
	}

	st, _ := m.State(ctx)
	if st.ActiveProfile != "trabalho" {
		t.Errorf("depois de confirmar, o perfil devia cobrir a conta nova: %+v", st)
	}
}

func TestAtivarProtegeSessaoNaoGuardada(t *testing.T) {
	m, vault, config, _ := setup(t)
	ctx := context.Background()

	if _, err := m.Save(ctx, "pessoal"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Login em outra conta, fora do lealing: a sessão ativa deixa de estar
	// coberta por qualquer perfil.
	vault.cred = credential("max", now().Add(time.Hour), now().AddDate(0, 0, 30))
	config.account = account("eu@empresa.com", "Empresa", "uuid-empresa")

	if err := m.Activate(ctx, "pessoal"); !errors.Is(err, ccaccount.ErrActiveUnsaved) {
		t.Fatalf("esperava ErrActiveUnsaved, veio %v", err)
	}
	if string(config.account) == "" || string(vault.cred) == "" {
		t.Fatal("a recusa não pode ter tocado no cofre")
	}
	if got := string(config.account); got != string(account("eu@empresa.com", "Empresa", "uuid-empresa")) {
		t.Errorf("a identidade foi alterada apesar da recusa: %s", got)
	}
}

func TestAtivarRestauraCredencialEIdentidade(t *testing.T) {
	m, vault, config, _ := setup(t)
	ctx := context.Background()

	if _, err := m.Save(ctx, "pessoal"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	pessoal := vault.cred

	vault.cred = credential("max", now().Add(time.Hour), now().AddDate(0, 0, 30))
	config.account = account("eu@empresa.com", "Empresa", "uuid-empresa")
	config.userID = "user-empresa"
	if _, err := m.Save(ctx, "trabalho"); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if err := m.Activate(ctx, "pessoal"); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if string(vault.cred) != string(pessoal) {
		t.Error("a credencial do perfil não voltou ao cofre")
	}
	if config.userID != "user-pessoal" {
		t.Errorf("userID não acompanhou a troca: %q", config.userID)
	}

	st, _ := m.State(ctx)
	if st.Active.Email != "eu@exemplo.com" || st.ActiveProfile != "pessoal" {
		t.Errorf("estado após a troca: %+v", st)
	}
}

func TestAtivarPerfilInexistente(t *testing.T) {
	m, _, _, _ := setup(t)
	if err := m.Activate(context.Background(), "fantasma"); !errors.Is(err, ccaccount.ErrProfileNotFound) {
		t.Fatalf("esperava ErrProfileNotFound, veio %v", err)
	}
}

func TestEsquecerNaoDeslogaAContaEmUso(t *testing.T) {
	m, vault, _, store := setup(t)
	ctx := context.Background()

	if _, err := m.Save(ctx, "pessoal"); err != nil {
		t.Fatalf("Save: %v", err)
	}
	before := string(vault.cred)

	if err := m.Forget(ctx, "pessoal"); err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if len(store.profiles) != 0 {
		t.Error("o perfil continuou no store")
	}
	if string(vault.cred) != before {
		t.Error("esquecer o perfil mexeu na credencial ativa")
	}
}

func TestValidadeDaSessaoSeparaRenovavelDeMorta(t *testing.T) {
	renovavel := ccaccount.Describe(ccaccount.Session{
		Credential: credential("pro", now().Add(-time.Hour), now().AddDate(0, 0, 10)),
	})
	if !renovavel.Stale(now()) || renovavel.Dead(now()) {
		t.Error("token vencido com refresh válido devia ser só “renovável”")
	}

	morta := ccaccount.Describe(ccaccount.Session{
		Credential: credential("pro", now().Add(-48*time.Hour), now().Add(-time.Hour)),
	})
	if !morta.Dead(now()) {
		t.Error("refresh vencido devia exigir login novo")
	}
}

func TestNomeDePerfilRecusaOQueOCofreNaoAceita(t *testing.T) {
	casos := map[string]bool{
		"pessoal":       true,
		"conta de casa": true,
		"trabalho-2":    true,
		" espaços  ":    true, // é aparado, não recusado
		"":              false,
		"   ":           false,
		"caminho/ruim":  false,
		"dois:pontos":   false,
	}
	for entrada, ok := range casos {
		_, err := ccaccount.NormalizeName(entrada)
		if ok && err != nil {
			t.Errorf("%q devia ser aceito: %v", entrada, err)
		}
		if !ok && err == nil {
			t.Errorf("%q devia ser recusado", entrada)
		}
	}

	if got, _ := ccaccount.NormalizeName("  pessoal  "); got != "pessoal" {
		t.Errorf("espaços das pontas deviam sumir, veio %q", got)
	}
}

func TestSessaoIlegivelNaoDerrubaALeitura(t *testing.T) {
	id := ccaccount.Describe(ccaccount.Session{
		Credential: json.RawMessage(`{"claudeAiOauth":`), // truncado
		Account:    json.RawMessage(`não é json`),
	})
	if !id.Empty() {
		t.Errorf("blob quebrado devia render identidade vazia, veio %+v", id)
	}
}
