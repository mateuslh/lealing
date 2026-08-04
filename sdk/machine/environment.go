// Package machine oferece às tools o contexto da máquina negociado no
// handshake e operações básicas que respeitam as permissões do manifest.
//
// A plataforma vem da engine, não de runtime.GOOS. Isso permite que a
// composição da tool seja testada para macOS, Linux e Windows na mesma suíte.
package machine

import (
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/mateuslh/lealing/sdk/protocol"
)

// Platform é o GOOS negociado com a engine.
type Platform string

const (
	Darwin  Platform = "darwin"
	Linux   Platform = "linux"
	Windows Platform = "windows"
)

// ParsePlatform normaliza um nome de plataforma conhecido. Um valor
// desconhecido vira a plataforma vazia e falha em Valid.
func ParsePlatform(value string) Platform {
	platform := Platform(strings.ToLower(strings.TrimSpace(value)))
	if !platform.Valid() {
		return ""
	}
	return platform
}

// Valid informa se a plataforma faz parte do contrato público.
func (p Platform) Valid() bool { return p == Darwin || p == Linux || p == Windows }

// Unix informa se caminhos e processos seguem a família Unix.
func (p Platform) Unix() bool { return p == Darwin || p == Linux }

// Architecture é a arquitetura negociada com a engine.
type Architecture string

const (
	AMD64 Architecture = "amd64"
	ARM64 Architecture = "arm64"
)

// ParseArchitecture normaliza uma arquitetura publicada pelo lealing.
func ParseArchitecture(value string) Architecture {
	architecture := Architecture(strings.ToLower(strings.TrimSpace(value)))
	if architecture != AMD64 && architecture != ARM64 {
		return ""
	}
	return architecture
}

// PlatformError indica que não há implementação para o alvo negociado.
type PlatformError struct{ Platform Platform }

func (e *PlatformError) Error() string {
	if !e.Platform.Valid() {
		return "plataforma da máquina inválida"
	}
	return fmt.Sprintf("tool sem implementação para %s", e.Platform)
}

// Select constrói somente a implementação da plataforma negociada. A
// ausência retorna erro em vez de cair silenciosamente no adapter de outro
// sistema.
func Select[T any](platform Platform, factories map[Platform]func() T) (T, error) {
	factory := factories[platform]
	if !platform.Valid() || factory == nil {
		var zero T
		return zero, &PlatformError{Platform: platform}
	}
	return factory(), nil
}

// Environment é o retrato da máquina entregue à tool no handshake.
type Environment struct {
	Platform     Platform
	Architecture Architecture
	HomeDir      string
	DataDir      string
	CacheDir     string
	// WorkingDir é o diretório de onde o usuário abriu a engine. Fica vazio
	// quando o manifest não pede permissions.workingDir ou quando a engine
	// é anterior ao campo.
	WorkingDir  string
	Permissions protocol.Permissions
}

// NewEnvironment converte o DTO do protocolo em contexto operacional.
// HomeDir vazio mantém compatibilidade com engines antigas.
func NewEnvironment(initialize protocol.Initialize) Environment {
	home := initialize.HomeDir
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	permissions := initialize.Permissions
	permissions.Filesystem.Read = append([]string(nil), permissions.Filesystem.Read...)
	permissions.Filesystem.Write = append([]string(nil), permissions.Filesystem.Write...)
	if !protocol.ValidWorkingDir(permissions.WorkingDir) {
		// Nível desconhecido de uma engine futura não vira concessão.
		permissions.WorkingDir = protocol.WorkingDirNone
	}
	return Environment{
		Platform: ParsePlatform(initialize.Platform), Architecture: ParseArchitecture(initialize.Architecture),
		HomeDir: home, DataDir: initialize.DataDir, CacheDir: initialize.CacheDir,
		WorkingDir: initialize.WorkingDir, Permissions: permissions,
	}
}

// CanUseWorkingDir informa se a tool recebeu de fato o diretório de trabalho.
// Uma tool que depende dele deve conferir isso antes de tocar o disco, porque
// engines antigas e instalações sem a permissão devolvem o valor vazio.
func (e Environment) CanUseWorkingDir() bool {
	return e.WorkingDir != "" && e.Permissions.WorkingDir != protocol.WorkingDirNone
}

// WorkingPath resolve um caminho dentro do diretório de trabalho, como
// DataPath e CachePath fazem para os diretórios privados. A concessão de
// escrita só existe quando o manifest declara workingDir: write.
func (e Environment) WorkingPath(elements ...string) (string, error) {
	if !e.CanUseWorkingDir() {
		return "", &PermissionError{Operation: "de diretório de trabalho", Path: e.WorkingDir}
	}
	candidate := e.WorkingDir
	for _, element := range elements {
		candidate = joinFor(e.Platform, candidate, element)
	}
	return e.ResolveRead(candidate)
}

// PermissionError informa qual operação ficou fora das concessões do
// manifest. A SDK ajuda tools bem-comportadas; ela não substitui um sandbox
// do sistema operacional.
type PermissionError struct {
	Operation string
	Path      string
}

func (e *PermissionError) Error() string {
	if e.Path == "" {
		return fmt.Sprintf("permissão %s não concedida à tool", e.Operation)
	}
	return fmt.Sprintf("permissão %s não concedida para %s", e.Operation, e.Path)
}

// ErrPermissionDenied permite identificar recusas com errors.Is.
var ErrPermissionDenied = errors.New("permissão da máquina não concedida")

func (e *PermissionError) Unwrap() error { return ErrPermissionDenied }

