package usersync

import (
	"context"
	"time"
)

// Identity é quem está conectado. Guardamos o mínimo para a tela dizer de
// quem é a conta; nada disso é usado para autorizar coisa alguma.
type Identity struct {
	Login string `json:"login"`
	Name  string `json:"name,omitempty"`
}

func (i Identity) Empty() bool { return i.Login == "" }

// DeviceCode é o convite do device flow: o usuário digita UserCode na página
// indicada e a engine espera a aprovação.
type DeviceCode struct {
	// Code é o segredo trocado pelo token; nunca deve ser exibido, ao
	// contrário do UserCode.
	Code            string
	UserCode        string
	VerificationURL string
	Interval        time.Duration
	ExpiresAt       time.Time
}

// Credential é o token emitido pelo GitHub.
type Credential struct {
	Token      string    `json:"token"`
	Scope      string    `json:"scope,omitempty"`
	ObtainedAt time.Time `json:"obtainedAt"`
}

func (c Credential) Empty() bool { return c.Token == "" }

// Authenticator implementa o device flow. A porta existe para que o núcleo
// descreva o fluxo — pedir código, esperar aprovação, identificar — sem saber
// que existe HTTP do outro lado.
type Authenticator interface {
	RequestDevice(ctx context.Context) (DeviceCode, error)
	// Wait bloqueia até o usuário aprovar, negar ou o código expirar,
	// respeitando o intervalo de polling pedido pelo servidor.
	Wait(ctx context.Context, code DeviceCode) (Credential, error)
	Identity(ctx context.Context, credential Credential) (Identity, error)
}

// TokenStore guarda a credencial no cofre da plataforma.
type TokenStore interface {
	Load(ctx context.Context) (Credential, error)
	Save(ctx context.Context, credential Credential) error
	Delete(ctx context.Context) error
}

// Snapshot é o estado remoto com a revisão que o produziu. Revision é opaca
// para o núcleo: é o adapter que sabe se aquilo é um SHA do git ou outra
// coisa.
type Snapshot struct {
	State    State
	Revision string
	// Missing distingue "repositório vazio" de "erro ao ler", que pedem
	// mensagens opostas na tela.
	Missing bool
}

// Repository é o destino do estado.
type Repository struct {
	Owner   string
	Name    string
	Private bool
	URL     string
	// Created informa que este acesso foi quem criou o repositório.
	Created bool
}

func (r Repository) FullName() string {
	if r.Owner == "" {
		return r.Name
	}
	return r.Owner + "/" + r.Name
}

// Remote é o repositório privado do usuário.
type Remote interface {
	// Ensure garante que o repositório existe e é privado, criando-o na
	// primeira vez.
	Ensure(ctx context.Context, credential Credential, name string) (Repository, error)
	Read(ctx context.Context, credential Credential, name string) (Snapshot, error)
	// Write grava o documento. expected é a revisão que o chamador acredita
	// estar publicada; o adapter recusa a escrita se ela já não for a atual.
	Write(ctx context.Context, credential Credential, name string, state State, expected string) (string, error)
}

// Applied conta o que uma aplicação local mudou, por seção.
type Applied map[Section]int

// Local lê e escreve o estado desta máquina. Cada seção mora num store
// diferente da engine; o composition root é quem os reúne.
type Local interface {
	Collect(ctx context.Context) (State, error)
	Apply(ctx context.Context, state State, selection Selection) (Applied, error)
}

// Settings é o que fica no disco desta máquina sobre a sincronização.
type Settings struct {
	Identity   Identity  `json:"identity"`
	Repository string    `json:"repository"`
	Sections   []Section `json:"sections"`
	// Revision é a última revisão remota que esta máquina conhece. É o que
	// transforma "sobrescrever cegamente" em "detectar conflito".
	Revision string    `json:"revision,omitempty"`
	LastSync time.Time `json:"lastSync,omitempty"`
}

// Selection materializa as seções habilitadas.
func (s Settings) Selection() Selection {
	selection := make(Selection, len(s.Sections))
	for _, section := range s.Sections {
		if section.Valid() {
			selection[section] = true
		}
	}
	return selection
}

// WithSelection devolve os ajustes com as seções na ordem canônica.
func (s Settings) WithSelection(selection Selection) Settings {
	sections := make([]Section, 0, len(AllSections))
	for _, section := range AllSections {
		if selection.Enabled(section) {
			sections = append(sections, section)
		}
	}
	s.Sections = sections
	return s
}

// SettingsStore persiste os ajustes desta máquina.
type SettingsStore interface {
	Load(ctx context.Context) (Settings, error)
	Save(ctx context.Context, settings Settings) error
}
