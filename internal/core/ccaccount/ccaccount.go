// Package ccaccount é o domínio da tool "Contas do Claude Code": guardar as
// sessões de mais de uma conta e devolver a escolhida ao lugar de onde a CLI
// a lê.
//
// Trocar de conta é mover duas coisas em conjunto: a credencial OAuth, que
// vive no cofre da plataforma (chaveiro no macOS, arquivo no Windows), e o
// bloco de identidade gravado em ~/.claude.json. Mover só a primeira deixa a
// CLI autenticada como uma conta e exibindo outra — por isso o domínio trata
// as duas como uma unidade indivisível, a Session.
package ccaccount

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode"
)

// Erros do domínio. São sentinelas porque a tela decide o texto e a ação a
// partir deles — de "pedir confirmação" a "mandar rodar `claude`".
var (
	ErrNoActiveSession = errors.New("nenhuma sessão ativa do Claude Code — rode `claude` e faça login")
	ErrProfileNotFound = errors.New("perfil não encontrado")
	ErrProfileExists   = errors.New("já existe um perfil com esse nome, de outra conta")
	ErrActiveUnsaved   = errors.New("a sessão ativa não está guardada em nenhum perfil")
	ErrEmptyProfile    = errors.New("o perfil guardado não tem credencial")
	ErrInvalidName     = errors.New("nome inválido: letras, números, espaço, hífen, ponto ou sublinhado, até 40 caracteres")
)

// NameLimit é o tamanho máximo do nome de um perfil. O nome vira chave no
// cofre da plataforma, então é curto de propósito.
const NameLimit = 40

// Session é o estado completo de uma conta: o que a CLI precisa encontrar
// para se considerar logada.
type Session struct {
	// Credential é o JSON cru que o Claude Code guarda no cofre. Guardamos
	// o blob inteiro, e não os campos que sabemos ler, porque ele carrega o
	// refresh token — sem ele a sessão restaurada morreria em uma hora.
	Credential json.RawMessage
	// Account é o valor cru de "oauthAccount" em ~/.claude.json.
	Account json.RawMessage
	// UserID é o campo "userID" do mesmo arquivo.
	UserID string
}

// Valid informa se a sessão tem o mínimo para ser restaurada.
func (s Session) Valid() bool { return len(s.Credential) > 0 }

// Identity são os campos legíveis de uma conta, extraídos da sessão.
type Identity struct {
	Email        string
	DisplayName  string
	Organization string
	AccountUUID  string
	// Plan é o subscriptionType da credencial: "pro", "max", "team"…
	Plan string
	// ExpiresAt é a validade do access token. Vencido não é problema: a CLI
	// renova sozinha no próximo uso.
	ExpiresAt time.Time
	// RenewsUntil é a validade do refresh token. Vencido esse, só um novo
	// login resolve.
	RenewsUntil time.Time
}

// Label é o melhor nome disponível para mostrar na tela.
func (i Identity) Label() string {
	switch {
	case i.Email != "":
		return i.Email
	case i.DisplayName != "":
		return i.DisplayName
	case i.AccountUUID != "":
		return i.AccountUUID
	default:
		return "conta desconhecida"
	}
}

// Empty informa se nada foi identificado.
func (i Identity) Empty() bool {
	return i.Email == "" && i.DisplayName == "" && i.AccountUUID == "" && i.Plan == ""
}

// Stale informa que o access token venceu — a CLI renova sozinha.
func (i Identity) Stale(now time.Time) bool {
	return !i.ExpiresAt.IsZero() && !i.ExpiresAt.After(now)
}

// Dead informa que nem o refresh token vale mais: só um login novo resolve.
func (i Identity) Dead(now time.Time) bool {
	return !i.RenewsUntil.IsZero() && !i.RenewsUntil.After(now)
}

// SameAccount compara duas identidades pelo que de fato as identifica.
//
// O UUID é a chave; o e-mail entra como reserva porque uma sessão lida antes
// da CLI popular o ~/.claude.json não tem UUID nenhum.
func (i Identity) SameAccount(other Identity) bool {
	if i.AccountUUID != "" && other.AccountUUID != "" {
		return i.AccountUUID == other.AccountUUID
	}
	if i.Email != "" && other.Email != "" {
		return strings.EqualFold(i.Email, other.Email)
	}
	return false
}

// Profile é uma sessão guardada sob um nome.
type Profile struct {
	Name     string
	Identity Identity
	SavedAt  time.Time
}

// --- Portas de saída ---------------------------------------------------

// Vault é o cofre onde a CLI guarda a credencial ativa.
type Vault interface {
	Read(ctx context.Context) (json.RawMessage, error)
	Write(ctx context.Context, credential json.RawMessage) error
	// Origin descreve para o usuário onde essa credencial mora.
	Origin() string
}

// Config é o ~/.claude.json, do qual só a identidade da conta nos interessa.
type Config interface {
	Account(ctx context.Context) (account json.RawMessage, userID string, err error)
	SetAccount(ctx context.Context, account json.RawMessage, userID string) error
}

