// Package toolmanifest implementa o formato declarativo lealing.dev/v1.
// Ler e validar um manifest nunca verifica nem inicia seu executável.
package toolmanifest

import (
	"bytes"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"

	"github.com/mateuslh/lealing-sdk/protocol"
	"github.com/mateuslh/lealing/internal/core/domain"
)

const (
	APIVersion = "lealing.dev/v1"
	MaxSize    = 1 << 20
)

var (
	idPattern      = regexp.MustCompile(`^[a-z0-9]+(?:[-/][a-z0-9]+)*$`)
	versionPattern = regexp.MustCompile(`^v?(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)` +
		`(?:-(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*)(?:\.(?:0|[1-9]\d*|\d*[A-Za-z-][0-9A-Za-z-]*))*)?` +
		`(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?$`)
)

type Manifest struct {
	APIVersion   string               `json:"apiVersion" yaml:"apiVersion"`
	ID           string               `json:"id" yaml:"id"`
	Version      string               `json:"version" yaml:"version"`
	Name         string               `json:"name" yaml:"name"`
	Summary      string               `json:"summary" yaml:"summary"`
	Detail       string               `json:"detail" yaml:"detail"`
	Category     string               `json:"category" yaml:"category"`
	Risk         string               `json:"risk" yaml:"risk"`
	Glyph        string               `json:"glyph,omitempty" yaml:"glyph,omitempty"`
	Keywords     []string             `json:"keywords,omitempty" yaml:"keywords,omitempty"`
	Tags         []string             `json:"tags,omitempty" yaml:"tags,omitempty"`
	Runtime      Runtime              `json:"runtime" yaml:"runtime"`
	UI           UI                   `json:"ui" yaml:"ui"`
	Platforms    []string             `json:"platforms" yaml:"platforms"`
	Requirements []Requirement        `json:"requirements" yaml:"requirements"`
	Permissions  protocol.Permissions `json:"permissions" yaml:"permissions"`
}

type Runtime struct {
	Kind       string                `json:"kind" yaml:"kind"`
	Protocol   protocol.VersionRange `json:"protocol" yaml:"protocol"`
	Executable string                `json:"executable" yaml:"executable"`
}

type UI struct {
	Mode         string   `json:"mode" yaml:"mode"`
	Capabilities []string `json:"capabilities,omitempty" yaml:"capabilities,omitempty"`
	// WantsMouse pede à engine para capturar o mouse do terminal e
	// encaminhar cliques, arraste e roda à tool. Omitido (ou false), a
	// engine deixa o terminal livre e o usuário pode selecionar texto na
	// tela normalmente enquanto a tool roda.
	WantsMouse bool `json:"wantsMouse,omitempty" yaml:"wantsMouse,omitempty"`
}

type Requirement struct {
	Executable  string `json:"executable" yaml:"executable"`
	Name        string `json:"name,omitempty" yaml:"name,omitempty"`
	InstallHint string `json:"installHint,omitempty" yaml:"installHint,omitempty"`
}

type Target struct {
	OS   string
	Arch string
}

type ValidationOptions struct {
	Categories map[domain.CategoryID]bool
	Target     Target
}

func Parse(raw []byte) (Manifest, error) {
	if len(raw) > MaxSize {
		return Manifest{}, fmt.Errorf("manifest excede %d bytes", MaxSize)
	}
	decoder := yaml.NewDecoder(bytes.NewReader(raw))
	decoder.KnownFields(true)
	var manifest Manifest
	if err := decoder.Decode(&manifest); err != nil {
		return Manifest{}, fmt.Errorf("manifest ilegível: %w", err)
	}
	return manifest, nil
}

func ParseAndValidate(raw []byte, opts ValidationOptions) (Manifest, error) {
	manifest, err := Parse(raw)
	if err != nil {
		return Manifest{}, err
	}
	if err := manifest.Validate(opts); err != nil {
		return Manifest{}, err
	}
	return manifest, nil
}

func (m Manifest) Validate(opts ValidationOptions) error {
	fail := func(field, reason string) error {
		return fmt.Errorf("manifest %q: campo %s %s", m.ID, field, reason)
	}
	switch {
	case m.APIVersion != APIVersion:
		return fail("apiVersion", "é desconhecido")
	case !idPattern.MatchString(m.ID):
		return fail("id", "é inválido")
	case !versionPattern.MatchString(m.Version):
		return fail("version", "não é semver")
	case strings.TrimSpace(m.Name) == "" || !safeText(m.Name, false):
		return fail("name", "é obrigatório")
	case m.Summary == "" || strings.ContainsAny(m.Summary, "\r\n") || !strings.HasSuffix(m.Summary, "."):
		return fail("summary", "deve ter uma linha e terminar com ponto")
	case !safeText(m.Summary, false) || !safeText(m.Detail, true) || !safeText(m.Glyph, false):
		return fail("texto", "contém controles de terminal")
	case opts.Categories != nil && !opts.Categories[domain.CategoryID(m.Category)]:
		return fail("category", "não existe")
	case m.Runtime.Kind != "process":
		return fail("runtime.kind", "deve ser process")
	case !m.Runtime.Protocol.Valid():
		return fail("runtime.protocol", "é vazio ou inválido")
	case unsafeExecutable(m.Runtime.Executable):
		return fail("runtime.executable", "deve ser apenas um nome relativo, sem argumentos")
	case m.UI.Mode != protocol.UIModeScreenV1:
		return fail("ui.mode", "é desconhecido")
	}
	if _, err := risk(m.Risk); err != nil {
		return fail("risk", err.Error())
	}
	if len(m.Platforms) == 0 {
		return fail("platforms", "não pode ser vazio")
	}
	for _, platform := range m.Platforms {
		if _, _, ok := splitPlatform(platform); !ok {
			return fail("platforms", "contém alvo desconhecido "+platform)
		}
	}
	seenRequirements := map[string]bool{}
	for _, requirement := range m.Requirements {
		if unsafeExecutable(requirement.Executable) || seenRequirements[requirement.Executable] {
			return fail("requirements", "contém executável vazio, inseguro ou duplicado")
		}
		if !safeText(requirement.Name, false) || !safeText(requirement.InstallHint, false) {
			return fail("requirements", "contém texto inseguro")
		}
		seenRequirements[requirement.Executable] = true
	}
	for _, value := range append(append([]string{}, m.Keywords...), m.Tags...) {
		if !safeText(value, false) {
			return fail("keywords/tags", "contém texto inseguro")
		}
	}
	for _, path := range append(append([]string{}, m.Permissions.Filesystem.Read...), m.Permissions.Filesystem.Write...) {
		if !validPermissionPath(path) {
			return fail("permissions.filesystem", "contém caminho inválido")
		}
	}
	if !protocol.ValidWorkingDir(m.Permissions.WorkingDir) {
		return fail("permissions.workingDir", "aceita somente read ou write")
	}
	for _, capability := range m.UI.Capabilities {
		if !knownCapability(capability) {
			return fail("ui.capabilities", "contém capability desconhecida "+capability)
		}
	}
	return nil
}

