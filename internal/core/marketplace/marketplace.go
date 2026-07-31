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
	case !safeHTTPS(e.ManifestURL):
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
		case !safeHTTPS(artifact.URL):
			return fmt.Errorf("URL do artefato %s/%s precisa usar HTTPS", e.ID, artifact.Platform)
		case !validSHA256(artifact.SHA256):
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
			if artifact.Platform == platform && artifact.URL != "" && validSHA256(artifact.SHA256) {
				candidates = append(candidates, candidate{entry: entry, artifact: artifact, version: version})
			}
		}
	}
	if len(candidates) == 0 {
		return Entry{}, Artifact{}, ErrNotAvailable
	}
	sort.Slice(candidates, func(a, b int) bool { return less(candidates[b].version, candidates[a].version) })
	return candidates[0].entry, candidates[0].artifact, nil
}

// IndexSource lê o índice. Implementações podem usar HTTP, arquivo local ou
// um cache assinado sem alterar o caso de uso.
type IndexSource interface {
	Fetch(ctx context.Context) (Index, error)
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
	Prepare(ctx context.Context, artifact Artifact) (PreparedPackage, error)
}

// CatalogReloader torna uma instalação visível na mesma execução da engine.
type CatalogReloader interface {
	Reload(ctx context.Context) error
}

type Listing struct {
	Entry
	InstalledVersion string
	UpdateAvailable  bool
}

// Manager é a porta de entrada usada tanto pela CLI quanto pela tela.
type Manager interface {
	List(ctx context.Context) ([]Listing, error)
	Install(ctx context.Context, id string) (toolinstall.Installation, error)
}

type Config struct {
	Platform        string
	EngineVersion   string
	Protocol        VersionRange
	Validation      ValidationOptions
	Index           IndexSource
	Packages        PackageSource
	Installer       toolinstall.Manager
	CatalogReloader CatalogReloader
}

type Service struct{ config Config }

var _ Manager = (*Service)(nil)

func NewService(config Config) *Service { return &Service{config: config} }

func (s *Service) List(ctx context.Context) ([]Listing, error) {
	index, err := s.loadIndex(ctx)
	if err != nil {
		return nil, err
	}
	installed, err := s.config.Installer.ListInstalled(ctx)
	if err != nil {
		return nil, err
	}
	active := make(map[string]string, len(installed))
	for _, item := range installed {
		active[item.ID] = item.ActiveVersion
	}

	ids := make(map[string]bool, len(index.Tools))
	for _, entry := range index.Tools {
		ids[entry.ID] = true
	}
	listings := make([]Listing, 0, len(ids))
	for id := range ids {
		entry, _, selectErr := index.SelectLatest(id, s.config.Platform, s.config.EngineVersion, s.config.Protocol)
		if selectErr != nil {
			continue
		}
		current := active[id]
		listings = append(listings, Listing{
			Entry: entry, InstalledVersion: current,
			UpdateAvailable: current != "" && versionLess(current, entry.Version),
		})
	}
	sort.Slice(listings, func(i, j int) bool {
		return strings.ToLower(listings[i].Name) < strings.ToLower(listings[j].Name)
	})
	return listings, nil
}

func (s *Service) Install(ctx context.Context, id string) (toolinstall.Installation, error) {
	if strings.TrimSpace(id) == "" {
		return toolinstall.Installation{}, errors.New("ID da tool é obrigatório")
	}
	index, err := s.loadIndex(ctx)
	if err != nil {
		return toolinstall.Installation{}, err
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

	prepared, err := s.config.Packages.Prepare(ctx, artifact)
	if err != nil {
		return toolinstall.Installation{}, err
	}
	if prepared.Cleanup != nil {
		defer prepared.Cleanup()
	}
	installation, err := s.config.Installer.InstallLocal(ctx, toolinstall.InstallRequest{
		SourceDir: prepared.Directory, ExpectedID: entry.ID, ExpectedVersion: entry.Version,
		ExpectedManifest: &toolinstall.ManifestExpectation{
			ID: entry.ID, Version: entry.Version, Name: entry.Name, Summary: entry.Summary,
			Detail: entry.Detail, Category: entry.Category, Risk: entry.Risk, Glyph: entry.Glyph,
			ProtocolMin: entry.Protocol.Min, ProtocolMax: entry.Protocol.Max,
			FilesystemRead:  append([]string(nil), entry.Permissions.Filesystem.Read...),
			FilesystemWrite: append([]string(nil), entry.Permissions.Filesystem.Write...),
			Network:         entry.Permissions.Network, Subprocess: entry.Permissions.Subprocess,
		},
	})
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

func (s *Service) loadIndex(ctx context.Context) (Index, error) {
	if s.config.Index == nil || s.config.Packages == nil || s.config.Installer == nil {
		return Index{}, errors.New("marketplace não configurado")
	}
	index, err := s.config.Index.Fetch(ctx)
	if err != nil {
		return Index{}, err
	}
	if err := index.Validate(s.config.Validation); err != nil {
		return Index{}, err
	}
	return index, nil
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
	return nil
}