// ResolveRead expande um caminho relativo à home e confirma a concessão de
// leitura. Engines antigas, que não enviavam HomeDir, ainda são atendidas por
// uma busca segmentada no fim dos caminhos concedidos.
func (e Environment) ResolveRead(name string) (string, error) {
	return e.resolve(name, "de leitura", e.readGrants())
}

// ResolveWrite é o equivalente de ResolveRead para escrita.
func (e Environment) ResolveWrite(name string) (string, error) {
	return e.resolve(name, "de escrita", e.writeGrants())
}

// DataPath resolve um caminho dentro do diretório persistente privado.
func (e Environment) DataPath(elements ...string) (string, error) {
	return e.privatePath(e.DataDir, elements...)
}

// CachePath resolve um caminho dentro do cache privado.
func (e Environment) CachePath(elements ...string) (string, error) {
	return e.privatePath(e.CacheDir, elements...)
}

// CanRead informa se path está dentro de uma concessão de leitura.
func (e Environment) CanRead(path string) bool { return e.allowed(path, e.readGrants()) }

// CanWrite informa se path está dentro de uma concessão de escrita.
func (e Environment) CanWrite(path string) bool { return e.allowed(path, e.writeGrants()) }

func (e Environment) readGrants() []string {
	grants := append([]string(nil), e.Permissions.Filesystem.Read...)
	// Os diretórios privados são criados pela engine para a tool e sempre
	// podem ser relidos, mesmo sem aparecer no manifest.
	grants = appendNonEmpty(grants, e.DataDir, e.CacheDir)
	// Ler o diretório de trabalho basta declarar read; escrever exige write.
	if e.Permissions.WorkingDir == protocol.WorkingDirRead || e.Permissions.WorkingDir == protocol.WorkingDirWrite {
		grants = appendNonEmpty(grants, e.WorkingDir)
	}
	return grants
}

func (e Environment) writeGrants() []string {
	grants := append([]string(nil), e.Permissions.Filesystem.Write...)
	grants = appendNonEmpty(grants, e.DataDir, e.CacheDir)
	if e.Permissions.WorkingDir == protocol.WorkingDirWrite {
		grants = appendNonEmpty(grants, e.WorkingDir)
	}
	return grants
}

func appendNonEmpty(values []string, candidates ...string) []string {
	for _, candidate := range candidates {
		if candidate != "" {
			values = append(values, candidate)
		}
	}
	return values
}

func (e Environment) resolve(name, operation string, grants []string) (string, error) {
	if strings.TrimSpace(name) == "" {
		return "", &PermissionError{Operation: operation, Path: name}
	}
	candidate := e.expandHome(name)
	if e.allowed(candidate, grants) {
		return cleanFor(e.Platform, candidate), nil
	}

	// O fallback por sufixo é necessário somente para engines anteriores ao
	// campo homeDir. A comparação por segmentos evita que "sessions-old"
	// seja aceito como se fosse "sessions".
	if e.HomeDir == "" {
		suffix := normalizeFor(e.Platform, strings.TrimPrefix(strings.TrimPrefix(name, "~/"), `~\`))
		for _, grant := range grants {
			normalized := normalizeFor(e.Platform, grant)
			if normalized == suffix || strings.HasSuffix(normalized, "/"+suffix) {
				return cleanFor(e.Platform, grant), nil
			}
		}
	}
	return "", &PermissionError{Operation: operation, Path: candidate}
}

func (e Environment) privatePath(root string, elements ...string) (string, error) {
	if root == "" {
		return "", &PermissionError{Operation: "de escrita", Path: root}
	}
	candidate := root
	for _, element := range elements {
		candidate = joinFor(e.Platform, candidate, element)
	}
	return e.ResolveWrite(candidate)
}

func (e Environment) allowed(candidate string, grants []string) bool {
	candidate = normalizeFor(e.Platform, candidate)
	if candidate == "" || candidate == "." {
		return false
	}
	for _, grant := range grants {
		grant = normalizeFor(e.Platform, grant)
		if grant != "" && (candidate == grant || strings.HasPrefix(candidate, grant+"/")) {
			return true
		}
	}
	return false
}

func (e Environment) expandHome(name string) string {
	clean := strings.ReplaceAll(name, `\`, "/")
	switch {
	case clean == "~":
		return e.HomeDir
	case strings.HasPrefix(clean, "~/"):
		return joinFor(e.Platform, e.HomeDir, strings.TrimPrefix(clean, "~/"))
	case !isAbsFor(e.Platform, clean) && e.HomeDir != "":
		return joinFor(e.Platform, e.HomeDir, clean)
	default:
		return name
	}
}

func normalizeFor(platform Platform, value string) string {
	value = strings.ReplaceAll(strings.TrimSpace(value), `\`, "/")
	value = path.Clean(value)
	if platform == Windows {
		value = strings.ToLower(value)
	}
	return strings.TrimSuffix(value, "/")
}

func cleanFor(platform Platform, value string) string {
	slashed := strings.ReplaceAll(value, `\`, "/")
	unc := platform == Windows && strings.HasPrefix(slashed, "//")
	clean := path.Clean(slashed)
	if platform == Windows {
		clean = strings.ReplaceAll(clean, "/", `\`)
		if unc && !strings.HasPrefix(clean, `\\`) {
			clean = `\` + clean
		}
		return clean
	}
	return clean
}

func joinFor(platform Platform, base, name string) string {
	joined := path.Join(strings.ReplaceAll(base, `\`, "/"), strings.ReplaceAll(name, `\`, "/"))
	return cleanFor(platform, joined)
}

func isAbsFor(platform Platform, value string) bool {
	if platform == Windows {
		return strings.HasPrefix(value, "//") ||
			(len(value) >= 3 && value[1] == ':' && value[2] == '/')
	}
	return strings.HasPrefix(value, "/")
}