func safeText(value string, multiline bool) bool {
	for _, r := range value {
		if unicode.IsControl(r) && !(multiline && r == '\n') {
			return false
		}
	}
	return true
}

func unsafeExecutable(executable string) bool {
	return executable == "" || filepath.IsAbs(executable) || filepath.Base(executable) != executable ||
		strings.ContainsAny(executable, "/\\ \t\r\n") || executable == "." || executable == ".."
}

func validPermissionPath(path string) bool {
	if path == "" || strings.ContainsRune(path, 0) {
		return false
	}
	for _, r := range path {
		if unicode.IsControl(r) {
			return false
		}
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return false
	}
	return filepath.IsAbs(path) || strings.HasPrefix(filepath.ToSlash(path), "~/")
}

func knownCapability(capability string) bool {
	switch capability {
	case protocol.CapabilityNavigationBack,
		protocol.CapabilityNotificationShow,
		protocol.CapabilityClipboardWrite,
		protocol.CapabilityConfirmationRequest,
		protocol.CapabilityBrowserOpen:
		return true
	default:
		return false
	}
}

func splitPlatform(value string) (string, string, bool) {
	os, arch, ok := strings.Cut(value, "-")
	if !ok {
		return "", "", false
	}
	validOS := os == "darwin" || os == "windows" || os == "linux"
	validArch := arch == "amd64" || arch == "arm64"
	return os, arch, validOS && validArch
}

func (m Manifest) Supports(target Target) bool {
	for _, value := range m.Platforms {
		os, arch, ok := splitPlatform(value)
		if ok && os == target.OS && arch == target.Arch {
			return true
		}
	}
	return false
}

// ExecutableName aplica somente a convenção de artefato do alvo já
// selecionado pelo composition root. O manifest permanece portátil e não
// precisa duplicar o mesmo nome apenas por causa do sufixo do Windows.
func (m Manifest) ExecutableName(target Target) string {
	if target.OS == "windows" && filepath.Ext(m.Runtime.Executable) == "" {
		return m.Runtime.Executable + ".exe"
	}
	return m.Runtime.Executable
}

func (m Manifest) DomainPlatform() domain.Platform {
	var platforms domain.Platform
	for _, value := range m.Platforms {
		os, _, ok := splitPlatform(value)
		if ok {
			platforms |= domain.ParsePlatform(os)
		}
	}
	return platforms
}

func (m Manifest) Tool(installDir, executable string) (domain.Tool, error) {
	r, err := risk(m.Risk)
	if err != nil {
		return domain.Tool{}, err
	}
	requirements := make([]domain.Requirement, len(m.Requirements))
	for i, requirement := range m.Requirements {
		requirements[i] = domain.Requirement{
			Executable:  requirement.Executable,
			Name:        requirement.Name,
			InstallHint: requirement.InstallHint,
		}
	}
	tags := make([]domain.Tag, len(m.Tags))
	for i, tag := range m.Tags {
		tags[i] = domain.Tag(tag)
	}
	return domain.Tool{
		ID: domain.ToolID(m.ID), Name: m.Name, Summary: m.Summary, Detail: m.Detail,
		Category: domain.CategoryID(m.Category), Kind: domain.KindProcess, Risk: r,
		Platforms: m.DomainPlatform(), Requirements: requirements, Glyph: m.Glyph,
		Keywords: m.Keywords, Tags: tags, Version: m.Version,
		Runtime: &domain.ExternalRuntime{
			InstallDir: installDir, Executable: executable,
			ProtocolMin: m.Runtime.Protocol.Min, ProtocolMax: m.Runtime.Protocol.Max,
			UIMode: m.UI.Mode, Capabilities: append([]string(nil), m.UI.Capabilities...),
			WantsMouse: m.UI.WantsMouse,
			Permissions: domain.ToolPermissions{
				ReadPaths:  append([]string(nil), m.Permissions.Filesystem.Read...),
				WritePaths: append([]string(nil), m.Permissions.Filesystem.Write...),
				Network:    m.Permissions.Network, Subprocess: m.Permissions.Subprocess,
				WorkingDir: m.Permissions.WorkingDir,
			},
		},
	}, nil
}

func risk(value string) (domain.Risk, error) {
	switch value {
	case "safe":
		return domain.RiskSafe, nil
	case "caution":
		return domain.RiskCaution, nil
	case "destructive":
		return domain.RiskDestructive, nil
	default:
		return 0, errors.New("é desconhecido")
	}
}
