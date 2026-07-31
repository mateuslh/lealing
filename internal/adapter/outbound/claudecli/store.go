package claudecli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mateuslh/lealing/internal/core/ccaccount"
	"github.com/mateuslh/lealing/internal/platform/xdg"
)

// Store guarda os perfis em duas metades: os metadados legíveis em um índice
// JSON e a credencial no cofre da plataforma.
//
// A separação é o ponto: o índice pode ser lido, versionado e inspecionado à
// vontade porque não tem segredo nenhum; o token nunca sai do lugar que o
// sistema operacional sabe proteger.
type Store struct {
	IndexPath string
	secrets   secretStore
}

var _ ccaccount.Store = (*Store)(nil)

// NewStore monta o store com o índice no caminho dado. O cofre dos segredos
// é escolhido pela plataforma.
func NewStore(indexPath string) *Store {
	return newStore(indexPath, newSecretStore(filepath.Dir(indexPath)))
}

// newStore é o construtor com o cofre explícito. Existe para o teste poder
// exercitar o índice sem tocar no chaveiro da máquina.
func newStore(indexPath string, secrets secretStore) *Store {
	return &Store{IndexPath: indexPath, secrets: secrets}
}

// secretStore é o cofre onde as credenciais dos perfis ficam. Uma chave por
// perfil.
type secretStore interface {
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte) error
	Delete(ctx context.Context, key string) error
}

// --- Formato em disco --------------------------------------------------

// indexFile é o índice serializado. A versão existe para que um formato
// futuro saiba o que está lendo em vez de adivinhar.
type indexFile struct {
	Version  int             `json:"version"`
	Profiles []profileRecord `json:"profiles"`
}

// profileRecord é a forma serializada de ccaccount.Profile. O DTO separado
// impede que renomear um campo do domínio invalide os perfis já gravados.
type profileRecord struct {
	Name         string    `json:"name"`
	Email        string    `json:"email,omitempty"`
	DisplayName  string    `json:"display_name,omitempty"`
	Organization string    `json:"organization,omitempty"`
	AccountUUID  string    `json:"account_uuid,omitempty"`
	Plan         string    `json:"plan,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitzero"`
	RenewsUntil  time.Time `json:"renews_until,omitzero"`
	SavedAt      time.Time `json:"saved_at"`
}

// sessionRecord é a credencial serializada dentro do cofre.
type sessionRecord struct {
	Credential json.RawMessage `json:"credential"`
	Account    json.RawMessage `json:"account,omitempty"`
	UserID     string          `json:"user_id,omitempty"`
}

const indexVersion = 1

// --- Porta -------------------------------------------------------------

