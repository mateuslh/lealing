package bootstrap

import (
	"time"

	"github.com/mateuslh/lealing/internal/adapter/outbound/macos"
	"github.com/mateuslh/lealing/internal/adapter/outbound/windows"
	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/power"
	"github.com/mateuslh/lealing/internal/core/sysinfo"
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
}

// adaptersFor escolhe as implementações da plataforma.
//
// É o único switch por sistema operacional do programa. Todo o resto do
// código conhece apenas as portas — é o que permite os parsers do Windows
// serem testados no Mac, e o que fará uma terceira plataforma custar um case.
func adaptersFor(p domain.Platform, now func() time.Time) nativeAdapters {
	switch p {
	case domain.Darwin:
		return nativeAdapters{
			inspector: macos.NewSystemInspector(now),
			power:     macos.NewPowerManager(),
		}
	case domain.Windows:
		return nativeAdapters{
			inspector: windows.NewSystemInspector(now),
			power:     windows.NewPowerManager(),
		}
	default:
		return nativeAdapters{}
	}
}
