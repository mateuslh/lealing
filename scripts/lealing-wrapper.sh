#!/bin/sh
# Wrapper do lealing instalado no PATH.
#
# Recompila o binário quando qualquer fonte do repositório for mais recente
# que ele, e só então executa. Assim, editar o código e chamar `lealing` no
# terminal já usa a versão nova, sem `go install` manual.
#
# O custo é uma comparação de mtime (poucos milissegundos); o `go build` só
# roda quando há mudança de verdade, e mesmo aí o cache do Go o torna rápido.
#
# Este arquivo é um modelo: `make install` reescreve @@REPO@@ e @@GO@@ com os
# caminhos reais e instala o resultado. Não o edite depois de instalado —
# edite aqui e rode `make install` de novo.

set -eu

REPO="@@REPO@@"
GO="@@GO@@"
BIN="$REPO/bin/lealing"

# Sem o repositório não há o que recompilar; se houver um binário antigo,
# ele ainda serve.
if [ ! -d "$REPO" ]; then
    if [ -x "$BIN" ]; then
        exec "$BIN" "$@"
    fi
    echo "lealing: repositório não encontrado em $REPO" >&2
    exit 1
fi

needs_build() {
    [ ! -x "$BIN" ] && return 0

    # -newer compara mtime. Paramos no primeiro achado: saber *se* algo mudou
    # basta, e varrer o resto seria desperdício.
    found=$(find "$REPO/cmd" "$REPO/internal" \
        -name '*.go' -newer "$BIN" -print -quit 2>/dev/null || true)
    [ -n "$found" ] && return 0

    # go.mod/go.sum mudam sem que nenhum .go mude (troca de dependência).
    for f in "$REPO/go.mod" "$REPO/go.sum"; do
        [ -f "$f" ] && [ "$f" -nt "$BIN" ] && return 0
    done

    return 1
}

if needs_build; then
    # A mensagem vai para stderr para não sujar a saída de `lealing -render`,
    # que é feita para ser canalizada.
    printf 'lealing: recompilando…\n' >&2

    version=$(cd "$REPO" && git describe --tags --always --dirty 2>/dev/null || echo dev)

    if ! (cd "$REPO" && "$GO" build -trimpath \
        -ldflags "-s -w -X main.version=$version" \
        -o "$BIN" ./cmd/lealing); then
        # Compilação quebrada não pode deixar o usuário sem ferramenta: se
        # existe um binário anterior que funciona, seguimos com ele.
        if [ -x "$BIN" ]; then
            printf 'lealing: build falhou — usando a versão anterior\n' >&2
        else
            printf 'lealing: build falhou e não há binário anterior\n' >&2
            exit 1
        fi
    fi
fi

exec "$BIN" "$@"
