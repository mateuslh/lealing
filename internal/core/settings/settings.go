// Package settings é a configuração da engine: campos declarados uma vez,
// com tipo, validação e origem do valor.
//
// O catálogo de campos é declarativo pelo mesmo motivo do catálogo de tools:
// a tela desenha o que existe aqui, sem uma linha de código por ajuste. E,
// como todo core, este pacote não lê ambiente nem disco — as duas coisas
// entram por porta, o que torna a precedência testável sem exportar variável
// nenhuma.
package settings

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// Key identifica um campo. É estável: mudá-la descarta o valor que o usuário
// já tinha gravado.
type Key string

const (
	KeyGitHubClientID    Key = "github.client_id"
	KeyGreetingName      Key = "home.greeting_name"
	KeyMarketplaceIndex  Key = "marketplace.index_url"
	KeyMarketplaceOnHome Key = "marketplace.check_on_home"
)

// Kind decide como a tela edita o campo.
type Kind uint8

const (
	// KindText é editado em um campo de texto.
	KindText Kind = iota
	// KindToggle alterna entre "true" e "false".
	KindToggle
)

// Section agrupa campos na navegação.
type Section struct {
	ID          string
	Name        string
	Glyph       string
	Description string
}

var (
	SectionAccount     = Section{ID: "account", Name: "Conta", Glyph: "☁", Description: "identidade no GitHub e sincronização"}
	SectionMarketplace = Section{ID: "marketplace", Name: "Marketplace", Glyph: "✦", Description: "de onde as tools vêm"}
	SectionAppearance  = Section{ID: "appearance", Name: "Aparência", Glyph: "◈", Description: "como a home se apresenta"}
	SectionEnvironment = Section{ID: "environment", Name: "Ambiente", Glyph: "⌬", Description: "onde a engine guarda as coisas"}
)

// Sections é a ordem em que aparecem na tela.
func Sections() []Section {
	return []Section{SectionAccount, SectionMarketplace, SectionAppearance, SectionEnvironment}
}

// Field descreve um ajuste.
type Field struct {
	Key         Key
	Section     string
	Label       string
	Description string
	Kind        Kind
	// Default é o valor quando nada foi definido. O composition root pode
	// sobrescrevê-lo para campos que só ele conhece, como o client_id gravado
	// no build.
	Default     string
	Placeholder string
	// EnvVar permite sobrescrever pelo ambiente, útil em desenvolvimento e em
	// máquina compartilhada. O valor gravado pelo usuário tem precedência:
	// a tela não pode mostrar um número e a engine usar outro.
	EnvVar string
	// Restart marca o que só vale na próxima abertura. Dizer isso é o que
	// separa uma configuração honesta de uma que parece não funcionar.
	Restart  bool
	Validate func(string) error
}

// Fields é o catálogo de ajustes.
func Fields() []Field {
	return []Field{
		{
			Key: KeyGitHubClientID, Section: SectionAccount.ID,
			Label:       "Client ID do OAuth App",
			Description: "Aplicativo usado no login por device flow. Só troque se você registrou o seu próprio; vazio volta ao aplicativo do lealing.",
			Kind:        KindText, Placeholder: "Ov23li…", EnvVar: "LEALING_GITHUB_CLIENT_ID",
			Validate: validateClientID,
		},
		{
			Key: KeyMarketplaceIndex, Section: SectionMarketplace.ID,
			Label:       "Índice oficial",
			Description: "URL HTTPS do índice embutido. É a única origem que pode publicar nos canais official e verified.",
			Kind:        KindText, Placeholder: "https://…/index.json",
			Restart: true, Validate: validateHTTPS,
		},
		{
			Key: KeyMarketplaceOnHome, Section: SectionMarketplace.ID,
			Label:       "Consultar origens na home",
			Description: "Desligue para a home não falar com a rede ao abrir. A vitrine passa a consultar só quando você pedir.",
			Kind:        KindToggle, Default: "true",
		},
		{
			Key: KeyGreetingName, Section: SectionAppearance.ID,
			Label:       "Nome na saudação",
			Description: "Como a home chama você. Vazio usa o nome de usuário do sistema.",
			Kind:        KindText, Placeholder: "seu nome", Validate: validateGreeting,
		},
	}
}

// Source diz de onde veio o valor em uso. A tela mostra isso porque
// "definido por você" e "padrão do build" pedem ações diferentes quando algo
// não funciona.
type Source uint8

const (
	SourceDefault Source = iota
	SourceEnv
	SourceUser
)

func (s Source) Label() string {
	switch s {
	case SourceUser:
		return "definido por você"
	case SourceEnv:
		return "variável de ambiente"
	default:
		return "padrão"
	}
}

// Value é um campo com o valor em vigor.
type Value struct {
	Field
	Current string
	Source  Source
}

// Bool interpreta o valor de um KindToggle.
func (v Value) Bool() bool { return v.Current == "true" }

// InfoRow é uma linha somente-leitura preenchida pelo composition root —
// caminhos de disco, versão, plataforma. Não é ajuste: é o contexto que
// responde "onde isso foi parar?" sem abrir um terminal.
type InfoRow struct {
	Section string
	Label   string
	Value   string
}

// Store persiste apenas o que o usuário mudou. Guardar os padrões junto
// congelaria decisões que a engine deve poder revisar em uma atualização.
type Store interface {
	Load() (map[string]string, error)
	Save(values map[string]string) error
}

var (
	ErrUnknownField = errors.New("ajuste desconhecido")

	validClientID = regexp.MustCompile(`^[A-Za-z0-9._-]{8,100}$`)
	validRepoName = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
)

func validateClientID(value string) error {
	if value == "" || validClientID.MatchString(value) {
		return nil
	}
	return errors.New("um client ID tem letras, números, ponto, hífen ou sublinhado")
}

func validateHTTPS(value string) error {
	if value == "" {
		return nil
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
		return errors.New("informe uma URL HTTPS completa")
	}
	return nil
}

func validateGreeting(value string) error {
	switch {
	case strings.ContainsAny(value, "\r\n\x00"):
		return errors.New("o nome precisa ter uma linha só")
	case len([]rune(value)) > 40:
		return errors.New("o nome precisa ter até 40 caracteres")
	}
	return nil
}

// ValidateRepositoryName é usada pelo composition root ao aceitar o nome do
// repositório de estado; mora aqui para a regra não se bifurcar.
func ValidateRepositoryName(value string) error {
	if validRepoName.MatchString(value) {
		return nil
	}
	return fmt.Errorf("nome de repositório inválido: %q", value)
}
