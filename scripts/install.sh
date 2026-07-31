#!/bin/sh
# Instalador do lealing.
#
#   curl -fsSL https://raw.githubusercontent.com/mateuslh/lealing/main/scripts/install.sh | sh
#
# Baixa o binário da última release, confere o checksum publicado e o instala
# em ~/.local/bin. Não precisa de Go, de git nem de privilégio.
#
# Variáveis:
#   LEALING_VERSION   tag a instalar (padrão: a última release)
#   LEALING_BIN_DIR   diretório de instalação (padrão: ~/.local/bin)
#
# O script é POSIX sh de propósito: ele roda antes de o usuário ter qualquer
# coisa instalada, então não pode depender de bash, de jq nem de python.

set -eu

REPO="mateuslh/lealing"
BINARY="lealing"
BIN_DIR="${LEALING_BIN_DIR:-$HOME/.local/bin}"

die() {
    printf '%s: %s\n' "$BINARY" "$1" >&2
    exit 1
}

info() {
    printf '%s\n' "$1" >&2
}

need() {
    command -v "$1" >/dev/null 2>&1 || die "$1 é necessário e não está no PATH"
}

# --- Plataforma --------------------------------------------------------

detect_platform() {
    os=$(uname -s)
    case "$os" in
        Darwin) os=darwin ;;
        Linux)  os=linux ;;
        *)      die "sistema não suportado por este script: $os (no Windows, baixe o .zip da release)" ;;
    esac

    # uname -m fala vários dialetos para a mesma arquitetura.
    arch=$(uname -m)
    case "$arch" in
        arm64|aarch64) arch=arm64 ;;
        x86_64|amd64)  arch=amd64 ;;
        *)             die "arquitetura não suportada: $arch" ;;
    esac

    printf '%s_%s' "$os" "$arch"
}

# --- Release -----------------------------------------------------------

# latest_tag lê a tag da última release sem jq: a chave "tag_name" vem uma vez
# só no JSON de /releases/latest, então um sed sobre ela é suficiente e não
# adiciona dependência ao instalador.
latest_tag() {
    api="https://api.github.com/repos/$REPO/releases/latest"
    body=$(fetch "$api") || die "não consegui consultar as releases de $REPO"
    tag=$(printf '%s' "$body" | sed -n 's/.*"tag_name"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1)
    [ -n "$tag" ] || die "$REPO ainda não tem release publicada"
    printf '%s' "$tag"
}

fetch() {
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL "$1"
    else
        wget -qO- "$1"
    fi
}

download() {
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL -o "$2" "$1"
    else
        wget -qO "$2" "$1"
    fi
}

# checksum imprime o sha256 do arquivo. macOS traz shasum; Linux, sha256sum.
checksum() {
    if command -v sha256sum >/dev/null 2>&1; then
        sha256sum "$1" | cut -d' ' -f1
    elif command -v shasum >/dev/null 2>&1; then
        shasum -a 256 "$1" | cut -d' ' -f1
    else
        die "nem sha256sum nem shasum encontrados — sem eles não dá para verificar o download"
    fi
}

# --- Instalação --------------------------------------------------------

main() {
    command -v curl >/dev/null 2>&1 || command -v wget >/dev/null 2>&1 || die "curl ou wget é necessário"
    need tar
    need mktemp

    platform=$(detect_platform)
    version="${LEALING_VERSION:-$(latest_tag)}"
    asset="${BINARY}_${platform}.tar.gz"
    base="https://github.com/$REPO/releases/download/$version"

    info "lealing $version ($platform)"

    tmp=$(mktemp -d)
    # O trap cobre a saída por erro também: set -e sai no meio do caminho e
    # deixaria o tarball baixado em /tmp para sempre.
    trap 'rm -rf "$tmp"' EXIT INT TERM

    info "baixando $asset…"
    download "$base/$asset" "$tmp/$asset" || die "não consegui baixar $base/$asset"
    download "$base/checksums.txt" "$tmp/checksums.txt" || die "não consegui baixar o checksums.txt"

    want=$(grep " $asset\$" "$tmp/checksums.txt" | cut -d' ' -f1)
    [ -n "$want" ] || die "$asset não consta no checksums.txt da release"
    got=$(checksum "$tmp/$asset")
    # Um binário que não bate com o checksum publicado não é instalado em
    # hipótese alguma: ou o download corrompeu, ou não é o arquivo publicado.
    [ "$want" = "$got" ] || die "checksum não confere ($got, esperava $want) — nada foi instalado"

    tar -xzf "$tmp/$asset" -C "$tmp"
    [ -f "$tmp/$BINARY" ] || die "$BINARY não veio dentro de $asset"

    mkdir -p "$BIN_DIR"
    chmod +x "$tmp/$BINARY"
    # Move para o destino em dois passos: se o binário estiver em execução,
    # sobrescrevê-lo direto pode falhar, e o mv sobre o mesmo volume é atômico.
    mv -f "$tmp/$BINARY" "$BIN_DIR/$BINARY.new"
    mv -f "$BIN_DIR/$BINARY.new" "$BIN_DIR/$BINARY"

    info "instalado: $BIN_DIR/$BINARY"

    case ":$PATH:" in
        *":$BIN_DIR:"*)
            info "rode: $BINARY"
            ;;
        *)
            info ""
            info "AVISO: $BIN_DIR não está no PATH. Adicione ao seu ~/.zprofile ou ~/.bashrc:"
            info "  export PATH=\"$BIN_DIR:\$PATH\""
            ;;
    esac
}

main "$@"
