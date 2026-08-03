package marketplace

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

// OriginKind separa o que a engine baixa do que ela apenas lê no disco.
//
// A distinção não é cosmética: uma origem remota exige HTTPS e checksum em
// todo artefato, enquanto uma origem local aponta para um diretório que o
// próprio usuário acabou de compilar — cobrar SHA-256 de um build que muda a
// cada `go build` tornaria o repositório de desenvolvimento inutilizável.
type OriginKind string

const (
	OriginRemote OriginKind = "remote"
	OriginLocal  OriginKind = "local"
)

func (k OriginKind) Valid() bool { return k == OriginRemote || k == OriginLocal }

// Origin é um repositório de tools. O marketplace é a soma das origens
// habilitadas, não um endereço único: qualquer pessoa pode publicar um índice
// paralelo e apontar a engine para ele.
type Origin struct {
	// Name é o identificador estável da origem, usado nas referências
	// qualificadas "origem/tool" e como chave de persistência.
	Name string `json:"name"`
	// Label é o nome de exibição; vazio cai no Name.
	Label string     `json:"label,omitempty"`
	Kind  OriginKind `json:"kind"`
	// Ref é a URL HTTPS do índice ou o caminho do diretório/arquivo local.
	Ref string `json:"ref"`
	// Trusted autoriza a origem a publicar nos canais official e verified.
	// Só o composition root marca isto; origens adicionadas pelo usuário
	// entram sempre como community, para que o selo de canal continue
	// significando algo.
	Trusted bool `json:"-"`
	// Builtin marca as origens que acompanham a engine: podem ser desligadas,
	// nunca removidas.
	Builtin bool `json:"-"`
	Enabled bool `json:"enabled"`
	// Priority decide conflitos de ID entre origens; menor vence.
	Priority int `json:"-"`
}

// Title devolve o rótulo de exibição.
func (o Origin) Title() string {
	if strings.TrimSpace(o.Label) != "" {
		return o.Label
	}
	return o.Name
}

// Validate confere o que o usuário digitou antes de a origem ser persistida.
func (o Origin) Validate() error {
	switch {
	case !validID.MatchString(o.Name):
		return fmt.Errorf("nome de origem inválido: %q (use letras minúsculas, números e hífen)", o.Name)
	case !o.Kind.Valid():
		return fmt.Errorf("tipo de origem desconhecido em %s: %q", o.Name, o.Kind)
	case strings.TrimSpace(o.Ref) == "":
		return fmt.Errorf("origem %s não tem endereço", o.Name)
	case strings.ContainsAny(o.Ref, "\r\n\x00"):
		return fmt.Errorf("endereço da origem %s contém caracteres de controle", o.Name)
	case o.Kind == OriginRemote && !safeHTTPS(o.Ref):
		return fmt.Errorf("origem remota %s precisa usar HTTPS", o.Name)
	case strings.ContainsAny(o.Label, "\r\n"):
		return fmt.Errorf("rótulo da origem %s precisa ter uma linha", o.Name)
	}
	return nil
}

// windowsAbsolute reconhece "C:\repo" e "C:/repo". Sem isto, um caminho do
// Windows cairia na mensagem genérica de endereço desconhecido.
var windowsAbsolute = regexp.MustCompile(`^[A-Za-z]:[\\/]`)

// NewOrigin interpreta o endereço que o usuário digitou e devolve a origem
// pronta para ser cadastrada. Quando o nome vem vazio, ele é derivado do
// próprio endereço: obrigar alguém a inventar um slug antes de colar uma URL
// é a fricção que faz um recurso de repositórios paralelos não ser usado.
func NewOrigin(name, label, ref string) (Origin, error) {
	ref = strings.TrimSpace(ref)
	origin := Origin{
		Name:    slug(name),
		Label:   strings.TrimSpace(label),
		Ref:     ref,
		Enabled: true,
	}
	switch {
	case ref == "":
		return Origin{}, errors.New("informe a URL do índice ou o caminho do repositório local")
	case strings.HasPrefix(ref, "https://"):
		origin.Kind = OriginRemote
	case strings.HasPrefix(ref, "http://"):
		return Origin{}, errors.New("índice remoto precisa usar HTTPS")
	case strings.HasPrefix(ref, "file://"), strings.HasPrefix(ref, "/"), windowsAbsolute.MatchString(ref):
		origin.Kind = OriginLocal
	default:
		return Origin{}, fmt.Errorf(
			"endereço %q não é uma URL HTTPS nem um caminho absoluto", ref)
	}
	if origin.Name == "" {
		origin.Name = deriveName(origin.Kind, ref)
	}
	if err := origin.Validate(); err != nil {
		return Origin{}, err
	}
	return origin, nil
}

