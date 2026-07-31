# lealing

Centro de comando de tools no terminal. Uma TUI em Go + Bubble Tea desenhada
para acomodar centenas de ferramentas sem virar um menu interminável.

O lealing é dividido em **engine** e **tools instaláveis**. A engine é dona do
terminal, home, busca, favoritos, chrome, segurança, instalação e processos.
Uma tool `screen-v1` roda em outro processo, mantém domínio, adapters e estado
visual próprios e envia somente snapshots JSON versionados. Assim ela pode ser
instalada ou atualizada sem publicar outra versão da engine — e um crash da
tool não derruba a TUI.

```
██╗     ███████╗ █████╗ ██╗     ██╗███╗   ██╗ ██████╗
██║     ██╔════╝██╔══██╗██║     ██║████╗  ██║██╔════╝
██║     █████╗  ███████║██║     ██║██╔██╗ ██║██║  ███╗
██║     ██╔══╝  ██╔══██║██║     ██║██║╚██╗██║██║   ██║
███████╗███████╗██║  ██║███████╗██║██║ ╚████║╚██████╔╝
╚══════╝╚══════╝╚═╝  ╚═╝╚══════╝╚═╝╚═╝  ╚═══╝ ╚═════╝
```

## Instalação

**macOS e Linux, sem Go instalado:**

```sh
curl -fsSL https://raw.githubusercontent.com/mateuslh/lealing/main/scripts/install.sh | sh
```

O script detecta sistema e arquitetura, baixa o binário da última release,
**confere o sha256 contra o `checksums.txt` publicado** e instala em
`~/.local/bin/lealing`. Nada é instalado se o checksum não bater.

**Ele também põe o diretório no seu PATH**, escrevendo uma linha no perfil do
shell (`~/.zshrc`, `~/.bash_profile`, `~/.bashrc` ou `~/.profile`, conforme o
seu shell) — só quando o diretório ainda não está lá, e a linha é idempotente,
então rodar o instalador de novo não duplica nada. Num terminal novo, `lealing`
já funciona; no que está aberto, aplique com
`export PATH="$HOME/.local/bin:$PATH"`.

Variáveis: `LEALING_BIN_DIR` escolhe o destino e `LEALING_NO_PATH=1` deixa o
seu perfil em paz. Sem informar versão, o instalador sempre consulta e baixa
a última release disponível. `LEALING_VERSION` existe apenas para quem quiser
fixar deliberadamente uma tag antiga.

```sh
LEALING_BIN_DIR=/usr/local/bin sh install.sh
```

**Windows 10+ com PowerShell:**

```powershell
irm https://raw.githubusercontent.com/mateuslh/lealing/main/scripts/install.ps1 | iex
```

O instalador detecta `amd64` ou `arm64`, confere o mesmo `checksums.txt`,
instala em `%LOCALAPPDATA%\lealing\bin` e acrescenta o diretório ao `PATH` do
usuário. Sem informar versão, ele também instala sempre a última release.

**Com Go instalado**, sem passar por release nenhuma:

```sh
go install github.com/mateuslh/lealing/cmd/lealing@latest
```

O binário vai para `$(go env GOPATH)/bin`. A versão fica como `dev` — o
`go install` não injeta a tag —, e a tool de atualização trata esse caso.

**Para desenvolver**, do clone:

```sh
make install
```

Instala um wrapper em `~/.local/bin/lealing` que aponta para este
repositório. A partir daí, basta digitar `lealing` em qualquer lugar.

**O wrapper recompila sozinho.** Ele compara o mtime dos fontes com o do
binário e roda `go build` quando algo mudou — então editar o código e chamar
`lealing` já usa a versão nova, sem passo manual. Quando nada mudou, o custo
é uma comparação de arquivos. Se um build quebrar, ele mantém o binário
anterior em vez de deixar você sem ferramenta.

Mover o repositório de lugar exige rodar `make install` de novo: o wrapper
guarda o caminho absoluto. Para desinstalar, `make uninstall`.

### Tools externas

Abra **Marketplace de Tools** na própria home ou use a CLI. A listagem remota
filtra protocolo, versão mínima da engine e plataforma antes de oferecer a
instalação:

