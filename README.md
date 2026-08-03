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
`go install` não injeta a tag —, e o atualizador da engine trata esse caso.

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

A vitrine do marketplace fica na própria home: chegue nela com as setas e
tecle `↵`, ou use `m` de qualquer lugar. Ela não é uma tool do catálogo — é de
onde as tools vêm, então não disputa espaço com o que instala. A listagem
filtra protocolo, versão mínima da engine e plataforma antes de oferecer a
instalação:

```sh
lealing -marketplace
lealing -tool-install example-tool
lealing -tool-update example-tool
lealing -tools
```

Ao abrir a Home, a vitrine consulta as origens em segundo plano; ela nunca
bloqueia a interface. O painel ordena por urgência — o que tem atualização
vem antes da novidade, e o que já está em dia por último — e resume quantas
origens responderam.

Dentro da loja, `→` abre a **ficha** da tool em largura cheia: descrição
longa, procedência, requisitos e a lista dos caminhos que ela vai poder ler e
escrever, um por linha. `⇞ ⇟` rolam, `←` volta à lista e `↵` instala. Ver as
permissões concretas antes de instalar é o ponto: contar "2 leituras" não
deixa ninguém decidir nada.

A tela completa e a CLI reutilizam a mesma porta. Busca local não inicia
processos, e nenhum executável é iniciado durante a descoberta. O pacote
é baixado para cache temporário com limite de tamanho, tem o SHA-256 conferido
antes da extração, recusa caminhos externos e links e é revalidado contra o ID
e a versão escolhidos antes da troca atômica.

