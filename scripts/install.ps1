# Instalador do lealing para Windows 10+.
#
#   irm https://raw.githubusercontent.com/mateuslh/lealing/main/scripts/install.ps1 | iex
#
# Baixa o binário da última release, confere o SHA-256 publicado e instala sem
# exigir Go, Git ou privilégios de administrador.
#
# Variáveis de ambiente:
#   LEALING_VERSION   tag a instalar (padrão: a última release)
#   LEALING_BIN_DIR   diretório de instalação
#   LEALING_NO_PATH   se definida, não altera o PATH do usuário

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"
$ProgressPreference = "SilentlyContinue"

$Repository = "mateuslh/lealing"
$Binary = "lealing.exe"

function Write-Info {
    param([string]$Message)
    Write-Host $Message
}

function Get-Architecture {
    $architecture = $env:PROCESSOR_ARCHITEW6432
    if ([string]::IsNullOrWhiteSpace($architecture)) {
        $architecture = $env:PROCESSOR_ARCHITECTURE
    }

    switch ($architecture.ToUpperInvariant()) {
        "AMD64" { return "amd64" }
        "ARM64" { return "arm64" }
        default { throw "arquitetura não suportada: $architecture" }
    }
}

function Get-LatestVersion {
    $headers = @{ "User-Agent" = "lealing-installer" }
    $release = Invoke-RestMethod `
        -Uri "https://api.github.com/repos/$Repository/releases/latest" `
        -Headers $headers
    if ([string]::IsNullOrWhiteSpace($release.tag_name)) {
        throw "$Repository ainda não tem release publicada"
    }
    return [string]$release.tag_name
}

function Download-File {
    param(
        [string]$Uri,
        [string]$Destination
    )
    Invoke-WebRequest -UseBasicParsing -Uri $Uri -OutFile $Destination
}

function Test-PathContains {
    param(
        [string]$PathValue,
        [string]$Directory
    )
    if ([string]::IsNullOrWhiteSpace($PathValue)) {
        return $false
    }

    $wanted = [IO.Path]::GetFullPath($Directory).TrimEnd('\')
    foreach ($entry in ($PathValue -split ';')) {
        if ([string]::IsNullOrWhiteSpace($entry)) {
            continue
        }
        try {
            $current = [IO.Path]::GetFullPath($entry).TrimEnd('\')
            if ([string]::Equals($current, $wanted, [StringComparison]::OrdinalIgnoreCase)) {
                return $true
            }
        }
        catch {
            # Uma entrada inválida já presente no PATH não deve impedir a
            # instalação nem ser reescrita pelo instalador.
        }
    }
    return $false
}

function Add-ToUserPath {
    param([string]$Directory)

    if (Test-PathContains -PathValue $env:Path -Directory $Directory) {
        Write-Info "$Directory já está no PATH"
        return
    }

    $userPath = [Environment]::GetEnvironmentVariable("Path", "User")
    if ([string]::IsNullOrWhiteSpace($userPath)) {
        $newPath = $Directory
    }
    else {
        $newPath = $userPath.TrimEnd(';') + ';' + $Directory
    }
    [Environment]::SetEnvironmentVariable("Path", $newPath, "User")

    # O PATH persistente vale para terminais novos. Atualizar o processo
    # também permite rodar `lealing` logo depois do `irm ... | iex`.
    $env:Path = $Directory + ';' + $env:Path
    Write-Info "$Directory adicionado ao PATH do usuário"
}

# PowerShell 5.1 no Windows 10 pode iniciar sem TLS 1.2 habilitado.
[Net.ServicePointManager]::SecurityProtocol =
    [Net.ServicePointManager]::SecurityProtocol -bor [Net.SecurityProtocolType]::Tls12

$architecture = Get-Architecture
$version = $env:LEALING_VERSION
if ([string]::IsNullOrWhiteSpace($version)) {
    $version = Get-LatestVersion
}
if ($version -notmatch '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$') {
    throw "versão inválida: $version"
}

$binDir = $env:LEALING_BIN_DIR
if ([string]::IsNullOrWhiteSpace($binDir)) {
    $binDir = Join-Path $env:LOCALAPPDATA "lealing\bin"
}
$binDir = [IO.Path]::GetFullPath($binDir)

$asset = "lealing_windows_$architecture.zip"
$baseUrl = "https://github.com/$Repository/releases/download/$version"
$tempDir = Join-Path ([IO.Path]::GetTempPath()) ("lealing-" + [guid]::NewGuid().ToString("N"))

Write-Info "lealing $version (windows/$architecture)"
New-Item -ItemType Directory -Path $tempDir | Out-Null

try {
    $archive = Join-Path $tempDir $asset
    $checksums = Join-Path $tempDir "checksums.txt"

    Write-Info "baixando $asset..."
    Download-File -Uri "$baseUrl/$asset" -Destination $archive
    Download-File -Uri "$baseUrl/checksums.txt" -Destination $checksums

    $assetPattern = '^[0-9a-fA-F]{64}\s+' + [regex]::Escape($asset) + '$'
    $checksumLine = Get-Content $checksums |
        Where-Object { $_ -match $assetPattern } |
        Select-Object -First 1
    if ([string]::IsNullOrWhiteSpace($checksumLine)) {
        throw "$asset não consta no checksums.txt da release"
    }

    $expected = ($checksumLine -split '\s+')[0].ToLowerInvariant()
    $actual = (Get-FileHash -Algorithm SHA256 -Path $archive).Hash.ToLowerInvariant()
    if ($actual -ne $expected) {
        throw "checksum não confere ($actual, esperava $expected) — nada foi instalado"
    }

    $expanded = Join-Path $tempDir "expanded"
    Expand-Archive -Path $archive -DestinationPath $expanded -Force
    $source = Join-Path $expanded $Binary
    if (-not (Test-Path -LiteralPath $source -PathType Leaf)) {
        throw "$Binary não veio dentro de $asset"
    }

    New-Item -ItemType Directory -Path $binDir -Force | Out-Null
    $target = Join-Path $binDir $Binary
    $staged = Join-Path $binDir "$Binary.new"
    Copy-Item -LiteralPath $source -Destination $staged -Force

    if (Test-Path -LiteralPath $target -PathType Leaf) {
        # File.Replace troca arquivos no mesmo volume de forma atômica e
        # mantém o executável anterior intacto se a operação falhar.
        [IO.File]::Replace($staged, $target, $null)
    }
    else {
        Move-Item -LiteralPath $staged -Destination $target
    }

    Write-Info "instalado: $target"

    if (-not [string]::IsNullOrWhiteSpace($env:LEALING_NO_PATH)) {
        Write-Info "adicione ao PATH: `$env:Path = `"$binDir;`$env:Path`""
    }
    else {
        Add-ToUserPath -Directory $binDir
        Write-Info "rode: lealing"
    }
}
finally {
    if (Test-Path -LiteralPath $tempDir) {
        Remove-Item -LiteralPath $tempDir -Recurse -Force
    }
}