// List implementa ccaccount.Store.
func (s *Store) List(context.Context) ([]ccaccount.Profile, error) {
	idx, err := s.read()
	if err != nil {
		return nil, err
	}
	out := make([]ccaccount.Profile, 0, len(idx.Profiles))
	for _, r := range idx.Profiles {
		out = append(out, r.toDomain())
	}
	// Ordem alfabética, e não por data: a lista é curta e navegada com as
	// setas — uma ordem que muda a cada gravação faria o cursor pular.
	slices.SortFunc(out, func(a, b ccaccount.Profile) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return out, nil
}

// Load implementa ccaccount.Store.
func (s *Store) Load(ctx context.Context, name string) (ccaccount.Session, error) {
	raw, err := s.secrets.Get(ctx, s.canonical(name))
	if err != nil {
		return ccaccount.Session{}, ccaccount.ErrEmptyProfile
	}
	var rec sessionRecord
	if err := json.Unmarshal(raw, &rec); err != nil {
		return ccaccount.Session{}, ccaccount.ErrEmptyProfile
	}
	return ccaccount.Session{
		Credential: rec.Credential,
		Account:    rec.Account,
		UserID:     rec.UserID,
	}, nil
}

// Save implementa ccaccount.Store.
//
// O segredo é gravado antes do índice: se o cofre recusar, o índice segue
// descrevendo apenas perfis que existem de fato. A ordem inversa produziria
// um perfil listado e vazio, que só se descobre na hora de ativar.
func (s *Store) Save(ctx context.Context, profile ccaccount.Profile, session ccaccount.Session) error {
	payload, err := json.Marshal(sessionRecord{
		Credential: session.Credential,
		Account:    session.Account,
		UserID:     session.UserID,
	})
	if err != nil {
		return err
	}
	if err := s.secrets.Set(ctx, profile.Name, payload); err != nil {
		return err
	}

	idx, err := s.read()
	if err != nil {
		return err
	}
	rec := fromDomain(profile)
	replaced := false
	for i := range idx.Profiles {
		if strings.EqualFold(idx.Profiles[i].Name, profile.Name) {
			idx.Profiles[i] = rec
			replaced = true
			break
		}
	}
	if !replaced {
		idx.Profiles = append(idx.Profiles, rec)
	}
	return s.write(idx)
}

// Delete implementa ccaccount.Store.
func (s *Store) Delete(ctx context.Context, name string) error {
	idx, err := s.read()
	if err != nil {
		return err
	}
	// A chave do cofre é o nome como foi gravado; resolver antes de apagar o
	// registro é o que evita deixar o segredo órfão quando quem chama
	// escreveu o nome com outra caixa.
	key := s.canonical(name)
	idx.Profiles = slices.DeleteFunc(idx.Profiles, func(r profileRecord) bool {
		return strings.EqualFold(r.Name, name)
	})
	if err := s.write(idx); err != nil {
		return err
	}
	// O índice já não cita o perfil; um segredo órfão no cofre é ruído, não
	// inconsistência, então o erro daqui não desfaz a remoção.
	_ = s.secrets.Delete(ctx, key)
	return nil
}

// canonical devolve o nome como está no índice, para que a chave do cofre
// seja sempre a mesma que foi usada na gravação.
func (s *Store) canonical(name string) string {
	idx, err := s.read()
	if err != nil {
		return name
	}
	for _, r := range idx.Profiles {
		if strings.EqualFold(r.Name, name) {
			return r.Name
		}
	}
	return name
}

// --- Arquivo -----------------------------------------------------------

func (s *Store) read() (indexFile, error) {
	raw, err := os.ReadFile(s.IndexPath)
	if errors.Is(err, os.ErrNotExist) {
		return indexFile{Version: indexVersion}, nil
	}
	if err != nil {
		return indexFile{}, err
	}
	var idx indexFile
	if err := json.Unmarshal(raw, &idx); err != nil {
		return indexFile{}, err
	}
	return idx, nil
}

func (s *Store) write(idx indexFile) error {
	idx.Version = indexVersion
	if idx.Profiles == nil {
		idx.Profiles = []profileRecord{}
	}
	out, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	if err := xdg.MkdirAll(filepath.Dir(s.IndexPath), 0o700); err != nil {
		return err
	}
	return writeAtomic(s.IndexPath, out, 0o600)
}

// --- Conversões --------------------------------------------------------

func (r profileRecord) toDomain() ccaccount.Profile {
	return ccaccount.Profile{
		Name:    r.Name,
		SavedAt: r.SavedAt,
		Identity: ccaccount.Identity{
			Email:        r.Email,
			DisplayName:  r.DisplayName,
			Organization: r.Organization,
			AccountUUID:  r.AccountUUID,
			Plan:         r.Plan,
			ExpiresAt:    r.ExpiresAt,
			RenewsUntil:  r.RenewsUntil,
		},
	}
}

func fromDomain(p ccaccount.Profile) profileRecord {
	return profileRecord{
		Name:         p.Name,
		Email:        p.Identity.Email,
		DisplayName:  p.Identity.DisplayName,
		Organization: p.Identity.Organization,
		AccountUUID:  p.Identity.AccountUUID,
		Plan:         p.Identity.Plan,
		ExpiresAt:    p.Identity.ExpiresAt,
		RenewsUntil:  p.Identity.RenewsUntil,
		SavedAt:      p.SavedAt,
	}
}