O índice público consolidado fica em
[`lealing-tools/marketplace/index.json`](https://github.com/mateuslh/lealing-tools/blob/main/marketplace/index.json).
Esse repositório é apenas o acervo configurado por padrão e um exemplo de uso;
o contrato normativo para criar e publicar tools está no
[guia mantido pela engine](docs/tool-development.md). Para testar outro
registry compatível, use `-marketplace-url URL_HTTPS`.

Liste somente o que já está instalado:

```sh
lealing -tools
```

### Conta e sincronização

Suas preferências podem viver em um repositório privado da sua conta do
GitHub, para que outra máquina chegue configurada:

```sh
lealing -login          # device flow: abre uma página e pede um código
lealing -sync           # o que está aqui e o que está lá
lealing -sync-push      # envia
lealing -sync-pull      # baixa
lealing -logout
```

Três seções, ligadas e desligadas separadamente: **favoritos e uso**,
**origens do marketplace** e **tools instaladas** — desta última só a lista
viaja, porque instalar código de terceiros continua sendo uma decisão sua,
tool a tool, no marketplace.

O `state.json` usa exclusivamente o formato v3. Toda preferência de tool é
qualificada por `host` e `id` (`lealing/token-usage`, por exemplo), então uma
origem paralela que publique o mesmo ID não herda favoritos, histórico nem a
lista de instalações. A leitura também recusa campos desconhecidos,
duplicados, coleções acima dos limites e referências ou versões inválidas.

O que nunca sai da máquina: credenciais. As contas do Claude Code, os tokens
do cofre e qualquer segredo ficam onde estão — repositório privado não é
lugar de segredo.

Enviar e baixar são explícitos, e divergência vira pergunta. Se o repositório
mudou desde a última vez que esta máquina sincronizou, o lealing recusa a
escrita e mostra quem enviou e quando; sobrescrever exige confirmar (ou
`-force` na CLI). Fundir contadores de uso produziria números que nunca
aconteceram, e escolher um lado em silêncio descartaria o trabalho do outro.

O token fica no chaveiro do macOS e, nas outras plataformas, em um arquivo só
do dono no diretório de dados. `lealing -logout` esquece a credencial desta
máquina; para encerrar o acesso de vez, revogue o aplicativo em
`github.com/settings/applications`.

**Para builds próprios:** o device flow usa o OAuth App do lealing, cujo
client_id está no código — ele é público por definição, e o que não existe
neste fluxo é segredo de cliente. Um fork registra o seu em
`github.com/settings/developers`, com *Enable Device Flow* marcado, e informa
por qualquer um dos três caminhos:

```sh
# na tela de configuração (c → Conta → Client ID), que grava em disco
export LEALING_GITHUB_CLIENT_ID=Ov23li…          # ou por ambiente
go build -ldflags "-X …/bootstrap.githubClientID=Ov23li…"   # ou no build
```

A precedência é essa mesma: o que você gravou na tela vence o ambiente, que
vence o valor do build.

### Configuração

`c` abre a configuração da engine, em seções:

| Seção | O que tem |
|---|---|
| **Conta** | Client ID do OAuth App usado no login |
| **Marketplace** | URL do índice padrão e se a home consulta as origens ao abrir |
| **Aparência** | Nome usado na saudação da home |
| **Ambiente** | Versão e os caminhos onde a engine guarda config, dados, cache e tools |

`↑↓` percorre, `→` entra nos ajustes, `↵` edita (ou alterna um interruptor) e
`r` volta ao padrão. Cada campo mostra de onde veio o valor em vigor —
`padrão`, `variável de ambiente` ou `definido por você` —, porque quando algo
não funciona a ação é diferente em cada caso.

Só o que você mudou vai para `~/.config/lealing/settings.json`. Gravar a
configuração inteira congelaria padrões que a engine deve poder melhorar numa
atualização. Ajustes que exigem reabrir o lealing dizem isso na própria linha,
em vez de parecerem sem efeito.

### Repositórios paralelos de tools

O marketplace não é um endereço: é a soma das origens que você habilitou.
Além do índice padrão embutido, dá para registrar quantos repositórios
quiser — um índice publicado por HTTPS ou um diretório no seu disco:

```sh
lealing -source-add https://exemplo.dev/tools/index.json
lealing -source-add /Users/voce/dev/minhas-tools   # repositório em disco
lealing -sources
lealing -source-disable exemplo-dev
lealing -source-remove exemplo-dev
```

O nome é derivado do endereço quando você não passa `-source-name`. Na TUI,
`⇄` abre a aba de origens: `a` cadastra, `espaço` liga e desliga, `d` remove.
As origens ficam em `~/.config/lealing/marketplace-sources.json`
(`%APPDATA%\lealing\` no Windows).

Três regras mantêm a descentralização segura:

- **O canal é da engine, não do índice.** Só a origem embutida publica nos
  canais `official` e `verified`; qualquer outra tem suas entradas rebaixadas
  para `community`, mesmo que o JSON diga o contrário.
- **Conflito de ID é vencido por prioridade, não por versão.** Se um índice
  paralelo publicar `example-tool` numa versão maior, a entrada da origem
  prioritária continua sendo instalada por padrão. Para escolher a outra de
  propósito, use a referência qualificada:
  `lealing -tool-install origem/example-tool`.
- **Uma origem fora do ar não derruba as demais.** Cada índice é buscado e
  validado isoladamente; a falha aparece marcada na aba de origens e o resto
  do catálogo continua utilizável.

Um repositório local é o caminho de quem está desenvolvendo uma tool: o
`index.json` fica no diretório do projeto e cada artefato aponta para a pasta
de build, relativa ao índice. Como o diretório muda a cada `go build`, uma
origem local não declara `sha256` — em troca, ela nunca sai da sua máquina:

```json
{
  "apiVersion": "lealing.dev/marketplace/v1",
  "tools": [{
    "id": "minha-tool", "version": "0.1.0", "name": "Minha Tool",
    "summary": "Tool em desenvolvimento.", "category": "utilities",
    "risk": "safe", "publisher": "voce", "channel": "community",
    "protocol": {"min": 1, "max": 1},
    "permissions": {"filesystem": {"read": [], "write": []}, "network": false, "subprocess": false},
    "artifacts": [{"platform": "darwin-arm64", "url": "dist/darwin-arm64"}]
  }]
}
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
lealing -tool-rollback example-tool
lealing -tool-disable example-tool
lealing -tool-enable example-tool
lealing -tool-remove example-tool
```

Na TUI, abra o marketplace com `m` e vá à aba **gerenciar**. `espaço`
ativa ou desativa a tool selecionada; `d` abre a confirmação de
desinstalação. Desativar mantém o pacote e a versão anterior no disco.
Desinstalar move a instalação para `.trash` e mostra o caminho recuperável.

A engine configura o índice indicado acima como origem padrão. Ele continua
sendo um repositório externo como qualquer outra origem: a engine conhece o
índice, mas não os IDs nem a implementação das tools publicadas. O contrato
para criar uma tool ou hospedar um marketplace independente está no
[guia de desenvolvimento da engine](docs/tool-development.md).

A engine não inclui nenhuma tool. Ela abre com catálogo vazio, descobre apenas
pacotes instalados e nunca transforma a ausência de uma extensão em item
quebrado.

Instalações ficam em
`~/.local/share/lealing/tools/<id>/<version>/` no macOS/Linux e em
`%LOCALAPPDATA%\lealing\tools\<id>\<version>\` no Windows. `active` aponta
para a versão atual, `previous` guarda o rollback e remoções recuperáveis vão
para `.trash`. `XDG_DATA_HOME` continua tendo prioridade quando definido.

Cada entrada do marketplace aponta para seu manifest e artefatos versionados.
A engine segue essas URLs e checksums; não fixa em código uma versão de tool.

## Atualização

A atualização é uma capacidade administrativa da engine, exposta pela CLI:

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

## Catálogo e tools

A engine não compila verticais concretas. O conteúdo do catálogo muda sem
exigir uma release do lealing: cada origem publica seus próprios IDs, versões,
plataformas, requisitos e permissões.

O guia normativo para desenvolver uma extensão está em
**[docs/tool-development.md](docs/tool-development.md)**. Ele cobre arquitetura,
manifest, SDKs públicos, `screen-v1`, plataforma, permissões, testes,
empacotamento, instalação local e publicação de um índice próprio. Os contratos
executáveis vivem em `sdk/*`, `internal/toolmanifest` e
`internal/core/marketplace`, todos nesta engine.

A origem padrão é apenas um exemplo não normativo de consumidor desse contrato.
A engine não importa domínio, adapters ou model daquele repositório e não cria
factory por ID. Para alterar a engine, siga **[AGENTS.md](AGENTS.md)**.

O SDK público também expõe `sdk/machine`: a factory recebe plataforma,
arquitetura, home e diretórios privados já negociados; resolve concessões do
manifest; escolhe adapters sem fallback entre sistemas; executa subprocessos
sem shell e grava estado local atomicamente. Use `screen.FactoryWithError`
com `machine.Select` para reportar uma plataforma sem implementação.

Atualização, login, sincronização, configuração e marketplace são capacidades
administrativas da engine. Elas não aparecem no catálogo e não simulam tools.

## Plataformas

macOS, Windows 10+ e Linux. Cada tool declara em quais sistemas roda, e o catálogo
esconde as demais: no Windows, uma tool exclusiva do macOS não aparece na
busca nem nas sugestões, em vez de abrir e falhar no primeiro comando.

```sh
lealing -platforms      # a matriz, gerada do catálogo
```

A matriz é gerada dos manifests instalados. A documentação da engine não
mantém uma cópia, porque ela ficaria desatualizada assim que uma origem
publicasse ou removesse um artefato.

O estado vai para `%LOCALAPPDATA%\lealing` no Windows e
`~/.local/share/lealing` no macOS e Linux. `XDG_DATA_HOME` tem prioridade
quando definida.

No Windows, `make install` não se aplica (o wrapper é um shell script que
recompila sozinho): use `make build-windows`, que gera `bin/lealing.exe`, e
ponha o executável onde preferir.

Linux compila e a TUI roda. Cada repositório externo decide no manifest se
publica ou não um artefato Linux; a engine não presume suporte.

## Uso

```sh
lealing                                          # abre a TUI
lealing -render 150x42                           # imprime um frame estático
lealing -render 120x34 -keys '/example[enter]'   # já dentro de uma tool
lealing -update                                  # atualiza e sai
lealing -marketplace                             # tools compatíveis em todas as origens
lealing -sources                                 # repositórios de tools cadastrados
lealing -source-add /Users/voce/dev/tools        # registra um repo próprio
lealing -tools                                   # tools instaladas e estado de ativação
lealing -tool-install example-tool               # instala pelo marketplace
lealing -tool-install ./pacote                   # instala/atualiza localmente
lealing -tool-rollback example-tool              # recupera versão anterior
lealing -tool-disable example-tool               # esconde sem desinstalar
lealing -tool-enable example-tool                # torna ativa novamente
lealing -tool-remove example-tool                # move o pacote para .trash
```

Flags: `-debug` (log em arquivo + validação estrita do catálogo),
`-ephemeral` (não persiste favoritos), `-platforms` (matriz de suporte),
`-update` (atualiza a engine sem abrir a TUI), `-marketplace`,
`-marketplace-url`, `-sources`, `-source-add`, `-source-name`,
`-source-remove`, `-source-enable`, `-source-disable`, `-tools`,
`-tool-install`, `-tool-update`, `-tool-enable`, `-tool-disable`,
`-tool-remove`, `-tool-rollback`,
`-tool-validate`, `-login`, `-logout`, `-sync`, `-sync-push`, `-sync-pull`,
`-force` e `-version`.

Sem instalar tools: `make run`. Para renderizar uma extensão, instale um pacote
e use `make render SIZE=150x42 KEYS='/consulta[enter]'`, trocando `consulta`
por um termo que encontre o item instalado.

## Atalhos

| Tecla | Ação |
|---|---|
| `/` ou `ctrl+k` | abre a busca |
| `↑ ↓` / `j k` | move a seleção |
| `← →` / `h l` | troca de painel |
| `tab` | cicla entre painéis |
| `↵` | abre a tool (ou a loja, com a vitrine focada) |
| `m` | abre o marketplace de qualquer lugar |
| `c` | abre a configuração da engine |
| `f` | favorita / desfavorita |
| `r` | recarrega |
| `esc` | volta um nível |
| `?` | ajuda |
| `q` | sai |

A busca aceita filtros inline combináveis com texto livre:
`tag:sistema`, `cat:ai`, `kind:process`, `is:fav`.

Dentro de uma tool, a barra de status lista os atalhos publicados pelo próprio
processo externo.

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
  machine/                      plataforma, permissões, processos e arquivos
internal/
  core/
    domain/                     catálogo, runtime declarativo e uso
    interactive/                portas e caso de uso de sessão externa
    toolinstall/ marketplace/   instalação, rollback e agregação de origens
    toolmanage/                 ativação, desativação e remoção explícitas
    usersync/                   estado sincronizável, conflito e seções
    settings/                   campos declarados, validação e precedência
    service/                    catálogo, launcher e requisitos
  adapter/
    inbound/tui/
      screen/home/              home, busca e catálogo consolidado
      screen/plugin/            tela única para todo manifest screen-v1
      sanitize/                 recorte e allowlist ANSI
    outbound/
      externalcatalog/          descoberta lazy, somente por manifest
      pluginprocess/            spawn, handshake, framing e shutdown
      marketplacehttp/          índice remoto, checksum e extração segura
      marketplacefile/          índice e artefatos em disco (repo próprio)
      marketplacesources/       origens do usuário em JSON atômico
      settingsstore/            ajustes alterados em JSON atômico
      githubauth/               OAuth Device Flow do GitHub
      githubstate/              repositório privado de preferências
      usersyncstore/            credencial no cofre e ajustes em JSON
      toolstore/                instalação local atômica e rollback
      toolstate/                referências host/ID desativadas em JSON atômico
      registry/ search/         consolidação e relevância
      persistence/              favoritos e estatísticas em JSON atômico
  toolmanifest/                 valida lealing.dev/v1
  architecture/                testes das fronteiras de dependência
  bootstrap/                    único composition root
```

### Por que assim

- **Nenhum núcleo de tool na engine.** Domínio, adapters e model pertencem ao
  repositório que publica o executável; a engine conhece apenas manifest e
  protocolo.
- **Providers independentes.** Cada fonte publica um
  `outbound.ToolProvider`; o `registry` consolida, valida e indexa uma vez
  só. Tool sem nome, ID duplicado ou categoria não declarada falham no
  registro, não na tela.
- **Paginação obrigatória.** `domain.Query` sempre carrega `Offset`/`Limit`.
  A TUI nunca materializa o acervo inteiro.
- **Uma tela genérica.** Qualquer item `screen-v1` abre a mesma
  `PluginScreen`, sem factory ou import por ID; a instalação decide quais
  tools existem.
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
instalação/rollback, sanitização ANSI, a PluginScreen genérica, adapters
administrativos e as fronteiras de dependência. A **geometria da TUI**
renderiza as telas da engine em nove tamanhos, de 200×60 a 26×8, verificando
que nenhuma linha excede o frame. Domínio, adapters e geometria de extensões
são responsabilidade do repositório que as publica.

`make cross` compila a engine para todos os alvos publicados e pega imports ou
arquivos específicos de sistema que a suíte executada numa única plataforma
não alcança.

Esse teste de geometria pegou seis bugs reais durante a construção, entre eles
uma fila de cartões transbordando a largura (o `lipgloss.Width` dimensiona o
conteúdo, não o bloco com borda) e painéis saindo pela base em janelas baixas.
Toda tela administrativa nova deve entrar nele — veja
**[AGENTS.md](AGENTS.md#7-componentes-e-geometria)**.

Em janela pequena a home descarta painéis que não cabem. O que é desenhado é
também o que o teclado alcança: o foco nunca fica num painel invisível, e a
moldura do último painel anuncia quantos ficaram de fora. Layout e navegação
lendo listas diferentes é como uma TUI passa a responder a setas que o usuário
não vê.
