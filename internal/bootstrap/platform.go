package bootstrap

import (
	"os"
	"runtime"

	hostadapter "github.com/mateuslh/lealing/internal/adapter/outbound/hostaction"
	"github.com/mateuslh/lealing/internal/adapter/outbound/selfupdate"
	"github.com/mateuslh/lealing/internal/core/domain"
	corehost "github.com/mateuslh/lealing/internal/core/hostaction"
	"github.com/mateuslh/lealing/internal/platform/xdg"
	"github.com/mateuslh/lealing/internal/toolmanifest"
)

// nativeAdapters reúne somente integrações administrativas da engine que
// dependem do sistema operacional. Tools instaláveis escolhem os adapters
// delas após o handshake.
type nativeAdapters struct {
	host corehost.Executor
}

// adaptersFor escolhe as implementações da plataforma.
//
// É o único switch por sistema operacional do programa. Todo o resto do
// código conhece apenas as portas administrativas do host.
func adaptersFor(p domain.Platform) nativeAdapters {
	switch p {
	case domain.Darwin:
		return nativeAdapters{
			host: hostadapter.New(
				&hostadapter.Command{Executable: "/usr/bin/pbcopy"},
				&hostadapter.Command{Executable: "/usr/bin/open"},
			),
		}
	case domain.Windows:
		return nativeAdapters{
			host: hostadapter.New(
				&hostadapter.Command{Executable: "clip.exe"},
				&hostadapter.Command{
					Executable: "rundll32.exe",
					Args:       []string{"url.dll,FileProtocolHandler"},
				},
			),
		}
	default:
		return nativeAdapters{}
	}
}

// currentPlatform é a única detecção de sistema operacional do programa.
func currentPlatform() domain.Platform {
	return domain.ParsePlatform(runtime.GOOS)
}

// currentTarget identifica o artefato publicado para este binário.
func currentTarget() selfupdate.Target {
	return selfupdate.Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
}

// currentToolTarget identifica a variante de manifest instalada que pode ser
// descoberta neste binário.
func currentToolTarget() toolmanifest.Target {
	return toolmanifest.Target{OS: runtime.GOOS, Arch: runtime.GOARCH}
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
