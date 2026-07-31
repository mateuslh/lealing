// Package theme é o design system do lealing.
//
// Nenhum outro pacote da TUI declara cor, moldura ou espaçamento literal.
// Centralizar isso é o que mantém dezenas de telas coerentes entre si e o
// que torna a troca de tema uma operação de uma linha.
package theme

import "github.com/charmbracelet/lipgloss"

// Palette são os tokens de cor do tema. Todos são AdaptiveColor: o mesmo
// token resolve para um valor no terminal escuro e outro no claro, então
// nenhuma tela precisa saber em qual dos dois está rodando.
type Palette struct {
	// Superfícies, do fundo para a frente.
	Base    lipgloss.AdaptiveColor
	Surface lipgloss.AdaptiveColor
	Overlay lipgloss.AdaptiveColor

	// Molduras.
	Border      lipgloss.AdaptiveColor
	BorderFocus lipgloss.AdaptiveColor

	// Tipografia, do mais forte ao mais apagado.
	Text  lipgloss.AdaptiveColor
	Muted lipgloss.AdaptiveColor
	Faint lipgloss.AdaptiveColor

	// Cores semânticas.
	Primary   lipgloss.AdaptiveColor
	Secondary lipgloss.AdaptiveColor
	Accent    lipgloss.AdaptiveColor
	Success   lipgloss.AdaptiveColor
	Warning   lipgloss.AdaptiveColor
	Danger    lipgloss.AdaptiveColor

	// Contraste sobre superfícies preenchidas com Primary/Danger.
	OnPrimary lipgloss.AdaptiveColor

	// Spectrum é o ciclo usado para colorir categorias. Category.Accent é um
	// índice aqui — o domínio escolhe a posição, o tema escolhe a cor.
	Spectrum []lipgloss.AdaptiveColor

	// Gradient são os pontos de parada do gradiente do logo e das réguas.
	Gradient []string
}

// Midnight é o tema padrão: azul-noite profundo no escuro, papel frio no
// claro. Os pares foram escolhidos para manter contraste AA nos dois modos.
func Midnight() Palette {
	return Palette{
		Base:    lipgloss.AdaptiveColor{Dark: "#0B0E14", Light: "#FAFAFC"},
		Surface: lipgloss.AdaptiveColor{Dark: "#141926", Light: "#F0F1F6"},
		Overlay: lipgloss.AdaptiveColor{Dark: "#1C2333", Light: "#E4E6EF"},

		Border:      lipgloss.AdaptiveColor{Dark: "#232A3B", Light: "#D3D6E3"},
		BorderFocus: lipgloss.AdaptiveColor{Dark: "#3D4869", Light: "#9AA2C4"},

		Text:  lipgloss.AdaptiveColor{Dark: "#C8D0E4", Light: "#1E2233"},
		Muted: lipgloss.AdaptiveColor{Dark: "#7A849F", Light: "#5B6280"},
		Faint: lipgloss.AdaptiveColor{Dark: "#4A5370", Light: "#9096AC"},

		Primary:   lipgloss.AdaptiveColor{Dark: "#7AA2F7", Light: "#3B62C4"},
		Secondary: lipgloss.AdaptiveColor{Dark: "#BB9AF7", Light: "#7C4DD1"},
		Accent:    lipgloss.AdaptiveColor{Dark: "#7DCFFF", Light: "#0E7FB8"},
		Success:   lipgloss.AdaptiveColor{Dark: "#9ECE6A", Light: "#4A7C21"},
		Warning:   lipgloss.AdaptiveColor{Dark: "#E0AF68", Light: "#9A6B12"},
		Danger:    lipgloss.AdaptiveColor{Dark: "#F7768E", Light: "#C42B4A"},

		OnPrimary: lipgloss.AdaptiveColor{Dark: "#0B0E14", Light: "#FFFFFF"},

		Spectrum: []lipgloss.AdaptiveColor{
			{Dark: "#7AA2F7", Light: "#3B62C4"}, // azul
			{Dark: "#BB9AF7", Light: "#7C4DD1"}, // roxo
			{Dark: "#7DCFFF", Light: "#0E7FB8"}, // ciano
			{Dark: "#9ECE6A", Light: "#4A7C21"}, // verde
			{Dark: "#E0AF68", Light: "#9A6B12"}, // âmbar
			{Dark: "#F7768E", Light: "#C42B4A"}, // rosa
			{Dark: "#2AC3DE", Light: "#0B7A8C"}, // turquesa
			{Dark: "#FF9E64", Light: "#B65A18"}, // laranja
		},

		Gradient: []string{"#7AA2F7", "#7DCFFF", "#BB9AF7", "#F7768E"},
	}
}

// SpectrumAt devolve a cor da posição indicada, com wrap-around. Índices
// negativos são tratados como zero.
func (p Palette) SpectrumAt(i int) lipgloss.AdaptiveColor {
	if len(p.Spectrum) == 0 {
		return p.Primary
	}
	if i < 0 {
		i = 0
	}
	return p.Spectrum[i%len(p.Spectrum)]
}
