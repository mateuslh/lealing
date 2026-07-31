// Package power é a tela da tool "Controle de Energia".
package power

import "strconv"

// A tabela de campos, a escala de minutos e os modos de hibernação vivem em
// core/power: são domínio, e precisam ser os mesmos para quem mescla presets
// e para quem desenha a lista. Aqui fica só a formatação, que é apresentação.

// formatMinutes descreve uma duração em minutos para exibição.
func formatMinutes(m int) string {
	switch {
	case m == 0:
		return "nunca"
	case m < 60:
		return strconv.Itoa(m) + " min"
	case m%60 == 0:
		return strconv.Itoa(m/60) + "h"
	default:
		return strconv.Itoa(m/60) + "h " + strconv.Itoa(m%60) + "min"
	}
}

// formatHibernate explica o modo em vez de mostrar só o número.
func formatHibernate(mode int) string {
	switch mode {
	case 0:
		return "0 · só RAM"
	case 25:
		return "25 · só disco"
	default:
		return "3 · RAM + disco"
	}
}