// Store é onde o lealing guarda os perfis.
type Store interface {
	List(ctx context.Context) ([]Profile, error)
	Load(ctx context.Context, name string) (Session, error)
	Save(ctx context.Context, profile Profile, session Session) error
	Delete(ctx context.Context, name string) error
}

// Switcher é a porta de entrada da tool: tudo o que a tela pode pedir.
// Manter o contrato explícito é o que permite testar a tela com um duplo,
// sem chaveiro nem ~/.claude.json por perto.
type Switcher interface {
	State(ctx context.Context) (State, error)
	Save(ctx context.Context, name string) (Profile, error)
	SaveOverwriting(ctx context.Context, name string) (Profile, error)
	Activate(ctx context.Context, name string) error
	ActivateOverwriting(ctx context.Context, name string) error
	Forget(ctx context.Context, name string) error
}

// --- Serviço -----------------------------------------------------------

// Manager é o caso de uso da tool: ler o estado, guardar a sessão ativa e
// restaurar uma guardada.
type Manager struct {
	vault  Vault
	config Config
	store  Store
	now    func() time.Time
}

var _ Switcher = (*Manager)(nil)

// NewManager monta o serviço.
func NewManager(vault Vault, config Config, store Store, now func() time.Time) *Manager {
	if now == nil {
		now = time.Now
	}
	return &Manager{vault: vault, config: config, store: store, now: now}
}

// State é o retrato que a tela desenha.
type State struct {
	// Active é a conta em que a CLI está agora; vazia se não houver sessão.
	Active    Identity
	HasActive bool
	// ActiveProfile é o nome do perfil que corresponde à sessão ativa, ou
	// vazio se ela não está guardada em lugar nenhum.
	ActiveProfile string
	// Origin é onde a credencial ativa mora, para a tela dizer ao usuário.
	Origin   string
	Profiles []Profile
}

// State lê o estado atual. Falta de sessão ativa não é erro: é o retrato de
// quem ainda não fez login, e a tela precisa desenhá-lo.
func (m *Manager) State(ctx context.Context) (State, error) {
	profiles, err := m.store.List(ctx)
	if err != nil {
		return State{}, err
	}

	st := State{Profiles: profiles}

	session, err := m.Current(ctx)
	// Origin só é confiável depois da leitura: é ela que descobre em qual
	// das fontes possíveis a sessão está.
	st.Origin = m.vault.Origin()
	if errors.Is(err, ErrNoActiveSession) {
		return st, nil
	}
	if err != nil {
		return st, err
	}

	st.Active = Describe(session)
	st.HasActive = true
	if p, ok := matchProfile(profiles, st.Active); ok {
		st.ActiveProfile = p.Name
	}
	return st, nil
}

// Current lê a sessão ativa juntando cofre e configuração.
func (m *Manager) Current(ctx context.Context) (Session, error) {
	cred, err := m.vault.Read(ctx)
	if err != nil || len(cred) == 0 {
		return Session{}, ErrNoActiveSession
	}
	// A identidade é acessório: uma credencial sem ~/.claude.json ainda é
	// uma sessão válida, só menos informativa.
	account, userID, _ := m.config.Account(ctx)
	return Session{Credential: cred, Account: account, UserID: userID}, nil
}

// Save guarda a sessão ativa sob um nome.
//
// Devolve ErrProfileExists quando o nome já pertence a outra conta:
// sobrescrever ali apagaria a única cópia de uma credencial que o usuário
// não tem como recuperar sem refazer o login.
func (m *Manager) Save(ctx context.Context, name string) (Profile, error) {
	return m.save(ctx, name, false)
}

// SaveOverwriting é o Save depois de o usuário confirmar a substituição.
func (m *Manager) SaveOverwriting(ctx context.Context, name string) (Profile, error) {
	return m.save(ctx, name, true)
}

