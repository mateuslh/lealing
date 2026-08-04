// Package marketplace contém o catálogo público e o caso de uso de instalação
// remota. O core não conhece GitHub, HTTP, arquivos compactados nem a TUI:
// origens e pacotes entram por portas explícitas.
package marketplace

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/toolinstall"
)

const APIVersion = "lealing.dev/marketplace/v1"

var (
	ErrNotAvailable  = errors.New("tool não disponível para esta engine e plataforma")
	ErrAlreadyLatest = errors.New("tool já está na versão mais recente")
	validID          = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	validVersion     = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?$`)
)

type Channel string

const (
	ChannelOfficial  Channel = "official"
	ChannelVerified  Channel = "verified"
	ChannelCommunity Channel = "community"
)

func (c Channel) Valid() bool {
	return c == ChannelOfficial || c == ChannelVerified || c == ChannelCommunity
}

type VersionRange struct {
	Min int `json:"min" yaml:"min"`
	Max int `json:"max" yaml:"max"`
}

func (r VersionRange) Valid() bool { return r.Min > 0 && r.Max >= r.Min }

type Artifact struct {
	Platform string `json:"platform" yaml:"platform"`
	URL      string `json:"url" yaml:"url"`
	SHA256   string `json:"sha256" yaml:"sha256"`
}

type FilesystemPermissions struct {
	Read  []string `json:"read" yaml:"read"`
	Write []string `json:"write" yaml:"write"`
}

type Permissions struct {
	Filesystem FilesystemPermissions `json:"filesystem" yaml:"filesystem"`
	Network    bool                  `json:"network" yaml:"network"`
	Subprocess bool                  `json:"subprocess" yaml:"subprocess"`
	// WorkingDir espelha o campo do manifest para que a ficha da tool
	// mostre o pedido antes do download.
	WorkingDir string `json:"workingDir,omitempty" yaml:"workingDir,omitempty"`
}

// Entry carrega tudo que a tela precisa para listar uma tool sem baixar nem
// executar seu artefato. O manifest empacotado continua sendo revalidado no
// momento da instalação.
type Entry struct {
	ID               string       `json:"id" yaml:"id"`
	Version          string       `json:"version" yaml:"version"`
	Name             string       `json:"name" yaml:"name"`
	Summary          string       `json:"summary" yaml:"summary"`
	Detail           string       `json:"detail,omitempty" yaml:"detail,omitempty"`
	Category         string       `json:"category" yaml:"category"`
	Risk             string       `json:"risk" yaml:"risk"`
	Glyph            string       `json:"glyph,omitempty" yaml:"glyph,omitempty"`
	Permissions      Permissions  `json:"permissions" yaml:"permissions"`
	Publisher        string       `json:"publisher" yaml:"publisher"`
	ManifestURL      string       `json:"manifestUrl" yaml:"manifestUrl"`
	Artifacts        []Artifact   `json:"artifacts" yaml:"artifacts"`
	Protocol         VersionRange `json:"protocol" yaml:"protocol"`
	MinimumEngine    string       `json:"minimumEngine" yaml:"minimumEngine"`
	DistributionTier Channel      `json:"channel" yaml:"channel"`
	// Origin é preenchida pela engine ao ler o índice, nunca pelo publicador:
	// um índice que pudesse declarar sua própria procedência escolheria seu
	// selo de confiança.
	Origin Origin `json:"-" yaml:"-"`
}

// Ref é a referência qualificada "origem/id", que desempata duas origens que
// publicam o mesmo ID.
func (e Entry) Ref() string {
	if e.Origin.Name == "" {
		return e.ID
	}
	return e.Origin.Name + "/" + e.ID
}

type Index struct {
	APIVersion string  `json:"apiVersion" yaml:"apiVersion"`
	Tools      []Entry `json:"tools" yaml:"tools"`
}

// ValidationOptions contém a política editorial da engine. Categorias e
// plataformas são injetadas pelo composition root para o core não detectar o
// sistema nem importar o parser de manifests.
type ValidationOptions struct {
	Categories map[string]bool
	Platforms  map[string]bool
	// Local relaxa as exigências que só fazem sentido para um índice
	// publicado: um repositório em disco aponta para diretórios de build que
	// mudam a cada compilação e não tem URL de manifest para exibir.
	Local bool
}

func (i Index) Validate(opts ValidationOptions) error {
	if i.APIVersion != APIVersion {
		return fmt.Errorf("apiVersion do marketplace inválida: %q", i.APIVersion)
	}
	seen := make(map[string]bool, len(i.Tools))
	for position, entry := range i.Tools {
		if err := entry.Validate(opts); err != nil {
			return fmt.Errorf("tools[%d]: %w", position, err)
		}
		key := entry.ID + "@" + entry.Version
		if seen[key] {
			return fmt.Errorf("entrada duplicada no marketplace: %s", key)
		}
		seen[key] = true
	}
	return nil
}

func (e Entry) Validate(opts ValidationOptions) error {
	switch {
	case !validID.MatchString(e.ID):
		return fmt.Errorf("ID inválido: %q", e.ID)
	case !validSemver(e.Version):
		return fmt.Errorf("versão inválida em %s: %q", e.ID, e.Version)
	case strings.TrimSpace(e.Name) == "":
		return fmt.Errorf("nome vazio em %s", e.ID)
	case strings.ContainsAny(e.Summary, "\r\n") || !strings.HasSuffix(strings.TrimSpace(e.Summary), "."):
		return fmt.Errorf("summary de %s deve ter uma linha e terminar com ponto", e.ID)
	case len(opts.Categories) > 0 && !opts.Categories[e.Category]:
		return fmt.Errorf("categoria desconhecida em %s: %q", e.ID, e.Category)
	case e.Risk != domain.RiskSafe.String() && e.Risk != domain.RiskCaution.String() && e.Risk != domain.RiskDestructive.String():
		return fmt.Errorf("risk desconhecido em %s: %q", e.ID, e.Risk)
	case strings.TrimSpace(e.Publisher) == "":
		return fmt.Errorf("publisher vazio em %s", e.ID)
	case !e.DistributionTier.Valid():
		return fmt.Errorf("canal desconhecido em %s: %q", e.ID, e.DistributionTier)
	case !e.Protocol.Valid():
		return fmt.Errorf("protocolo inválido em %s", e.ID)
	case e.MinimumEngine != "" && !validSemver(e.MinimumEngine):
		return fmt.Errorf("minimumEngine inválida em %s: %q", e.ID, e.MinimumEngine)
	case !opts.Local && !safeHTTPS(e.ManifestURL):
		return fmt.Errorf("manifestUrl de %s precisa usar HTTPS", e.ID)
	case len(e.Artifacts) == 0:
		return fmt.Errorf("nenhum artefato em %s", e.ID)
	}

	seenPlatforms := make(map[string]bool, len(e.Artifacts))
	for _, artifact := range e.Artifacts {
		switch {
		case len(opts.Platforms) > 0 && !opts.Platforms[artifact.Platform]:
			return fmt.Errorf("plataforma desconhecida em %s: %q", e.ID, artifact.Platform)
		case seenPlatforms[artifact.Platform]:
			return fmt.Errorf("plataforma duplicada em %s: %s", e.ID, artifact.Platform)
		case opts.Local && !validLocalPath(artifact.URL):
			return fmt.Errorf("caminho do artefato %s/%s precisa ser relativo ao índice e não pode subir de diretório", e.ID, artifact.Platform)
		case !opts.Local && !safeHTTPS(artifact.URL):
			return fmt.Errorf("URL do artefato %s/%s precisa usar HTTPS", e.ID, artifact.Platform)
		// Numa origem local o artefato é o diretório de build do próprio
		// usuário, que muda a cada compilação. Aceitar um checksum ali seria
		// exibir uma garantia que ninguém confere.
		case opts.Local && artifact.SHA256 != "":
			return fmt.Errorf("origem local não verifica checksum; remova sha256 de %s/%s", e.ID, artifact.Platform)
		case !opts.Local && !validSHA256(artifact.SHA256):
			return fmt.Errorf("SHA-256 inválido em %s/%s", e.ID, artifact.Platform)
		}
		seenPlatforms[artifact.Platform] = true
	}
	if err := validatePermissions(e.ID, e.Permissions); err != nil {
		return err
	}
	return nil
}

// SelectLatest escolhe a versão mais nova compatível sem fixar a engine a
// uma versão específica da tool. Builds "dev" são tratados como posteriores
// aos releases: eles carregam o código atual, embora ainda não tenham SemVer.
func (i Index) SelectLatest(id, platform, engineVersion string, protocol VersionRange) (Entry, Artifact, error) {
	type candidate struct {
		entry    Entry
		artifact Artifact
		version  semanticVersion
	}
	var candidates []candidate
	for _, entry := range i.Tools {
		if entry.ID != id || entry.Protocol.Max < protocol.Min || protocol.Max < entry.Protocol.Min {
			continue
		}
		version, ok := parseVersion(entry.Version)
		minimum, minOK := parseVersion(entry.MinimumEngine)
		engine, engineOK := parseVersion(engineVersion)
		development := engineVersion == "dev" || strings.HasSuffix(engineVersion, "-dev")
		if !ok || (entry.MinimumEngine != "" && (!minOK || (!development && (!engineOK || less(engine, minimum))))) {
			continue
		}
		for _, artifact := range entry.Artifacts {
			usable := validSHA256(artifact.SHA256) || (entry.Origin.Kind == OriginLocal && artifact.SHA256 == "")
			if artifact.Platform == platform && artifact.URL != "" && usable {
				candidates = append(candidates, candidate{entry: entry, artifact: artifact, version: version})
			}
		}
	}
	if len(candidates) == 0 {
		return Entry{}, Artifact{}, ErrNotAvailable
	}
	// A prioridade da origem vem antes da versão de propósito. Se um índice
	// paralelo publicasse 9.9.9 com o ID de uma tool oficial, ordenar só por
	// versão faria a engine instalar o impostor.
	sort.SliceStable(candidates, func(a, b int) bool {
		if candidates[a].entry.Origin.Priority != candidates[b].entry.Origin.Priority {
			return candidates[a].entry.Origin.Priority < candidates[b].entry.Origin.Priority
		}
		return less(candidates[b].version, candidates[a].version)
	})
	return candidates[0].entry, candidates[0].artifact, nil
}

// SelectVersion resolve uma versão exata. Sincronização declarativa não pode
// trocar silenciosamente X.Y.Z pela release mais recente: o snapshot remoto
// é a fonte da verdade também para downgrade e reprodução histórica.
func (i Index) SelectVersion(id, version, platform, engineVersion string, protocol VersionRange) (Entry, Artifact, error) {
	for _, entry := range i.Tools {
		if entry.ID != id || entry.Version != version || entry.Protocol.Max < protocol.Min || protocol.Max < entry.Protocol.Min {
			continue
		}
		minimum, minOK := parseVersion(entry.MinimumEngine)
		engine, engineOK := parseVersion(engineVersion)
		development := engineVersion == "dev" || strings.HasSuffix(engineVersion, "-dev")
		if entry.MinimumEngine != "" && (!minOK || (!development && (!engineOK || less(engine, minimum)))) {
			continue
		}
		for _, artifact := range entry.Artifacts {
			usable := validSHA256(artifact.SHA256) || (entry.Origin.Kind == OriginLocal && artifact.SHA256 == "")
			if artifact.Platform == platform && artifact.URL != "" && usable {
				return entry, artifact, nil
			}
		}
	}
	return Entry{}, Artifact{}, ErrNotAvailable
}

// IndexSource lê o índice de uma origem. Implementações podem usar HTTP,
// arquivo local ou um cache assinado sem alterar o caso de uso.
type IndexSource interface {
	Fetch(ctx context.Context, origin Origin) (Index, error)
}

// PreparedPackage é um diretório temporário pronto para o instalador local.
// Cleanup precisa ser idempotente e nunca é omitido pelo serviço.
type PreparedPackage struct {
	Directory string
	Cleanup   func() error
}

// PackageSource baixa, verifica o checksum do arquivo e extrai seu conteúdo
// de forma confinada. Ele não instala nem executa a tool.
type PackageSource interface {
	Prepare(ctx context.Context, origin Origin, artifact Artifact) (PreparedPackage, error)
}

// CatalogReloader torna uma instalação visível na mesma execução da engine.
type CatalogReloader interface {
	Reload(ctx context.Context) error
}

type Listing struct {
	Entry
	InstalledVersion string
	UpdateAvailable  bool
	// Shadowed lista as outras origens que publicam este mesmo ID e perderam
	// a disputa de prioridade. A tela mostra isso porque um ID repetido é
	// tanto um acidente comum quanto a assinatura de uma tentativa de
	// sequestro de nome.
	Shadowed []string
}

// Manager é a porta de entrada usada tanto pela CLI quanto pela tela.
type Manager interface {
	// Catalog devolve as tools e o estado de cada origem consultada.
	Catalog(ctx context.Context) (Catalog, error)
	List(ctx context.Context) ([]Listing, error)
	// Install aceita o ID simples ou a referência qualificada "origem/id".
	// Devolve *toolinstall.PermissionEscalationError (verificável com
	// errors.As) sem instalar nada quando a versão nova pede permissão que
	// a versão ativa não tinha e opts.PermissionsAccepted é falso.
	Install(ctx context.Context, ref string, opts InstallOptions) (toolinstall.Installation, error)
	Sources(ctx context.Context) ([]Origin, error)
	AddSource(ctx context.Context, origin Origin) error
	RemoveSource(ctx context.Context, name string) (SourceRemoval, error)
	SetSourceEnabled(ctx context.Context, name string, enabled bool) error
}

// InstallOptions carrega decisões tomadas pelo driving adapter antes de
// instalar ou atualizar uma tool. Mesmo desenho de inbound.LaunchOptions.
type InstallOptions struct {
	// PermissionsAccepted sinaliza que o usuário já viu o diálogo de
	// ampliação de permissão desta atualização e confirmou.
	PermissionsAccepted bool
}

type Config struct {
	Platform      string
	EngineVersion string
	Protocol      VersionRange
	Validation    ValidationOptions
	// BuiltinSources são as origens que acompanham a engine, em ordem de
	// prioridade decrescente de confiança.
	BuiltinSources []Origin
	Sources        SourceStore
	// Index e Packages recebem a origem em cada chamada; o composition root
	// pode entregar um roteador que escolhe HTTP ou disco pelo tipo dela.
	Index           IndexSource
	Packages        PackageSource
	Installer       toolinstall.Manager
	CatalogReloader CatalogReloader
}

type Service struct {
	config   Config
	mutation sync.Mutex
}

var _ Manager = (*Service)(nil)

func NewService(config Config) *Service { return &Service{config: config} }

// Catalog consulta todas as origens habilitadas e resolve as tools visíveis
// nesta plataforma. Uma origem fora do ar não impede as demais de aparecer:
// o erro dela viaja em SourceStatus para a tela mostrar sem esconder o resto.
func (s *Service) Catalog(ctx context.Context) (Catalog, error) {
	index, statuses, err := s.fetch(ctx)
	if err != nil {
		return Catalog{Sources: statuses}, err
	}
	installed, err := s.config.Installer.ListInstalled(ctx)
	if err != nil {
		return Catalog{Sources: statuses}, err
	}
	active := make(map[string]string, len(installed))
	for _, item := range installed {
		active[item.ID] = item.ActiveVersion
	}

	ids := make([]string, 0, len(index.Tools))
	publishers := make(map[string][]string, len(index.Tools))
	for _, entry := range index.Tools {
		if _, seen := publishers[entry.ID]; !seen {
			ids = append(ids, entry.ID)
		}
		publishers[entry.ID] = appendUnique(publishers[entry.ID], entry.Origin.Name)
	}

	listings := make([]Listing, 0, len(ids))
	for _, id := range ids {
		entry, _, selectErr := index.SelectLatest(id, s.config.Platform, s.config.EngineVersion, s.config.Protocol)
		if selectErr != nil {
			continue
		}
		current := active[id]
		shadowed := make([]string, 0, len(publishers[id]))
		for _, name := range publishers[id] {
			if name != entry.Origin.Name {
				shadowed = append(shadowed, name)
			}
		}
		listings = append(listings, Listing{
			Entry: entry, InstalledVersion: current,
			UpdateAvailable: current != "" && versionLess(current, entry.Version),
			Shadowed:        shadowed,
		})
	}
	sort.Slice(listings, func(i, j int) bool {
		if left, right := strings.ToLower(listings[i].Name), strings.ToLower(listings[j].Name); left != right {
			return left < right
		}
		return listings[i].Origin.Priority < listings[j].Origin.Priority
	})
	return Catalog{Tools: listings, Sources: statuses}, nil
}

func (s *Service) List(ctx context.Context) ([]Listing, error) {
	catalog, err := s.Catalog(ctx)
	if err != nil {
		return nil, err
	}
	return catalog.Tools, nil
}

// fetch consulta as origens habilitadas em paralelo. Sequencial, uma origem
// lenta atrasaria todas as outras; e como o marketplace agora é a soma de
// vários repositórios, essa espera cresceria a cada um que o usuário
// adicionasse.
func (s *Service) fetch(ctx context.Context) (Index, []SourceStatus, error) {
	if s.config.Index == nil || s.config.Packages == nil || s.config.Installer == nil {
		return Index{}, nil, errors.New("marketplace não configurado")
	}
	origins, err := s.Sources(ctx)
	if err != nil {
		return Index{}, nil, err
	}
	enabled := make([]Origin, 0, len(origins))
	for _, origin := range origins {
		if origin.Enabled {
			enabled = append(enabled, origin)
		}
	}
	if len(enabled) == 0 {
		return Index{}, nil, errors.New("nenhuma origem de tools habilitada")
	}

	statuses := make([]SourceStatus, len(enabled))
	collected := make([][]Entry, len(enabled))
	var group sync.WaitGroup
	for position, origin := range enabled {
		group.Add(1)
		go func() {
			defer group.Done()
			entries, fetchErr := s.fetchOrigin(ctx, origin)
			collected[position] = entries
			statuses[position] = SourceStatus{Origin: origin, Tools: len(entries), Err: fetchErr}
		}()
	}
	group.Wait()

	aggregate := Index{APIVersion: APIVersion}
	failures := 0
	for position := range enabled {
		if statuses[position].Err != nil {
			failures++
			continue
		}
		aggregate.Tools = append(aggregate.Tools, collected[position]...)
	}
	if failures == len(enabled) {
		return Index{}, statuses, fmt.Errorf("nenhuma origem respondeu; %s: %w",
			statuses[0].Origin.Name, statuses[0].Err)
	}
	return aggregate, statuses, nil
}

// fetchOrigin lê e valida um índice isoladamente, para que um repositório
// malformado seja rejeitado inteiro sem contaminar os demais.
func (s *Service) fetchOrigin(ctx context.Context, origin Origin) ([]Entry, error) {
	index, err := s.config.Index.Fetch(ctx, origin)
	if err != nil {
		return nil, err
	}
	options := s.config.Validation
	options.Local = origin.Kind == OriginLocal
	if err := index.Validate(options); err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(index.Tools))
	for _, entry := range index.Tools {
		// O canal é uma afirmação sobre a revisão editorial da engine, não
		// sobre o índice. Um repositório paralelo que se declarasse official
		// compraria o selo apenas editando o próprio JSON.
		if !origin.Trusted {
			entry.DistributionTier = ChannelCommunity
		}
		entry.Origin = origin
		entries = append(entries, entry)
	}
	return entries, nil
}

func (s *Service) Install(ctx context.Context, ref string, opts InstallOptions) (toolinstall.Installation, error) {
	originName, id := splitRef(ref)
	if id == "" {
		return toolinstall.Installation{}, errors.New("ID da tool é obrigatório")
	}
	index, _, err := s.fetch(ctx)
	if err != nil {
		return toolinstall.Installation{}, err
	}
	if originName != "" {
		index = index.fromOrigin(originName)
		if len(index.Tools) == 0 {
			return toolinstall.Installation{}, fmt.Errorf("%s: %w", originName, ErrSourceNotFound)
		}
	}
	entry, artifact, err := index.SelectLatest(id, s.config.Platform, s.config.EngineVersion, s.config.Protocol)
	if err != nil {
		return toolinstall.Installation{}, fmt.Errorf("%s: %w", id, err)
	}
	installed, err := s.config.Installer.ListInstalled(ctx)
	if err != nil {
		return toolinstall.Installation{}, err
	}
	for _, item := range installed {
		if item.ID == id && item.ActiveVersion == entry.Version {
			return toolinstall.Installation{}, ErrAlreadyLatest
		}
	}

	prepared, err := s.config.Packages.Prepare(ctx, entry.Origin, artifact)
	if err != nil {
		return toolinstall.Installation{}, err
	}
	if prepared.Cleanup != nil {
		defer prepared.Cleanup()
	}
	installation, err := s.config.Installer.InstallLocal(ctx, installRequest(entry, prepared.Directory, opts.PermissionsAccepted))
	if err != nil {
		return toolinstall.Installation{}, err
	}
	if s.config.CatalogReloader != nil {
		if err := s.config.CatalogReloader.Reload(ctx); err != nil {
			return installation, fmt.Errorf("tool instalada, mas o catálogo não recarregou: %w", err)
		}
	}
	return installation, nil
}

// fromOrigin recorta o índice agregado para uma única origem, usado quando a
// instalação é pedida pela referência qualificada.
func (i Index) fromOrigin(name string) Index {
	filtered := Index{APIVersion: i.APIVersion}
	for _, entry := range i.Tools {
		if entry.Origin.Name == name {
			filtered.Tools = append(filtered.Tools, entry)
		}
	}
	return filtered
}

// splitRef separa "origem/id" de um ID simples.
func splitRef(ref string) (string, string) {
	ref = strings.TrimSpace(ref)
	origin, id, qualified := strings.Cut(ref, "/")
	if !qualified {
		return "", ref
	}
	return strings.TrimSpace(origin), strings.TrimSpace(id)
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

// validLocalPath aceita o caminho de um artefato em disco relativo ao índice.
// Absolutos e travessias são recusados para que um índice local baixado de
// terceiros não consiga apontar para fora do próprio repositório.
func validLocalPath(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || strings.ContainsAny(value, "\x00\r\n") {
		return false
	}
	normalized := strings.ReplaceAll(value, `\`, "/")
	if strings.HasPrefix(normalized, "/") || strings.Contains(normalized, ":") {
		return false
	}
	for _, segment := range strings.Split(normalized, "/") {
		if segment == ".." {
			return false
		}
	}
	return true
}

func validSemver(value string) bool {
	_, ok := parseVersion(value)
	return ok
}

type semanticVersion struct {
	core       [3]int
	prerelease []string
}

func parseVersion(value string) (semanticVersion, bool) {
	value = strings.TrimPrefix(value, "v")
	if !validVersion.MatchString(value) {
		return semanticVersion{}, false
	}
	core, prerelease, hasPrerelease := strings.Cut(value, "-")
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return semanticVersion{}, false
	}
	var result semanticVersion
	for index, part := range parts {
		n, err := strconv.Atoi(part)
		if err != nil || n < 0 || (len(part) > 1 && part[0] == '0') {
			return semanticVersion{}, false
		}
		result.core[index] = n
	}
	if hasPrerelease {
		result.prerelease = strings.Split(prerelease, ".")
		for _, identifier := range result.prerelease {
			if _, err := strconv.Atoi(identifier); err == nil && len(identifier) > 1 && identifier[0] == '0' {
				return semanticVersion{}, false
			}
		}
	}
	return result, true
}

func less(a, b semanticVersion) bool {
	for index := range a.core {
		if a.core[index] != b.core[index] {
			return a.core[index] < b.core[index]
		}
	}
	if len(a.prerelease) == 0 || len(b.prerelease) == 0 {
		return len(a.prerelease) > 0 && len(b.prerelease) == 0
	}
	for index := 0; index < min(len(a.prerelease), len(b.prerelease)); index++ {
		left, right := a.prerelease[index], b.prerelease[index]
		if left == right {
			continue
		}
		leftNumber, leftErr := strconv.Atoi(left)
		rightNumber, rightErr := strconv.Atoi(right)
		switch {
		case leftErr == nil && rightErr == nil:
			return leftNumber < rightNumber
		case leftErr == nil:
			return true
		case rightErr == nil:
			return false
		default:
			return left < right
		}
	}
	return len(a.prerelease) < len(b.prerelease)
}

func versionLess(a, b string) bool {
	left, leftOK := parseVersion(a)
	right, rightOK := parseVersion(b)
	return leftOK && rightOK && less(left, right)
}

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
}

func safeHTTPS(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme == "https" && parsed.Host != "" && parsed.User == nil
}

func validatePermissions(id string, permissions Permissions) error {
	seen := map[string]bool{}
	for kind, paths := range map[string][]string{
		"filesystem.read":  permissions.Filesystem.Read,
		"filesystem.write": permissions.Filesystem.Write,
	} {
		for _, value := range paths {
			value = strings.TrimSpace(value)
			if value == "" || strings.ContainsAny(value, "\r\n\x00") {
				return fmt.Errorf("permissão %s inválida em %s", kind, id)
			}
			key := kind + "\x00" + value
			if seen[key] {
				return fmt.Errorf("permissão %s duplicada em %s: %s", kind, id, value)
			}
			seen[key] = true
		}
	}
	// Os níveis são repetidos aqui, e não importados do SDK, porque o core
	// não depende do pacote do fio — a mesma razão pela qual as capabilities
	// aparecem duas vezes no repositório.
	switch permissions.WorkingDir {
	case "", "read", "write":
	default:
		return fmt.Errorf("permissão workingDir inválida em %s: %s", id, permissions.WorkingDir)
	}
	return nil
}
