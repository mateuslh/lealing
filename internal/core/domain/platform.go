package domain

import (
	"runtime"
	"strings"
)

// Platform é um conjunto de sistemas operacionais, não um só: uma tool
// declara em quais roda, e a máquina atual é o conjunto de um elemento com
// que esse conjunto é testado.
//
// Bitmask, e não slice, porque a comparação é o caminho quente do registry
// (roda para cada tool na carga) e porque um conjunto vazio é detectável em
// uma comparação com zero.
type Platform uint8

const (
	// Darwin é o macOS.
	Darwin Platform = 1 << iota
	// Windows é o Windows 10 ou superior — é o piso do PowerShell e do
	// namespace CIM que os adapters usam.
	Windows
	// Linux ainda não tem adapters nativos; declarar a constante mantém o
	// contrato pronto para quando tiver.
	Linux
)

// AllPlatforms é o conjunto completo, e também o significado do zero-value de
// Tool.Platforms: uma tool que não declara nada é portável.
const AllPlatforms = Darwin | Windows | Linux

// platformNames mapeia cada bit ao nome de exibição e ao GOOS correspondente.
// A ordem é a de exibição na matriz de suporte.
var platformNames = []struct {
	bit   Platform
	goos  string
	label string
}{
	{Darwin, "darwin", "macOS"},
	{Windows, "windows", "Windows"},
	{Linux, "linux", "Linux"},
}

// CurrentPlatform devolve a plataforma em que este binário está rodando.
// Zero quando o GOOS não é nenhum dos declarados — o que faz toda tool que
// não seja explicitamente portável sumir do catálogo, que é o lado seguro.
func CurrentPlatform() Platform { return ParsePlatform(runtime.GOOS) }

// ParsePlatform converte um GOOS ("darwin", "windows") na plataforma.
// Devolve zero para valores desconhecidos.
func ParsePlatform(goos string) Platform {
	for _, p := range platformNames {
		if strings.EqualFold(goos, p.goos) {
			return p.bit
		}
	}
	return 0
}

// Has informa se todos os bits de other estão presentes. Um conjunto vazio
// nunca casa: "nenhuma plataforma" não é "qualquer plataforma".
func (p Platform) Has(other Platform) bool {
	return other != 0 && p&other == other
}

// Any informa se há interseção entre os dois conjuntos.
func (p Platform) Any(other Platform) bool { return p&other != 0 }

// Names devolve os rótulos de exibição, na ordem canônica.
func (p Platform) Names() []string {
	out := make([]string, 0, len(platformNames))
	for _, entry := range platformNames {
		if p&entry.bit != 0 {
			out = append(out, entry.label)
		}
	}
	return out
}

// String implementa fmt.Stringer: "macOS · Windows".
func (p Platform) String() string {
	names := p.Names()
	if len(names) == 0 {
		return "nenhuma"
	}
	return strings.Join(names, " · ")
}

// Valid recusa bits fora dos declarados, que só chegam aqui por engano de
// quem escreveu a tool (um `1 << 7` digitado à mão, por exemplo).
func (p Platform) Valid() bool { return p&^AllPlatforms == 0 }