func (m *Manager) save(ctx context.Context, name string, force bool) (Profile, error) {
	clean, err := NormalizeName(name)
	if err != nil {
		return Profile{}, err
	}

	session, err := m.Current(ctx)
	if err != nil {
		return Profile{}, err
	}
	identity := Describe(session)

	if !force {
		existing, err := m.store.List(ctx)
		if err != nil {
			return Profile{}, err
		}
		for _, p := range existing {
			if strings.EqualFold(p.Name, clean) && !p.Identity.SameAccount(identity) {
				return Profile{}, ErrProfileExists
			}
		}
	}

	profile := Profile{Name: clean, Identity: identity, SavedAt: m.now()}
	if err := m.store.Save(ctx, profile, session); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

// Activate devolve a sessão do perfil ao cofre e ao ~/.claude.json.
//
// Devolve ErrActiveUnsaved quando a sessão que seria sobrescrita não está
// guardada em nenhum perfil — é a única proteção possível, porque depois da
// escrita a credencial anterior não existe mais em lugar nenhum.
func (m *Manager) Activate(ctx context.Context, name string) error {
	return m.activate(ctx, name, false)
}

// ActivateOverwriting é o Activate depois de o usuário confirmar a perda.
func (m *Manager) ActivateOverwriting(ctx context.Context, name string) error {
	return m.activate(ctx, name, true)
}

func (m *Manager) activate(ctx context.Context, name string, force bool) error {
	profiles, err := m.store.List(ctx)
	if err != nil {
		return err
	}
	target, ok := findProfile(profiles, name)
	if !ok {
		return ErrProfileNotFound
	}

	session, err := m.store.Load(ctx, target.Name)
	if err != nil {
		return err
	}
	if !session.Valid() {
		return ErrEmptyProfile
	}

	if !force {
		if current, err := m.Current(ctx); err == nil {
			active := Describe(current)
			if _, saved := matchProfile(profiles, active); !saved {
				return ErrActiveUnsaved
			}
		}
	}

	// O cofre vem primeiro: se a identidade falhar depois dele, a CLI fica
	// autenticada na conta certa mostrando o nome antigo — feio, mas
	// funcional. Na ordem inversa ficaria o contrário, que confunde de
	// verdade.
	if err := m.vault.Write(ctx, session.Credential); err != nil {
		return err
	}
	if len(session.Account) > 0 {
		if err := m.config.SetAccount(ctx, session.Account, session.UserID); err != nil {
			return err
		}
	}
	return nil
}

// Forget apaga um perfil. A sessão ativa não é tocada: esquecer o perfil da
// conta em uso não desloga ninguém.
func (m *Manager) Forget(ctx context.Context, name string) error {
	profiles, err := m.store.List(ctx)
	if err != nil {
		return err
	}
	target, ok := findProfile(profiles, name)
	if !ok {
		return ErrProfileNotFound
	}
	return m.store.Delete(ctx, target.Name)
}

// --- Regras puras ------------------------------------------------------

// NormalizeName valida e limpa o nome de um perfil.
func NormalizeName(name string) (string, error) {
	clean := strings.TrimSpace(name)
	if clean == "" || len([]rune(clean)) > NameLimit {
		return "", ErrInvalidName
	}
	for _, r := range clean {
		if !AllowedInName(r) {
			return "", ErrInvalidName
		}
	}
	return clean, nil
}

// AllowedInName informa se um caractere pode entrar no nome de um perfil.
//
// O conjunto é restrito porque o nome vira chave no cofre da plataforma e
// nome de conta no chaveiro do macOS: barras, dois-pontos e caracteres de
// controle não têm o mesmo significado nos dois lados.
func AllowedInName(r rune) bool {
	switch {
	case unicode.IsLetter(r), unicode.IsDigit(r):
		return true
	case r == ' ', r == '-', r == '_', r == '.':
		return true
	default:
		return false
	}
}

// Describe extrai a identidade dos dois blobs de uma sessão.
//
// Blob ilegível vira campo vazio, nunca erro: uma tela que se recusa a abrir
// porque a CLI renomeou um campo é pior que uma tela com um traço.
func Describe(s Session) Identity {
	var id Identity

	var cred struct {
		OAuth struct {
			ExpiresAt             int64  `json:"expiresAt"`
			RefreshTokenExpiresAt int64  `json:"refreshTokenExpiresAt"`
			SubscriptionType      string `json:"subscriptionType"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(s.Credential, &cred); err == nil {
		id.Plan = cred.OAuth.SubscriptionType
		// As duas datas vêm em milissegundos desde a época.
		if ms := cred.OAuth.ExpiresAt; ms > 0 {
			id.ExpiresAt = time.UnixMilli(ms)
		}
		if ms := cred.OAuth.RefreshTokenExpiresAt; ms > 0 {
			id.RenewsUntil = time.UnixMilli(ms)
		}
	}

	var account struct {
		EmailAddress     string `json:"emailAddress"`
		DisplayName      string `json:"displayName"`
		OrganizationName string `json:"organizationName"`
		AccountUUID      string `json:"accountUuid"`
	}
	if err := json.Unmarshal(s.Account, &account); err == nil {
		id.Email = account.EmailAddress
		id.DisplayName = account.DisplayName
		id.Organization = account.OrganizationName
		id.AccountUUID = account.AccountUUID
	}
	return id
}

// findProfile procura pelo nome, ignorando maiúsculas.
func findProfile(profiles []Profile, name string) (Profile, bool) {
	for _, p := range profiles {
		if strings.EqualFold(p.Name, strings.TrimSpace(name)) {
			return p, true
		}
	}
	return Profile{}, false
}

// matchProfile procura o perfil que guarda a conta descrita.
func matchProfile(profiles []Profile, id Identity) (Profile, bool) {
	for _, p := range profiles {
		if p.Identity.SameAccount(id) {
			return p, true
		}
	}
	return Profile{}, false
}
