package selfupdate

import (
	"fmt"
	"strconv"
	"strings"
)

// Version é uma versão semântica tolerante ao que o build injeta.
//
// O binário recebe a versão de `git describe --tags`, que devolve tanto
// "v1.4.0" (exatamente em cima da tag) quanto "v1.4.0-3-gaf21c9-dirty" (três
// commits depois dela) quanto "dev" (repositório sem tag alguma). Os três
// precisam entrar aqui sem erro: uma versão ilegível vira Known=false e a
// tela trata esse caso, em vez de a tool se recusar a abrir.
type Version struct {
	Major, Minor, Patch int
	// Suffix é o que vem depois do número ("3-gaf21c9-dirty"). Presente
	// significa build local à frente da tag — ver Compare.
	Suffix string
	// Raw é o texto original, que é o que a tela mostra.
	Raw string
	// Known diz se os três números foram interpretados.
	Known bool
}

// ParseVersion interpreta uma versão. Nunca falha: o que não der para ler
// volta com Known=false.
func ParseVersion(s string) Version {
	v := Version{Raw: strings.TrimSpace(s)}

	core := strings.TrimPrefix(v.Raw, "v")
	suffix := ""
	if i := strings.IndexAny(core, "-+"); i >= 0 {
		suffix, core = core[i+1:], core[:i]
	}

	parts := strings.Split(core, ".")
	if len(parts) == 0 || len(parts) > 3 {
		return v
	}
	var nums [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v
		}
		nums[i] = n
	}

	v.Major, v.Minor, v.Patch = nums[0], nums[1], nums[2]
	v.Suffix = suffix
	v.Known = true
	return v
}

// Compare devolve -1, 0 ou 1 comparando v com o. Só faz sentido entre versões
// conhecidas; o chamador confere Known antes.
//
// Com os números empatados e sufixo em apenas um dos lados, o sufixo vence.
// É o oposto da regra do semver para pré-lançamentos, e de propósito: aqui o
// sufixo vem do `git describe`, onde significa "commits depois da tag", não
// "candidato antes dela".
func (v Version) Compare(o Version) int {
	if c := sign(v.Major - o.Major); c != 0 {
		return c
	}
	if c := sign(v.Minor - o.Minor); c != 0 {
		return c
	}
	if c := sign(v.Patch - o.Patch); c != 0 {
		return c
	}
	switch {
	case v.Suffix != "" && o.Suffix == "":
		return 1
	case v.Suffix == "" && o.Suffix != "":
		return -1
	}
	return 0
}

// String implementa fmt.Stringer, devolvendo o texto original quando há um.
func (v Version) String() string {
	if v.Raw != "" {
		return v.Raw
	}
	if !v.Known {
		return "desconhecida"
	}
	return fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch)
}

func sign(n int) int {
	switch {
	case n > 0:
		return 1
	case n < 0:
		return -1
	}
	return 0
}