// deriveName escolhe um slug legível a partir do endereço.
func deriveName(kind OriginKind, ref string) string {
	if kind == OriginLocal {
		trimmed := strings.TrimSuffix(strings.TrimPrefix(ref, "file://"), "/")
		segments := splitSegments(trimmed)
		for position := len(segments) - 1; position >= 0; position-- {
			// O último segmento costuma ser "marketplace" ou o index.json; o
			// nome do repositório é o que identifica a origem para quem lê a
			// lista depois.
			if candidate := slug(segments[position]); candidate != "" &&
				candidate != "marketplace" && !strings.HasSuffix(segments[position], ".json") {
				return candidate
			}
		}
		return ""
	}

	parsed, err := url.Parse(ref)
	if err != nil || parsed.Host == "" {
		return ""
	}
	segments := splitSegments(parsed.Path)
	// Em hospedagens de código o dono e o repositório dizem muito mais que o
	// host, que seria "raw" para todo mundo.
	if len(segments) >= 2 && strings.Contains(parsed.Host, "github") {
		return slug(segments[0] + "-" + segments[1])
	}
	return slug(strings.TrimPrefix(parsed.Host, "www."))
}

func splitSegments(path string) []string {
	raw := strings.FieldsFunc(strings.ReplaceAll(path, `\`, "/"), func(r rune) bool { return r == '/' })
	segments := make([]string, 0, len(raw))
	for _, segment := range raw {
		if segment != "" && segment != "." {
			segments = append(segments, segment)
		}
	}
	return segments
}

// slug reduz um texto livre ao alfabeto aceito em Name.
func slug(value string) string {
	var builder strings.Builder
	previousHyphen := true // evita começar com hífen
	for _, symbol := range strings.ToLower(strings.TrimSpace(value)) {
		switch {
		case (symbol >= 'a' && symbol <= 'z') || (symbol >= '0' && symbol <= '9'):
			builder.WriteRune(symbol)
			previousHyphen = false
		case !previousHyphen:
			builder.WriteByte('-')
			previousHyphen = true
		}
	}
	return strings.Trim(builder.String(), "-")
}

// SourceState é a personalização do usuário: as origens que ele adicionou e
// as embutidas que resolveu desligar.
//
// Guardar as embutidas por nome, em vez de copiá-las para o arquivo, permite
// que a engine mude o endereço do índice padrão numa atualização sem deixar
// para trás uma cópia congelada no disco de quem já tinha rodado a versão
// anterior.
type SourceState struct {
	Custom           []Origin `json:"custom"`
	DisabledBuiltins []string `json:"disabledBuiltins,omitempty"`
}

// SourceStore persiste a personalização. Implementações usam disco, memória
// ou configuração remota sem que o caso de uso saiba.
type SourceStore interface {
	Load(ctx context.Context) (SourceState, error)
	Save(ctx context.Context, state SourceState) error
}

// SourceStatus é o resultado da última consulta a uma origem. Err preenchido
// não interrompe a listagem: o marketplace degrada por origem.
type SourceStatus struct {
	Origin
	// Tools conta as entradas válidas do índice desta origem, não as tools
	// visíveis: parte delas pode não ter artefato para esta plataforma nem
	// protocolo compatível com esta engine.
	Tools int
	Err   error
}

// Catalog é a resposta completa do marketplace: as tools resolvidas e o
// estado de cada origem consultada.
type Catalog struct {
	Tools   []Listing
	Sources []SourceStatus
}

// Degraded informa se alguma origem habilitada falhou.
func (c Catalog) Degraded() bool {
	for _, status := range c.Sources {
		if status.Err != nil {
			return true
		}
	}
	return false
}

var (
	ErrSourceNotFound = errors.New("origem não encontrada")
	ErrSourceBuiltin  = errors.New("origem embutida não pode ser removida")
	ErrSourceExists   = errors.New("já existe uma origem com esse nome")
)

// Sources devolve as origens conhecidas na ordem de prioridade: as embutidas
// primeiro, depois as do usuário na ordem em que foram adicionadas.
func (s *Service) Sources(ctx context.Context) ([]Origin, error) {
	state, err := s.loadState(ctx)
	if err != nil {
		return nil, err
	}
	return s.merge(state), nil
}

// AddSource registra um repositório paralelo de tools.
func (s *Service) AddSource(ctx context.Context, origin Origin) error {
	origin.Name = strings.TrimSpace(origin.Name)
	origin.Label = strings.TrimSpace(origin.Label)
	origin.Ref = strings.TrimSpace(origin.Ref)
	// Confiança e origem embutida são decisões do composition root, nunca de
	// quem preenche o formulário: aceitar estes campos vindos de fora seria
	// entregar o selo "official" a qualquer índice de terceiro.
	origin.Trusted, origin.Builtin, origin.Enabled = false, false, true
	if err := origin.Validate(); err != nil {
		return err
	}
	state, err := s.loadState(ctx)
	if err != nil {
		return err
	}
	for _, existing := range s.merge(state) {
		if existing.Name == origin.Name {
			return fmt.Errorf("%s: %w", origin.Name, ErrSourceExists)
		}
	}
	state.Custom = append(state.Custom, origin)
	return s.saveState(ctx, state)
}

// RemoveSource esquece uma origem do usuário. As embutidas só podem ser
// desligadas, para que desativar o índice padrão por engano não deixe a
// engine sem nenhuma procedência conhecida.
func (s *Service) RemoveSource(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	state, err := s.loadState(ctx)
	if err != nil {
		return err
	}
	for _, builtin := range s.config.BuiltinSources {
		if builtin.Name == name {
			return fmt.Errorf("%s: %w", name, ErrSourceBuiltin)
		}
	}
	filtered := make([]Origin, 0, len(state.Custom))
	for _, origin := range state.Custom {
		if origin.Name != name {
			filtered = append(filtered, origin)
		}
	}
	if len(filtered) == len(state.Custom) {
		return fmt.Errorf("%s: %w", name, ErrSourceNotFound)
	}
	state.Custom = filtered
	return s.saveState(ctx, state)
}

// SetSourceEnabled liga ou desliga uma origem sem descartar seu cadastro.
func (s *Service) SetSourceEnabled(ctx context.Context, name string, enabled bool) error {
	name = strings.TrimSpace(name)
	state, err := s.loadState(ctx)
	if err != nil {
		return err
	}
	for index, origin := range state.Custom {
		if origin.Name == name {
			state.Custom[index].Enabled = enabled
			return s.saveState(ctx, state)
		}
	}
	for _, builtin := range s.config.BuiltinSources {
		if builtin.Name != name {
			continue
		}
		disabled := make([]string, 0, len(state.DisabledBuiltins)+1)
		for _, entry := range state.DisabledBuiltins {
			if entry != name {
				disabled = append(disabled, entry)
			}
		}
		if !enabled {
			disabled = append(disabled, name)
		}
		sort.Strings(disabled)
		state.DisabledBuiltins = disabled
		return s.saveState(ctx, state)
	}
	return fmt.Errorf("%s: %w", name, ErrSourceNotFound)
}

func (s *Service) loadState(ctx context.Context) (SourceState, error) {
	if s.config.Sources == nil {
		return SourceState{}, nil
	}
	return s.config.Sources.Load(ctx)
}

func (s *Service) saveState(ctx context.Context, state SourceState) error {
	if s.config.Sources == nil {
		return errors.New("esta instalação não persiste origens do marketplace")
	}
	return s.config.Sources.Save(ctx, state)
}

// merge combina as origens embutidas com as do usuário e atribui a
// prioridade pela posição final da lista.
func (s *Service) merge(state SourceState) []Origin {
	disabled := make(map[string]bool, len(state.DisabledBuiltins))
	for _, name := range state.DisabledBuiltins {
		disabled[name] = true
	}
	origins := make([]Origin, 0, len(s.config.BuiltinSources)+len(state.Custom))
	for _, origin := range s.config.BuiltinSources {
		origin.Builtin = true
		origin.Enabled = !disabled[origin.Name]
		origins = append(origins, origin)
	}
	seen := make(map[string]bool, len(origins))
	for _, origin := range origins {
		seen[origin.Name] = true
	}
	for _, origin := range state.Custom {
		// Um arquivo editado à mão pode repetir o nome de uma embutida; a
		// embutida vence, senão bastaria escrever "official" no JSON para
		// herdar a confiança dela.
		if seen[origin.Name] || origin.Validate() != nil {
			continue
		}
		seen[origin.Name] = true
		origin.Trusted, origin.Builtin = false, false
		origins = append(origins, origin)
	}
	for index := range origins {
		origins[index].Priority = index
	}
	return origins
}