```sh
lealing -marketplace
lealing -tool-install token-usage
lealing -tool-update token-usage
lealing -tools
```

O índice só é consultado ao abrir o marketplace ou ao executar um desses
comandos; startup e busca local não fazem rede nem iniciam processos. O pacote
é baixado para cache temporário com limite de tamanho, tem o SHA-256 conferido
antes da extração, recusa caminhos externos e links e é revalidado contra o ID
e a versão escolhidos antes da troca atômica.

O índice público consolidado fica em
[`lealing-tools/marketplace/index.json`](https://github.com/mateuslh/lealing-tools/blob/main/marketplace/index.json).
Para testar outro registry compatível, use `-marketplace-url URL_HTTPS`.

Liste somente o que já está instalado:

```sh
lealing -tools
```

Um pacote local é um diretório com `manifest.yaml` e o executável da
plataforma. Validar e instalar não executa a tool:

```sh
lealing -tool-validate ./pacote/manifest.yaml
lealing -tool-install ./pacote
```

Uma atualização local usa o mesmo formato e preserva a versão anterior:

```sh
lealing -tool-update ./pacote-novo
lealing -tool-rollback token-usage
lealing -tool-remove token-usage
```

A vertical oficial vive em
[`mateuslh/lealing-tools`](https://github.com/mateuslh/lealing-tools). Autores
independentes podem hospedar seus próprios releases e enviar uma entrada pelo
[guia de publicação](https://github.com/mateuslh/lealing-tools/blob/main/marketplace/README.md).
A engine abre normalmente sem nenhuma tool externa; uma ausente não vira item
quebrado.

Instalações ficam em
`~/.local/share/lealing/tools/<id>/<version>/` no macOS/Linux e em
`%LOCALAPPDATA%\lealing\tools\<id>\<version>\` no Windows. `active` aponta
para a versão atual, `previous` guarda o rollback e remoções recuperáveis vão
para `.trash`. `XDG_DATA_HOME` continua tendo prioridade quando definido.

Manifest, checksums e índice do marketplace usam a
[última release disponível](https://github.com/mateuslh/lealing-tools/releases/latest),
nunca um link de documentação preso a uma versão numérica.

## Atualização

A tool **Atualizar o lealing** faz o trabalho de dentro da TUI, e o mesmo
caminho existe fora dela:

```sh
lealing -update
```

Ela primeiro descobre **como este binário chegou aqui**, e é isso que decide o
que acontece:

| Origem | O que a atualização faz |
|---|---|
| Binário de release | Baixa o artefato da plataforma, confere o `checksums.txt` e troca o executável em disco |
| Clone do repositório | `git pull --ff-only` e recompila; o binário anterior fica de pé se o build falhar |
| Origem desconhecida | Não mexe em nada — mostra a versão publicada e como reinstalar |

Três garantias que valem ser conhecidas: **nada é instalado sem o checksum
bater**; o `--ff-only` nunca faz merge automático em um clone com trabalho
local; e a troca do executável é um rename no mesmo volume, então ou você fica
com o binário novo, ou com o antigo — nunca com um pela metade.

O trabalho normal termina em editar, commitar e dar push. Quando for hora de
publicar, um mantenedor ou agente apenas solicita a versão:

```sh
make release VERSION=vX.Y.Z
```

Esse alvo só dispara o
[workflow de release](.github/workflows/release.yml). Dentro da pipeline, o
projeto é validado e empacotado primeiro; só depois ela cria a tag anotada,
compila macOS, Windows e Linux (amd64 e arm64), gera o `checksums.txt` e cria
a release. O clone local não cria tag nem faz um commit especial de release.

`VERSION` é obrigatório e precisa seguir `vX.Y.Z`. Tag já usada, formatação
pendente, teste quebrado ou falha de compilação interrompem a publicação.

## Tools

| Tool | O que faz |
|---|---|
| **Informações do Sistema** | Sistema, chip, memória, tempo ligado e bateria. Somente leitura. |
| **Controle de Energia** | Perfis de energia da bateria e do carregador, com presets e aplicação via `pmset` (macOS) ou `powercfg` (Windows). |
| **Uso de Tokens** | Tool externa `screen-v1`: cotas das CLIs de IA, consumo e custo por janela, modelo, projeto e dia. |
| **Contas do Claude Code** | Guarda as sessões de várias contas e alterna entre elas sem refazer login. |
| **Atualizar o lealing** | Compara a versão instalada com o último release e atualiza pelo caminho por onde o lealing foi instalado. |
| **Clone Repo Bradesco** | Descobre a família de um projeto no GitHub, clona os repositórios escolhidos e os registra no IntelliJ. |
| **Radar Git do dev** | Varre os clones em `~/dev`, mostra branches e alterações pendentes e oferece ações Git explícitas. |
| **Bancada de engenharia** | Sonda HTTP, DNS/TLS, JSON, JWT, CIDR, codecs, checksums e UUIDs em telas nativas. |

As cotas das duas CLIs são consultadas **ao vivo na conta**, nas mesmas
rotas que elas próprias usam para desenhar seu `/usage` — Claude Code em
`api.anthropic.com`, Codex em `chatgpt.com/backend-api`. A autenticação
reaproveita a sessão que cada CLI já mantém: o chaveiro do macOS para o
Claude Code, `~/.codex/auth.json` para o Codex. O lealing só **lê** essas
credenciais: não cria, não renova e não grava nenhuma, e o painel diz de
onde veio cada número (`conta · pro`).

Quando a conta não responde, o Codex cai no último `token_count` gravado em
`~/.codex/sessions` — rotulado `log local · visto há 2d`, porque esse número
tem a idade do último uso e a janela pode ter virado desde então. Sem
sessão, o bloco mostra o consumo medido dos logs; com a sessão vencida, a
barra de status pede para rodar `claude` ou `codex`.

**Contas do Claude Code** guarda o par que define quem a CLI acha que você
é: a credencial OAuth, que vive no cofre da plataforma, e o bloco
`oauthAccount` do `~/.claude.json`. Mover só o primeiro deixaria a CLI
autenticada em uma conta e exibindo outra, então a tool trata os dois como
uma coisa só. `s` guarda a sessão atual sob um nome, `↵` devolve a escolhida
ao lugar de onde a CLI lê.

Os tokens ficam onde o sistema sabe protegê-los — itens do chaveiro no
macOS, um arquivo só do dono (`0600`) no Windows e no Linux, ao lado do que a
própria CLI já grava ali. O índice em `~/.local/share/lealing/claude-accounts.json`
tem apenas e-mail, plano e data. Antes de escrever no `~/.claude.json` o
arquivo inteiro é copiado para `claude-json.backup`, e a gravação é atômica:
todo campo que não conhecemos — projetos, histórico, contadores — atravessa
a troca intacto.

Duas proteções valem ser conhecidas: trocar de conta com uma sessão que não
está guardada em nenhum perfil pede confirmação, porque depois da escrita
aquela credencial não existe mais em lugar nenhum; e **feche as sessões do
`claude` antes de trocar** — ao sair, a CLI regrava a conta em que estava.

As tools históricas vieram do [Arteus Tools](../ArteusTools). `token-usage`
foi extraída para
[`lealing-tools`](https://github.com/mateuslh/lealing-tools): a engine não
importa seu domínio, adapters nem model. Para criar outra sem editar o bootstrap, veja
**[AGENTS.md](AGENTS.md#11-criando-uma-tool-externa-screen-v1)**.

## Plataformas

macOS e Windows 10+. Cada tool declara em quais sistemas roda, e o catálogo
esconde as demais: no Windows, uma tool exclusiva do macOS não aparece na
busca nem nas sugestões, em vez de abrir e falhar no primeiro comando.

```sh
lealing -platforms      # a matriz, gerada do catálogo
```

| Tool | macOS | Windows | Como |
|---|:---:|:---:|---|
| Informações do Sistema | ✓ | ✓ | `sysctl`/`sw_vers`/`pmset` · CIM (WMI) |
| Controle de Energia | ✓ | parcial | `pmset` · `powercfg` |
| Uso de Tokens | ✓ | ✓ | lê os logs das CLIs, iguais nos dois |
| Contas do Claude Code | ✓ | ✓ | chaveiro (`security`) · `~/.claude/.credentials.json` |
| Atualizar o lealing | ✓ | ✓ | releases do GitHub · `git` + `go build` |
| Clone Repo Bradesco | ✓ | ✓ | GitHub CLI + Git · recentes do IntelliJ |
| Radar Git do dev | ✓ | ✓ | leitura e ações via Git |
| Bancada de engenharia | ✓ | ✓ | biblioteca padrão e rede |

**Parcial** quer dizer painel menor, não tool quebrada: o `powercfg` grava os
três tempos de inatividade (dormir, tela, disco) e não tem equivalente para
Power Nap, standby, `tcpkeepalive` nem modo de hibernação. O `power.Manager`
declara o que sabe gravar (`Features()`) e a tela desenha só isso — nenhum
interruptor que não chegaria ao sistema, e nenhum "alterações não aplicadas"
que aplicar não resolve. No Windows também não há dispensa de senha a
oferecer: mudar o plano de energia do próprio usuário não pede elevação.

O estado vai para `%LOCALAPPDATA%\lealing` no Windows e `~/.local/share/lealing`
no macOS — em ambos, `XDG_DATA_HOME` tem prioridade se estiver definida.

No Windows, `make install` não se aplica (o wrapper é um shell script que
recompila sozinho): use `make build-windows`, que gera `bin/lealing.exe`, e
ponha o executável onde preferir.

Linux compila e a TUI roda, mas as tools que dependem de adapter nativo ainda
não têm um: elas somem do catálogo até que exista.

## Uso

```sh
lealing                                          # abre a TUI
lealing -render 150x42                           # imprime um frame estático
lealing -render 120x34 -keys '/token[enter]'     # já dentro de uma tool
lealing -update                                  # atualiza e sai
lealing -marketplace                             # tools públicas compatíveis
lealing -tools                                   # tools externas instaladas
lealing -tool-install token-usage                # instala pelo marketplace
lealing -tool-install ./pacote                   # instala/atualiza localmente
lealing -tool-rollback token-usage               # recupera versão anterior
```

Flags: `-debug` (log em arquivo + validação estrita do catálogo),
`-ephemeral` (não persiste favoritos), `-platforms` (matriz de suporte),
`-update` (atualiza a engine sem abrir a TUI), `-marketplace`,
`-marketplace-url`, `-tools`, `-tool-install`, `-tool-update`, `-tool-remove`,
`-tool-rollback`, `-tool-validate` e `-version`.

Sem instalar tools: `make run`. Para renderizar `token-usage`, instale o pacote
extraído da release oficial e use
`make render SIZE=150x42 KEYS='/token[enter]'`.

## Atalhos

| Tecla | Ação |
|---|---|
| `/` ou `ctrl+k` | abre a busca |
| `↑ ↓` / `j k` | move a seleção |
| `← →` / `h l` | troca de painel |
| `tab` | cicla entre painéis |
| `↵` | abre a tool |
| `f` | favorita / desfavorita |
| `r` | recarrega |
| `esc` | volta um nível |
| `?` | ajuda |
| `q` | sai |

A busca aceita filtros inline combináveis com texto livre:
`tag:sistema`, `cat:ai`, `kind:builtin`, `is:fav`.

Dentro de uma tool, a barra de status lista os atalhos daquela tela. No
**Controle de Energia**, `↑ ↓` escolhe o campo, `← →` ajusta o valor e
`⇧← ⇧→` troca entre bateria e carregador — as setas puras editam, porque
mudar valores é o que a tela existe para fazer.

## Arquitetura

Hexagonal (ports & adapters) dentro da engine, com uma fronteira adicional de
processo para tools externas. A TUI genérica chama o caso de uso de sessão; o
caso de uso consome a porta de runtime; o adapter fala `screen-v1` com o
executável. A engine nunca importa o pacote concreto da tool.

```
┌──────────────────────────── ENGINE ─────────────────────────────┐
│ Home/catalog ─▶ PluginScreen ─▶ InteractiveService             │
│                                      │                          │
│                                      ▼                          │
│                              Runtime port                       │
│                                      │                          │
│                         process + JSON framing                  │
│ topbar · breadcrumb · statusbar · ANSI sanitizer · segurança   │
└──────────────────────────────────────┼──────────────────────────┘
                                       │ stdin/stdout screen-v1
┌──────────────────────────────────────▼──────────────────────────┐
│ TOOL: SDK runtime · model · domínio · adapters · persistência   │
└─────────────────────────────────────────────────────────────────┘
```

```
cmd/lealing/                    binário e flags da engine
sdk/
  protocol/                     DTOs + framing; somente biblioteca padrão
  screen/                       adapter Bubble Tea para screen-v1
  component/                    componentes visuais públicos
internal/
  core/
    domain/                     catálogo, runtime declarativo e uso
    interactive/                portas e caso de uso de sessão externa
    toolinstall/ marketplace/   instalação, rollback e índice neutro
    service/                    catálogo, launcher e requisitos
    sysinfo/ power/             núcleos das tools ainda builtin
  adapter/
    inbound/tui/
      screen/home/              home, busca e catálogo consolidado
      screen/plugin/            tela única para todo manifest screen-v1
      sanitize/                 recorte e allowlist ANSI
    outbound/
      externalcatalog/          descoberta lazy, somente por manifest
      pluginprocess/            spawn, handshake, framing e shutdown
      marketplacehttp/         índice, download, checksum e extração segura
      toolstore/                instalação local atômica e rollback
      registry/ search/         consolidação e relevância
      persistence/              favoritos e estatísticas em JSON atômico
      macos/ windows/           adapters das builtins por plataforma
  toolmanifest/                 valida lealing.dev/v1
  architecture/                testes das fronteiras de dependência
  bootstrap/                    único composition root
```

### Por que assim

- **Um núcleo por tool.** `core/power` conhece perfis de energia e nada mais;
  não sabe que `pmset` existe. Trocar o backend é trocar o adapter.
- **Providers independentes.** Cada fonte publica um
  `outbound.ToolProvider`; o `registry` consolida, valida e indexa uma vez
  só. Tool sem nome, ID duplicado ou categoria não declarada falham no
  registro, não na tela.
- **Paginação obrigatória.** `domain.Query` sempre carrega `Offset`/`Limit`.
  A TUI nunca materializa o acervo inteiro.
- **Telas sob demanda.** Builtins continuam no mapa `tui.Screens`. Qualquer
  item `screen-v1` abre a mesma `PluginScreen`, sem factory ou import por ID;
  outros processos ainda usam runners tipados.
- **Grafo acíclico.** O buscador calcula relevância textual; o serviço do
  catálogo combina frequência, recência e favoritos. Nenhum adapter recebe
  uma closure de volta para um caso de uso.
- **Nenhuma porta é chamada em `Update` ou `View`.** Na engine e na tool, todo
  I/O vive em `tea.Cmd`. Spawn, handshake, evento e espera de snapshot também
  são comandos assíncronos.

## Testes

```sh
make test     # suíte completa
make race     # com o detector de corrida
make cross    # compila para macOS, Windows e Linux
make bench    # custo de um frame de render
make cover    # relatório de cobertura
```

A suíte cobre framing parcial, limite de mensagens, handshake, crash,
cancelamento, shutdown, capabilities, manifests, descoberta sem spawn,
instalação/rollback, sanitização ANSI, a PluginScreen genérica, parsers nativos
e as fronteiras de dependência. A **geometria da TUI** renderiza as telas da
engine em nove tamanhos, de 200×60 a 26×8, verificando que nenhuma linha excede
o frame. Domínio, adapters e geometria reais de `token-usage` são testados no
repositório [`lealing-tools`](https://github.com/mateuslh/lealing-tools).

Os adapters de plataforma não têm build tag: o que é específico do sistema é o
processo que eles disparam, não o código Go. Por isso os parsers do Windows
são exercitados na mesma suíte que roda no Mac, com amostras reais de saída —
e `make cross` pega a única quebra que resta, a de um import que só existe de
um lado.

Esse teste de geometria pegou seis bugs reais durante a construção, entre eles
uma fila de cartões transbordando a largura (o `lipgloss.Width` dimensiona o
conteúdo, não o bloco com borda) e painéis saindo pela base em janelas baixas.
Toda tela nova deve entrar nele — veja a seção 6 do **[AGENTS.md](AGENTS.md)**.
