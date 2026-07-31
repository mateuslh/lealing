package bootstrap

import (
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/mateuslh/lealing/internal/adapter/outbound/githubclone"
	"github.com/mateuslh/lealing/internal/adapter/outbound/intellij"
	"github.com/mateuslh/lealing/internal/adapter/outbound/macos"
	"github.com/mateuslh/lealing/internal/adapter/outbound/selfupdate"
	"github.com/mateuslh/lealing/internal/adapter/outbound/usage"
	"github.com/mateuslh/lealing/internal/adapter/outbound/windows"
	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/power"
	"github.com/mateuslh/lealing/internal/core/repoclone"
	"github.com/mateuslh/lealing/internal/core/sysinfo"
	"github.com/mateuslh/lealing/internal/core/tokens"
	"github.com/mateuslh/lealing/internal/platform/xdg"
)

// nativeAdapters são as implementações que dependem do sistema operacional.
//
// Um campo nil quer dizer "esta plataforma não tem adapter para esta porta",
// e não é um erro: a tool correspondente declara em quais sistemas roda e o
// registry a esconde nos demais. Os dois lados precisam concordar — um campo
// nil aqui com a tool declarada como suportada no catálogo abriria uma tela
// que estoura no primeiro Read.
type nativeAdapters struct {
	inspector sysinfo.Inspector
	power     power.Manager
	repoClone repoclone.Manager
}

// adaptersFor escolhe as implementações da plataforma.
//
// É o único switch por sistema operacional do programa. Todo o resto do
// código conhece apenas as portas — é o que permite os parsers do Windows
// serem testados no Mac, e o que fará uma terceira plataforma custar um case.
func adaptersFor(p domain.Platform, now func() time.Time, home string) nativeAdapters {
	switch p {
	case domain.Darwin:
		return nativeAdapters{
			inspector: macos.NewSystemInspector(now),
			power:     macos.NewPowerManager(),
			repoClone: newRepoCloner(home,
				filepath.Join(home, "Library", "Application Support", "JetBrains",
					"IntelliJIdea*", "options", "recentProjects.xml"),
				filepath.Join(home, "Library", "Application Support", "JetBrains",
					"IdeaIC*", "options", "recentProjects.xml"),
			),
		}
	case domain.Windows:
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(home, "AppData", "Roaming")
		}
		return nativeAdapters{
			inspector: windows.NewSystemInspector(now),
			power:     windows.NewPowerManager(),
			repoClone: newRepoCloner(home,
				filepath.Join(appData, "JetBrains", "IntelliJIdea*",
					"options", "recentProjects.xml"),
				filepath.Join(appData, "JetBrains", "IdeaIC*",
					"options", "recentProjects.xml"),
			),
		}
	default:
		return nativeAdapters{}
	}
}

// newRepoCloner compõe adapters genéricos. A composição vive aqui, e não
// dentro de outro adapter: githubclone não precisa conhecer IntelliJ e os
// pacotes macos/windows não dependem de adapters irmãos.
func newRepoCloner(home string, recentPatterns ...string) repoclone.Manager {
	recent := intellij.NewRecentProjects(home, recentPatterns...)
	return githubclone.New(home, recent)
}

// currentPlatform é a única detecção de sistema operacional do programa.
func currentPlatform() domain.Platform {
	return domain.ParsePlatform(runtime.GOOS)
}

// currentTarget identifica o artefato publicado para este binário.
func currentTarget() selfupdate.Target {
	return selfupdate.Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
}

// directoriesFor resolve os caminhos nativos sem deixar a infraestrutura
// detectar a plataforma por conta própria.
func directoriesFor(p domain.Platform) xdg.Directories {
	return xdg.Resolve(p == domain.Windows)
}

// userNameFor resolve o rótulo da saudação com as convenções da plataforma.
func userNameFor(p domain.Platform) string {
	if p == domain.Windows {
		return os.Getenv("USERNAME")
	}
	return os.Getenv("USER")
}

// tokenProvidersFor escolhe somente a integração realmente nativa: o
// chaveiro do Claude Code no macOS. Caminhos de logs e credenciais continuam
// explícitos e iguais nas demais plataformas.
func tokenProvidersFor(p domain.Platform, home string) []tokens.Provider {
	claudeCredentials := usage.NewLocalCredentials(
		filepath.Join(home, ".claude", ".credentials.json"),
		p == domain.Darwin,
	)
	codexCredentials := usage.NewCodexFile(filepath.Join(home, ".codex", "auth.json"))
	return []tokens.Provider{
		usage.NewClaudeCode(
			filepath.Join(home, ".claude", "projects"),
			claudeCredentials,
		),
		usage.NewCodex(
			filepath.Join(home, ".codex", "sessions"),
			codexCredentials,
		),
	}
}
