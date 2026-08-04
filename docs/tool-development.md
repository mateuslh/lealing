# Desenvolvendo tools para o lealing

Este é o guia normativo para criar, testar, empacotar e publicar uma tool do
lealing. O fluxo para autores é definido aqui; o contrato executável do SDK
é definido e testado num repositório independente,
[`github.com/mateuslh/lealing-sdk`](https://github.com/mateuslh/lealing-sdk)
— ele **não é parte desta engine**, é uma dependência pública que autores de
tools importam diretamente e que esta engine também referencia por versão:

- este documento define o fluxo para autores;
- `github.com/mateuslh/lealing-sdk` (pacote `protocol`) define os DTOs e o
  framing serializável;
- `github.com/mateuslh/lealing-sdk` (pacote `screen`) adapta models Bubble
  Tea ao protocolo `screen-v1`;
- `github.com/mateuslh/lealing-sdk` (pacote `component`) oferece componentes
  visuais públicos;
- `github.com/mateuslh/lealing-sdk` (pacote `machine`) oferece plataforma,
  caminhos, arquivos e subprocessos;
- `internal/toolmanifest`, nesta engine, valida `manifest.yaml`;
- `internal/core/marketplace`, nesta engine, valida `index.json`.

Repositórios de tools, inclusive a origem configurada por padrão, são somente
exemplos e consumidores desses contratos. O conteúdo deles não acrescenta,
remove nem altera uma regra da engine nem do SDK. Em caso de divergência
sobre o formato do manifest ou do índice, os tipos e validadores desta
engine definem o comportamento executável; em caso de divergência sobre o
protocolo `screen-v1` em si, o repositório `lealing-sdk` é quem define. Este
guia precisa ser corrigido junto com qualquer mudança pública em qualquer um
dos dois repositórios.

## 1. O que é uma tool

Uma tool é um executável externo instalado junto com um `manifest.yaml`. Ela
roda em outro processo, mas desenha apenas a área central da TUI; topbar,
breadcrumb, statusbar, confirmação global, sanitização e ciclo de vida
pertencem à engine.

```text
engine                         processo da tool
──────                         ─────────────────
manifest e catálogo
spawn ───────────────────────▶ main
initialize ──────────────────▶ screen.Run
                               Factory
                               Model.Init/Update/View
ui/event ────────────────────▶ Update
ui/snapshot ◀──────────────── View + hints/status/cursor
host/request ────────────────▶ engine executa ação negociada
host/response ◀────────────── resultado ou erro
shutdown ────────────────────▶ encerramento gracioso
```

A engine não importa o módulo da tool, não registra factory por ID e não
inicia o executável durante descoberta, busca ou validação.

## 2. Estrutura recomendada

Um repositório pode publicar uma ou várias tools. Cada tool tem um executável
e um manifest próprios:

```text
go.mod
cmd/
  example-tool/
    main.go
internal/
  exampletool/
    domain/          tipos e regras puras
    adapter/         disco, HTTP, processos e parsers
    ui/              model Bubble Tea
manifests/
  example-tool.yaml
dist/
  darwin-arm64/
    manifest.yaml
    lealing-tool-example
  windows-amd64/
    manifest.yaml
    lealing-tool-example.exe
```

O módulo importa somente APIs públicas do SDK independente:

```go
require github.com/mateuslh/lealing-sdk vX.Y.Z
```

Imports permitidos:

```go
github.com/mateuslh/lealing-sdk/protocol
github.com/mateuslh/lealing-sdk/screen
github.com/mateuslh/lealing-sdk/component
github.com/mateuslh/lealing-sdk/machine
```

Nunca importe `github.com/mateuslh/lealing/internal`. Pacotes `internal` não
são contrato e Go recusará seu uso fora do módulo da engine. Note que o
módulo da tool não precisa (e normalmente não deve) depender do módulo da
engine em nenhum ponto — só do SDK. A engine só entra em cena em tempo de
desenvolvimento, como binário externo chamado via `go run` para validar o
manifest (`cmd/lealing -tool-validate`) e instalar localmente
(`cmd/lealing -tool-install`).

Fixe uma versão do SDK no `go.mod`. Atualize-a deliberadamente depois de ler
o changelog da tag nova; não dependa de uma branch mutável para publicar.

## 3. Manifest

O manifest é YAML estrito: campo desconhecido falha. O limite é 1 MiB e
validá-lo nunca inicia o executável.

Exemplo completo:

```yaml
apiVersion: lealing.dev/v1
id: example-tool
version: 0.1.0
name: Example Tool
summary: Demonstra uma extensão externa.
detail: |
  Explica o objetivo, a procedência dos dados e qualquer efeito colateral.
category: utilities
risk: safe
glyph: ◇
keywords: [exemplo, demonstração]
tags: [produtividade]
runtime:
  kind: process
  protocol: {min: 1, max: 1}
  executable: lealing-tool-example
ui:
  mode: screen-v1
  capabilities: [navigation.back, notification.show]
  wantsMouse: false
platforms:
  - darwin-arm64
  - windows-amd64
requirements:
  - executable: example-cli
    name: Example CLI
    installHint: Instale a Example CLI e autentique a sessão.
permissions:
  filesystem:
    read: [~/.example/input]
    write: [~/.example/output]
  network: false
  subprocess: true
```

Valide um arquivo ou um diretório de pacote:

```sh
lealing -tool-validate ./manifests/example-tool.yaml
lealing -tool-validate ./dist/darwin-arm64
```

### 3.1 Campos

| Campo | Contrato |
|---|---|
| `apiVersion` | Exatamente `lealing.dev/v1`. |
| `id` | Permanente; minúsculas, números, hífen ou `/`. Para publicar em marketplace, use o subconjunto sem `/`: `^[a-z0-9]+(?:-[a-z0-9]+)*$`. |
| `version` | SemVer. Use `X.Y.Z` para também atender ao índice do marketplace. |
| `name` | Nome humano, sem controles de terminal. |
| `summary` | Uma linha e ponto final obrigatório. |
| `detail` | Texto multilinha opcional, sem controles de terminal. |
| `category` | `system`, `ai`, `network`, `media`, `dev` ou `utilities`. |
| `risk` | `safe`, `caution` ou `destructive`. |
| `glyph` | Glyph opcional e seguro para terminal. |
| `keywords`, `tags` | Termos opcionais de busca, sem controles. |
| `runtime.kind` | Na v1, exatamente `process`. |
| `runtime.protocol` | Intervalo inclusivo, positivo e não vazio. |
| `runtime.executable` | Nome simples, relativo, sem diretório, espaço ou argumento. |
| `ui.mode` | Na v1, exatamente `screen-v1`. |
| `ui.capabilities` | Ações do host que a tool pode pedir. |
| `ui.wantsMouse` | Opcional, padrão `false`. Veja §5.2. |
| `platforms` | Lista não vazia de alvos publicados. |
| `requirements` | Executáveis obrigatórios encontrados no `PATH`. |
| `permissions` | Acesso real a disco, rede e subprocessos. |
| `permissions.workingDir` | Opcional: `read` ou `write` sobre o diretório de onde a engine foi aberta. Veja §3.5. |

Alvos reconhecidos:

```text
darwin-amd64   darwin-arm64
windows-amd64  windows-arm64
linux-amd64    linux-arm64
```

No Windows, a engine acrescenta `.exe` ao nome de `runtime.executable` quando
o manifest não declara extensão. Não publique caminho nem argumento nesse
campo.

### 3.2 Risco

| Valor | Use quando | Efeito na engine |
|---|---|---|
| `safe` | Somente leitura ou operação trivialmente reversível. | Sem confirmação global de abertura. |
| `caution` | Escreve estado local recuperável. | Sinalização de cautela. |
| `destructive` | Apaga dados ou altera ambiente externo. | Sinalização forte e confirmação global. |

Classifique pelo pior efeito possível, não pelo caminho mais comum. A
confirmação de abertura não substitui confirmações específicas dentro do
fluxo; para elas use `confirmation.request`.

### 3.3 Requisitos

Cada requisito contém o nome exato procurado no `PATH`:

```yaml
requirements:
  - executable: example-cli
    name: Example CLI
    installHint: Instale a Example CLI e execute o login.
```

`executable` não aceita caminho, argumento nem shell. Requisito ausente abre o
diagnóstico genérico da engine. A tool não instala dependências por conta
própria.

### 3.4 Permissões

Caminhos de manifest precisam ser absolutos ou começar por `~/`. Travessia
com `..`, NUL e controles são recusados.

```yaml
permissions:
  filesystem:
    read: [~/.example/input]
    write: [~/.example/output]
  network: true
  subprocess: false
```

Declare somente o que o binário realmente usa. `DataDir` e `CacheDir` são
diretórios privados negociados para a tool e podem ser lidos e escritos por
meio de `lealing-sdk/machine` sem repeti-los no manifest.

Na v1, permissões são apresentadas ao usuário e ajudam uma tool bem-comportada
a aplicar o contrato. Elas não prometem sandbox do sistema operacional. O SDK
confere arquivos e subprocessos antes da operação; rede continua sendo uma
declaração de confiança, não isolamento técnico.

### 3.5 Diretório de trabalho

O processo da tool roda com o diretório de instalação como diretório corrente;
`os.Getwd` não devolve onde o usuário está trabalhando. Uma tool que precisa
reagir ao projeto aberto declara a intenção no manifest:

```yaml
permissions:
  workingDir: read     # ou write
```

Só com essa declaração a engine envia `workingDir` no handshake e concede o
caminho ao `lealing-sdk/machine`. `read` permite ler a árvore; `write` também autoriza
escrever nela. O caminho não aparece no manifest porque só existe em tempo de
execução — quem valida a instalação vê o nível pedido, e a ficha da tool no
marketplace mostra `dir. atual: leitura` ou `leitura e escrita`.

```go
environment := machine.NewEnvironment(session.Initialize)
if !environment.CanUseWorkingDir() {
    return nil, errors.New("abra o lealing de dentro do projeto")
}
pom, err := environment.WorkingPath("pom.xml")
```

`CanUseWorkingDir` é obrigatório antes de tocar o disco: engines anteriores ao
campo e instalações sem a permissão devolvem o valor vazio, e uma tool que
assume o contrário quebra na máquina do usuário, não na sua.

No índice do marketplace, `permissions.workingDir` é opcional. Um índice que
não declara o campo continua instalando normalmente — ferramentas de publicação
anteriores a ele seguem válidas — e a engine só recusa quando o índice declara
um nível diferente do que o pacote traz. Um índice omisso apenas não exibe a
linha na ficha da tool.

## 4. Executável e `screen.Run`

`stdin` e `stdout` pertencem exclusivamente ao protocolo. Logs vão para
`stderr`.

```go
package main

import (
    "context"
    "fmt"
    "os"
    "os/signal"

    "github.com/mateuslh/lealing-sdk/machine"
    "github.com/mateuslh/lealing-sdk/protocol"
    "github.com/mateuslh/lealing-sdk/screen"
)

var version = "dev"

func factory(session screen.Session) (screen.Model, error) {
    environment := machine.NewEnvironment(session.Initialize)
    if !environment.Platform.Valid() {
        return nil, &machine.PlatformError{Platform: environment.Platform}
    }
    return newModel(session, environment), nil
}

func main() {
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
    defer stop()

    err := screen.Run(ctx, screen.Config{
        ToolVersion: version,
        Protocol: protocol.VersionRange{Min: 1, Max: 1},
        Capabilities: []string{
            protocol.CapabilityNavigationBack,
            protocol.CapabilityNotificationShow,
        },
        FactoryWithError: factory,
    })
    if err != nil {
        fmt.Fprintln(os.Stderr, "example-tool:", err)
        os.Exit(1)
    }
}
```

Use `FactoryWithError` quando composição, plataforma ou configuração puderem
falhar. Nunca devolva model `nil` nem use `panic` para plataforma sem adapter.

A lista em `screen.Config.Capabilities` é o que o executável suporta pedir. A
lista do manifest é o que a instalação solicita à engine. Só a interseção
negociada chega em `screen.Session.Capabilities`.

## 5. Model Bubble Tea

O contrato mínimo é:

```go
type Model interface {
    Init() tea.Cmd
    Update(tea.Msg) (screen.Model, tea.Cmd)
    View(protocol.Frame) string
}
```

Interfaces opcionais:

| Interface | Snapshot produzido |
|---|---|
| `screen.Hinter` | Atalhos na statusbar. |
| `screen.Statuser` | Estado textual à direita. |
| `screen.Capturer` | Informa que teclas globais, como `q`, pertencem ao campo focado. |
| `screen.CursorProvider` | Cursor relativo ao `Body`. |

Exemplo mínimo:

```go
type model struct {
    theme *component.Theme
    text  string
}

func newModel(session screen.Session, _ machine.Environment) screen.Model {
    return &model{
        theme: component.ThemeFrom(session.Initialize.Theme),
        text:  "pronto",
    }
}

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Update(message tea.Msg) (screen.Model, tea.Cmd) {
    switch message := message.(type) {
    case tea.KeyMsg:
        if message.String() == "r" {
            return m, m.load()
        }
    case loadedMsg:
        m.text = message.text
    case screen.ThemeChangedMsg:
        m.theme = component.ThemeFrom(message.Theme)
    }
    return m, nil
}

func (m *model) View(frame protocol.Frame) string {
    return component.Panel{
        Title: "exemplo", Glyph: "◇", Accent: m.theme.Accent,
        Width: frame.Width, Height: frame.Height,
    }.Render(m.theme, m.theme.Body.Render(m.text))
}

func (m *model) Hints() []protocol.Hint {
    return []protocol.Hint{{Key: "r", Label: "recarregar"}, {Key: "esc", Label: "voltar"}}
}
```

### 5.1 I/O assíncrono

`Update` e `View` são funções de estado e render. Arquivo, HTTP, processo,
cache e operações lentas rodam em `tea.Cmd`, com contexto e timeout:

```go
type loadedMsg struct {
    text string
    err  error
}

func (m *model) load() tea.Cmd {
    reader := m.reader
    return func() tea.Msg {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        text, err := reader.Read(ctx)
        return loadedMsg{text: text, err: err}
    }
}
```

O comando devolve uma mensagem; `Update` altera o estado; o SDK envia um novo
snapshot completo. A v1 não usa diff por célula.

### 5.2 Eventos entregues pelo SDK

O SDK normaliza eventos em mensagens Bubble Tea:

- teclado, paste, mouse e resize;
- `tea.FocusMsg` e `tea.BlurMsg`;
- `screen.ThemeChangedMsg`;
- `screen.TickMsg`;
- `screen.ShutdownMsg`.

Ao receber `ShutdownMsg`, atualize apenas estado necessário para encerramento.
Persistência lenta continua precisando de estratégia explícita; não bloqueie o
loop indefinidamente.

Eventos de mouse só chegam se o manifest declarar `ui.wantsMouse: true`. Sem
essa declaração — o padrão —, a engine não captura o mouse do terminal
enquanto a tool roda, e o usuário pode selecionar texto da tela normalmente,
como em qualquer programa de terminal. Só peça `wantsMouse` se a tool
realmente interpreta clique, arraste ou roda; caso contrário você troca a
seleção de texto do usuário por eventos que nunca são lidos.

## 6. Plataforma, arquivos e processos

Crie o ambiente a partir do handshake:

```go
environment := machine.NewEnvironment(session.Initialize)
```

Não use `runtime.GOOS`, `runtime.GOARCH` ou `os.UserHomeDir` para escolher
adapter. A engine já negociou plataforma, arquitetura, home e diretórios.

Seleção explícita:

```go
adapter, err := machine.Select(environment.Platform, map[machine.Platform]func() Reader{
    machine.Darwin:  func() Reader { return newDarwinReader(environment) },
    machine.Windows: func() Reader { return newWindowsReader(environment) },
    machine.Linux:   func() Reader { return newLinuxReader(environment) },
})
```

Não faça fallback para adapter de outro sistema. Pacotes por plataforma podem
compilar sem build tags quando o específico é somente o processo disparado;
isso permite testar parsers de todos os sistemas na mesma CI.

### 6.1 Arquivos

```go
files := environment.Files()
raw, err := files.ReadFile("~/.example/input/state.json")

statePath, err := environment.DataPath("state.json")
if err == nil {
    err = files.WriteFileAtomic(statePath, raw, 0o600)
}
```

APIs públicas: `ResolveRead`, `ResolveWrite`, `CanRead`, `CanWrite`,
`DataPath`, `CachePath`, `CanUseWorkingDir`, `WorkingPath`, `Open`, `ReadFile`,
`Stat`, `ReadDir`, `WalkDir`, `MkdirAll` e `WriteFileAtomic`.

Não toque o disco antes de resolver a concessão. Prefira escrita atômica para
estado persistente.

### 6.2 Subprocessos

```go
ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()

out, err := environment.Executor().OutputText(ctx,
    "example-cli", "inspect", "--format", "json")
```

O executor recusa a operação quando `permissions.subprocess` é falso. Nome e
argumentos permanecem tokens separados; nunca use `sh -c`, `cmd /c` ou
interpole uma linha de shell. `WithDir` e `WithEnv` devolvem cópias
configuradas do executor.

## 7. Componentes e geometria

Materialize sempre o tema recebido:

```go
theme := component.ThemeFrom(session.Initialize.Theme)
```

Use `lealing-sdk/component` para painéis, alinhamento, truncamento, centralização,
meters, barras e sparklines. Não copie cores internas da engine nem fixe uma
paleta como única opção; `component.DefaultTheme` é reserva para testes e host
incompleto.

Regras de geometria:

1. `protocol.Frame` já exclui o chrome da engine;
2. nenhuma linha pode exceder `frame.Width`;
3. o corpo não pode exceder `frame.Height`;
4. meça Unicode e ANSI com `lipgloss.Width`, nunca `len`;
5. `lipgloss.Width(n)` dimensiona conteúdo, não borda e padding;
6. `lipgloss.Place` posiciona, mas não recorta; use `component.Center`;
7. aplique `MaxWidth`/`MaxHeight` no mesmo estilo que adiciona padding;
8. tabs, scroll, modais, vazio, loading e erro precisam caber nos mesmos
   limites.

O `Body` aceita Unicode, quebras de linha e SGR conhecido para cor, bold,
italic, underline e reset. A engine remove OSC, clipboard, título, clear,
cursor global, alt-screen, mudança de modo, controles e sequências
desconhecidas. Hints, status e texto de modal perdem todo ANSI.

## 8. Ações do host

A tool solicita ações com `screen.Request`. A capability precisa aparecer no
manifest, no `screen.Config` e na negociação da sessão.

| Capability | Params | Resultado relevante |
|---|---|---|
| `navigation.back` | Objeto vazio ou `nil`. | `{"ok":true}` e retorno à tela anterior. |
| `notification.show` | `protocol.NotificationParams{Message: ...}`. | Notificação curta; `Message` é obrigatório. |
| `clipboard.write` | `protocol.ClipboardParams{Text: ...}`. | Até 1 MiB; pode não existir na plataforma. |
| `confirmation.request` | `protocol.ConfirmationParams`. | `protocol.ConfirmationResult`. |
| `browser.open` | `protocol.BrowserParams{URL: ...}`. | Somente URL HTTP/HTTPS válida. |

Exemplo:

```go
return m, screen.Request(
    protocol.CapabilityConfirmationRequest,
    protocol.ConfirmationParams{
        Title: "Confirmar operação",
        Message: "Deseja continuar?",
        ConfirmLabel: "continuar",
        CancelLabel: "cancelar",
    },
)
```

O resultado chega em `screen.HostResponseMsg`. Confira `Error` antes de
decodificar `Result`. Request não negociada volta ao model com
`capability_denied` e não sai para o host.

Confirmações destrutivas pertencem à engine. Não desenhe um modal que imite a
confirmação global e depois execute a ação sem `confirmation.request`.

## 9. Protocolo `screen-v1`

Autores em Go devem usar `lealing-sdk/screen`; não reimplementem o fio. Para
outro runtime, `lealing-sdk/protocol` é a especificação executável.

Invariantes da v1:

- framing `Content-Length: N\r\n\r\n<json>`;
- mensagens com `version`, `sequence`, `method` e `payload`;
- sequência começa em 1 e cresce monotonicamente em cada direção;
- handshake começa por `initialize` e termina em `initialized`;
- versão negociada é a maior interseção entre os intervalos;
- incompatibilidade responde `initialized` com estado `incompatible` antes de
  encerrar;
- limite padrão de 8 MiB por mensagem;
- frame limitado a 500×200;
- snapshot é completo e tem sequência própria;
- request de host usa ID correlacionado com response;
- método, versão ou payload inválido encerra a sessão com erro explícito;
- `shutdown` oferece encerramento gracioso.

`stdout` não aceita banner, log ou `fmt.Println`. Qualquer byte fora do framing
corrompe a sessão. Use `stderr` para logs.

## 10. Testes obrigatórios

Uma tool precisa cobrir, proporcionalmente ao risco:

### 10.1 Domínio e adapters

- regras e cálculos puros;
- serviço com fakes das dependências;
- parser com amostra fixa do formato real;
- campo ausente degradando para valor padrão quando a leitura é best-effort;
- contexto cancelado e timeout;
- nenhuma rede, credencial, home ou executável real na suíte normal.

### 10.2 Model

- loading, sucesso, vazio e erro;
- todas as teclas anunciadas;
- tabs, scroll, modal e foco;
- `Capturing()` enquanto entrada de texto estiver focada;
- mudança de tema e resize;
- shutdown;
- I/O somente dentro de comandos.

Renderize pelo menos estes nove frames:

```go
var frames = []protocol.Frame{
    {Width: 200, Height: 60}, {Width: 150, Height: 42},
    {Width: 120, Height: 36}, {Width: 100, Height: 30},
    {Width: 84, Height: 26}, {Width: 70, Height: 22},
    {Width: 50, Height: 16}, {Width: 34, Height: 12},
    {Width: 26, Height: 8},
}
```

Para cada estado e interação relevante, verifique quantidade de linhas e
`lipgloss.Width` de cada linha.

### 10.3 Conformidade do processo

Inicie `screen.Run` com `io.Pipe` e use `protocol.Encoder`/`Decoder` como host
de teste. Cubra:

- `initialize` → `initialized` com snapshot inicial;
- `Init` assíncrono produzindo novo snapshot;
- evento de teclado, paste, mouse e resize;
- capability permitida e recusada;
- intervalo incompatível mostrando os dois lados;
- sequência repetida ou fora de ordem;
- shutdown e cancelamento;
- stdout sem bytes estranhos;
- factory com erro e model nil;
- payload hostil e mensagem acima do limite.

O harness canônico está em `screen/screen_test.go` e framing parcial e
limites estão em `protocol/protocol_test.go`, ambos no repositório
`github.com/mateuslh/lealing-sdk`. Esses testes são a referência, não uma
suíte copiada de outro repositório.

### 10.4 Segurança visual

Teste payloads com OSC de título/clipboard, clear, cursor, alt-screen, modo,
CSI desconhecido, controles C0/C1, Unicode e SGR válido. A allowlist executável
está em `internal/adapter/inbound/tui/sanitize/ansi.go`.

Pacotes com goroutines, canais, pipes, cache ou escrita concorrente rodam com:

```sh
go test -race ./...
```

Não use sleeps exatos para sincronizar teste. Use canais, contextos e helpers
de processo herméticos.

## 11. Build e pacote local

Compile cada alvo declarado, preferencialmente sem CGO:

```sh
CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 \
  go build -trimpath -o dist/darwin-arm64/lealing-tool-example ./cmd/example-tool

cp manifests/example-tool.yaml dist/darwin-arm64/manifest.yaml
```

No Windows, gere `lealing-tool-example.exe`. Cada diretório de pacote contém
somente o necessário, com `manifest.yaml` e executável na raiz:

```text
dist/darwin-arm64/
  manifest.yaml
  lealing-tool-example
```

Valide e instale sem executar durante a validação:

```sh
lealing -tool-validate ./dist/darwin-arm64
lealing -tool-install ./dist/darwin-arm64
lealing -tools
lealing -platforms
```

Uma versão posterior preserva a anterior para rollback:

```sh
lealing -tool-update ./dist/darwin-arm64
lealing -tool-rollback example-tool
lealing -tool-disable example-tool
lealing -tool-enable example-tool
lealing -tool-remove example-tool
```

`-tool-remove` move a instalação para `.trash` e informa o caminho
recuperável. `-tool-checksum` em instalação local confere o SHA-256 do
executável; no marketplace remoto, o checksum do índice confere o pacote
compactado.

## 12. Origem local para desenvolvimento

Uma origem local permite testar o mesmo fluxo do marketplace sem publicar.
Crie `index.json` na raiz do repositório:

```json
{
  "apiVersion": "lealing.dev/marketplace/v1",
  "tools": [{
    "id": "example-tool",
    "version": "0.1.0",
    "name": "Example Tool",
    "summary": "Demonstra uma extensão externa.",
    "detail": "Build local para desenvolvimento.",
    "category": "utilities",
    "risk": "safe",
    "publisher": "example",
    "channel": "community",
    "protocol": {"min": 1, "max": 1},
    "minimumEngine": "0.1.0",
    "permissions": {
      "filesystem": {"read": [], "write": []},
      "network": false,
      "subprocess": false
    },
    "artifacts": [{
      "platform": "darwin-arm64",
      "url": "dist/darwin-arm64"
    }]
  }]
}
```

Cadastre um caminho absoluto:

```sh
lealing -source-add /caminho/absoluto/para/o/repositorio \
  -source-name example-local
lealing -marketplace
lealing -tool-install example-local/example-tool
```

Remover uma origem desinstala em cascata as tools instaladas por ela. A troca
do conjunto é atômica e o estado anterior permanece num diretório de
recuperação; desabilitar a origem apenas impede novas consultas ao índice.

Em origem local, `artifact.url` é um diretório relativo ao `index.json`, não
pode subir com `..` nem escapar por symlink e precisa omitir checksum. O
manifest do diretório continua sendo revalidado na instalação.

## 13. Marketplace remoto e publicação independente

Publicar não exige aprovação nem uso da origem padrão. Hospede por HTTPS:

1. um `index.json` compatível;
2. o manifest de cada versão;
3. um `.zip`, `.tar.gz` ou `.tgz` por alvo;
4. o SHA-256 de cada pacote.

O pacote compactado contém `manifest.yaml` e o executável na raiz. A engine
recusa formato desconhecido, checksum divergente, arquivo grande demais,
travessia, symlink e divergência entre índice e manifest.

Entrada remota:

```json
{
  "apiVersion": "lealing.dev/marketplace/v1",
  "tools": [{
    "id": "example-tool",
    "version": "0.1.0",
    "name": "Example Tool",
    "summary": "Demonstra uma extensão externa.",
    "detail": "Descrição longa exibida antes da instalação.",
    "category": "utilities",
    "risk": "safe",
    "publisher": "example",
    "channel": "community",
    "manifestUrl": "https://tools.example/0.1.0/manifest.yaml",
    "protocol": {"min": 1, "max": 1},
    "minimumEngine": "0.1.0",
    "permissions": {
      "filesystem": {"read": [], "write": []},
      "network": false,
      "subprocess": false
    },
    "artifacts": [{
      "platform": "darwin-arm64",
      "url": "https://tools.example/0.1.0/example-tool-darwin-arm64.tar.gz",
      "sha256": "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
    }]
  }]
}
```

Regras do índice:

- `apiVersion` é `lealing.dev/marketplace/v1`;
- ID usa letras minúsculas, números e hífens;
- versão e `minimumEngine` usam SemVer sem prefixo `v`;
- `summary` tem uma linha e ponto final;
- `publisher`, canal, protocolo, permissões e ao menos um artefato são
  obrigatórios;
- `manifestUrl` e URLs de artefato remoto usam HTTPS;
- cada plataforma aparece no máximo uma vez por entrada;
- pacote remoto exige SHA-256 hexadecimal de 64 caracteres;
- `id@version` não se repete no mesmo índice.

Origens adicionadas pelo usuário são tratadas como `community`, mesmo que o
JSON declare `official` ou `verified`. Confiança é política da engine, não do
publicador. Em conflito de ID, prioridade da origem vence versão; o usuário
ainda pode escolher explicitamente `origem/id`.

O cliente não depende de GitHub. Qualquer hospedagem HTTPS que sirva o índice,
manifest e pacotes atende ao contrato.

## 14. Versionamento e compatibilidade

Tool, engine e protocolo têm versões independentes:

- mudança compatível da tool incrementa sua SemVer;
- `minimumEngine` sobe somente quando a tool usa contrato ausente em engines
  anteriores;
- mudança aditiva no fio preserva o intervalo quando versões antigas ainda
  funcionam;
- quebra do fio exige nova versão de protocolo;
- o manifest declara o intervalo realmente suportado, nunca apenas a versão
  usada durante o desenvolvimento.

Teste incompatibilidade sem iniciar recursos da tool. A mensagem deve mostrar
o intervalo da engine e o da tool.

## 15. Definição de pronto

Antes de publicar, confirme:

- o módulo importa somente `github.com/mateuslh/lealing-sdk/*`, nunca `internal/*` da engine?
- ID e SemVer são estáveis e publicáveis?
- manifest e binário estão na raiz de cada pacote?
- plataformas declaradas possuem artefato real?
- plataforma vem do handshake e não de `runtime.GOOS`?
- permissões, requisitos e risco descrevem o comportamento real?
- arquivo, rede e processo rodam somente em `tea.Cmd` com contexto?
- subprocessos usam tokens separados e nenhum shell?
- stdout contém somente protocolo e logs vão para stderr?
- hints incluem saída visível e `Capturing` acompanha o foco?
- nove geometrias, vazio, loading, erro, scroll e modal passaram?
- protocolo, incompatibilidade, shutdown e payload hostil têm testes?
- `go fmt`, `go vet`, `go test`, `go test -race` e builds cruzados passaram?
- pacotes e `index.json` foram validados antes da publicação?

Depois de publicado, instale pela URL da origem numa máquina limpa e confira
manifest, permissões, checksum, abertura, atualização e rollback.
