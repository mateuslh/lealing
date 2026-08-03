# Alterando a engine lealing — contrato para agentes de IA

Este documento é o contrato operacional deste repositório. O lealing é a
engine: controla terminal, catálogo instalado, busca, favoritos, marketplace,
instalação, atualização, sincronização, segurança e o protocolo entre
processos. Implementações concretas de tools não pertencem aqui.

O guia normativo para autores está em `docs/tool-development.md`; os contratos
executáveis ficam nos SDKs públicos e nos validadores deste repositório. O
repositório [`github.com/mateuslh/lealing-tools`](https://github.com/mateuslh/lealing-tools)
é somente a origem de marketplace configurada por padrão e um exemplo não
normativo de consumidor. A engine não conhece seus IDs, domínio, adapters,
telas ou executáveis; conhece apenas índices, manifests e o protocolo público.

## 0. Protocolo obrigatório

Antes de editar qualquer arquivo:

1. rode `git status --short` e preserve alterações que já existiam;
2. rode `make test`;
3. se a linha de base falhar, corrija a causa ou reporte o bloqueio antes de
   acrescentar outra mudança;
4. leia inteira uma capacidade de referência da mesma vertical: core, portas,
   adapters, tela, bootstrap e testes;
5. se a mudança tocar manifest, SDK, marketplace ou `screen-v1`, leia inteiro
   `docs/tool-development.md`, os pacotes públicos afetados e seus testes;
6. só então implemente de dentro para fora.

Não use a verificação final para descobrir que a base já estava quebrada. Não
apague nem reverta alterações locais que não pertencem à tarefa.

## 1. Fronteira entre engine e tools

A separação é literal:

```text
┌──────────────────────────── ENGINE ────────────────────────────┐
│ índice → instalação → manifest → catálogo → tela genérica     │
│                                      │                         │
│                               runtime port                     │
│                                      │                         │
│                        processo + protocolo JSON               │
└──────────────────────────────────────┼─────────────────────────┘
                                       │ stdin/stdout screen-v1
┌──────────────────────────────────────▼─────────────────────────┐
│ TOOL: model · domínio · adapters · persistência · executável   │
└────────────────────────────────────────────────────────────────┘
```

Na engine podem existir somente capacidades administrativas, como:

- home, busca, favoritos e histórico;
- marketplace, origens, instalação, rollback, ativação e remoção;
- configuração, autenticação, sincronização e atualização da engine;
- chrome, confirmação global, ações do host e diagnóstico do runtime;
- SDK, protocolo, validação de manifest e execução genérica.

Não adicione à engine:

- ID, nome, resumo, palavras-chave ou matriz de uma tool concreta;
- domínio, caso de uso, parser ou adapter de uma tool;
- factory, tela ou runner escolhido por ID;
- fallback que simule uma tool ausente;
- referência a um executável concreto do acervo.

Uma tool nova entra por um pacote instalado com manifest. A engine deve abrir
normalmente com catálogo vazio. Ausência de pacote não vira item quebrado.

Fixtures e exemplos deste repositório usam nomes genéricos, como `example-tool`
e `another-tool`. Não copie IDs do marketplace padrão para testes da engine:
isso criaria acoplamento editorial sem aparecer nos imports.

## 2. Arquitetura hexagonal da engine

```text
driving adapter (CLI/TUI)
        │ chama
        ▼
core/port/inbound ──► casos de uso em core
                              │ consomem
                              ▼
                     core/port/outbound
                              ▲ implementam
                              │
                    driven adapters (disco, rede, processos)

bootstrap = único lugar que conhece e liga implementações concretas
```

As regras são executáveis por `internal/architecture`:

- `internal/core/**` importa somente biblioteca padrão e outros pacotes de
  `internal/core/**`;
- a TUI pode importar domínio e portas de entrada, nunca
  `core/port/outbound` nem `adapter/outbound`;
- um adapter de saída implementa uma porta do core e nunca importa TUI,
  catálogo, bootstrap ou outro adapter de saída;
- adapters não se constroem entre si; composição pertence a
  `internal/bootstrap`;
- `runtime.GOOS` e `runtime.GOARCH` só aparecem em
  `internal/bootstrap/platform.go`;
- caminhos, home, relógio, target e clientes variáveis entram por construtor;
- `Update` e `View` não leem arquivo, ambiente, rede ou processo e não chamam
  portas; todo I/O fica dentro de `tea.Cmd`;
- erros descem pelas portas e pelo logger; nada escreve em stdout enquanto a
  TUI ou o protocolo ocupam o terminal.

Portas compartilhadas ficam separadas em:

- `internal/core/port/inbound`: casos de uso consumidos por driving adapters;
- `internal/core/port/outbound`: recursos consumidos pelo core e
  implementados por driven adapters.

Mesmo um caso de uso fino é preferível a ligar TUI diretamente a disco, rede
ou processo.

## 3. Implementando uma capacidade administrativa

Implemente nesta ordem:

1. modelos e regras puras no core;
2. portas de entrada e saída;
3. serviço de aplicação;
4. adapter de saída;
5. tela ou comando de CLI;
6. composição no bootstrap;
7. testes por camada e geometria.

### 3.1 Core

O core contém tipos, validação, política e orquestração. Não contém framework
de terminal, cliente HTTP concreto, `os/exec`, formato de arquivo específico
de um adapter ou decisão de layout.

Cálculos derivados pertencem ao domínio. O serviço implementa a porta de
entrada e consome portas de saída. Trave os contratos em compile-time:

```go
var _ inbound.Reader = (*Service)(nil)
```

### 3.2 Adapters de saída

Um adapter traduz o mundo externo para tipos do core. Ele não escolhe política
editorial, não compõe outro adapter e não conhece a TUI.

Regras obrigatórias:

1. todo comando recebe `context.Context` e usa `exec.CommandContext`;
2. entrada do usuário nunca é interpolada numa linha de shell;
3. executável e argumentos são tokens separados e valores variáveis são
   validados;
4. cliente, caminho, relógio, home e limites entram pelo construtor;
5. parsers frágeis são funções testáveis com fixtures fixas;
6. a suíte normal não depende de rede, credencial, home ou executável real.

### 3.3 CLI e TUI

Driving adapters chamam portas de entrada. Eles convertem input em intenção e
estado em apresentação; não repetem regra do serviço.

I/O da TUI vive em `tea.Cmd`:

```go
func (m *Model) load() tea.Cmd {
    reader := m.reader
    return func() tea.Msg {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        value, err := reader.Read(ctx)
        return loadedMsg{value: value, err: err}
    }
}
```

`Update` apenas altera estado e agenda comandos. `View` apenas renderiza o
estado recebido. Capture a dependência antes de montar o comando, pois o model
pode mudar antes da execução.

### 3.4 Bootstrap

`internal/bootstrap` é o composition root. Ele liga somente capacidades da
engine e infraestrutura genérica. Não adicione switch, map ou factory por ID
de tool.

Uma instalação externa é descoberta pelo provider de manifests e aberta pela
factory genérica de `screen/plugin`. Se uma capacidade administrativa exigir
implementação por plataforma, a escolha fica em `bootstrap/platform.go` e o
restante do código recebe uma interface do core.

## 4. Contrato genérico de tools externas

Este repositório mantém o contrato público, não as verticais que o utilizam.
Uma tool `screen-v1` vive em seu próprio módulo e pode importar somente APIs
públicas:

- `github.com/mateuslh/lealing/sdk/protocol`;
- `github.com/mateuslh/lealing/sdk/screen`;
- `github.com/mateuslh/lealing/sdk/component`;
- `github.com/mateuslh/lealing/sdk/machine`.

Ela nunca importa `github.com/mateuslh/lealing/internal`.

O pacote instalado contém `manifest.yaml` e o executável da plataforma. O
manifest usa `apiVersion: lealing.dev/v1`, runtime declarativo e permissões
mínimas. A engine valida tudo antes de ativar a instalação e não inicia o
processo durante busca, descoberta ou validação.

Exemplo mínimo:

```yaml
apiVersion: lealing.dev/v1
id: example-tool
version: 0.1.0
name: Example Tool
summary: Demonstra uma extensão externa.
category: utilities
risk: safe
runtime:
  kind: process
  protocol: {min: 1, max: 1}
  executable: lealing-tool-example
ui: {mode: screen-v1}
platforms: [darwin-arm64, windows-amd64]
requirements: []
permissions:
  filesystem: {read: [], write: []}
  network: false
  subprocess: false
```

Regras do manifest:

- ID é permanente e único;
- `summary` tem uma linha e termina com ponto;
- executável é um nome simples, sem diretório, argumentos ou shell;
- requisitos de `PATH` contêm somente `executable`, `name` e `installHint`;
- risco e permissões descrevem o comportamento real;
- plataformas e intervalo de protocolo são explícitos;
- a engine não infere suporte, permissão ou requisito ausente;
- `ui.wantsMouse` é opcional e falso por padrão — só declare `true` se a tool
  de fato interpreta clique, arraste ou roda; sem a declaração, a engine não
  captura o mouse do terminal e o usuário mantém a seleção nativa de texto.

O model implementa `screen.Model`. `stdout` pertence exclusivamente ao framing
`Content-Length`; logs vão para stderr. Ações do host usam `screen.Request` e
somente capabilities negociadas. A engine sanitiza ANSI, confirma operações
destrutivas e rejeita requests não concedidos.

Estrutura, contratos, testes de conformidade, empacotamento e publicação estão
documentados em `docs/tool-development.md`. Mantenha esse guia sincronizado com
os SDKs e validadores da engine sempre que o contrato público mudar. Um
repositório externo pode ilustrar o uso, mas nunca definir comportamento.

## 5. Marketplace e origens

O marketplace agrega origens, não embute um acervo. A origem padrão aponta
para o repositório de exemplo indicado no início deste documento; o usuário
pode adicionar outras origens HTTPS ou locais.

Invariantes:

- **confiança pertence à engine:** apenas a origem marcada como confiável no
  composition root preserva canais elevados; configuração do usuário nunca
  promove uma origem;
- **prioridade vence versão:** uma origem menos prioritária não sequestra um
  ID publicando SemVer maior; referência qualificada permite escolha explícita;
- **origem é unidade de falha:** erro num índice não impede os demais;
- **descoberta não executa:** listar, buscar e validar nunca inicia binários;
- **instalação é atômica:** checksum, limites, path traversal, links, ID,
  versão, plataforma e manifest são validados antes da troca de `active`;
- **remoção é recuperável:** pacotes removidos vão para `.trash` e o caminho
  é informado ao usuário.

Uma origem local aponta para `index.json` e artefatos relativos ao diretório.
Ela não ganha confiança por estar em disco e não pode escapar por `..` ou
symlink.

## 6. Plataforma e configuração

A engine compila para macOS, Windows e Linux. Tools declaram seus próprios
targets no manifest; a engine filtra o catálogo pelo target atual e não mantém
matriz de suporte de extensões concretas.

Quando a engine precisa de integração nativa:

1. mantenha um pacote por sistema sem build tag quando o específico é apenas o
   processo disparado;
2. exponha parser testável para formatos externos;
3. injete home, diretórios, relógio, arquitetura e caminhos;
4. registre a escolha somente em `internal/bootstrap/platform.go`;
5. rode `make cross`.

Valores configuráveis seguem precedência explícita: ajuste persistido,
ambiente, valor de build e padrão. Persista somente overrides; gravar todos os
padrões impediria a engine de melhorá-los numa atualização.

## 7. Componentes e geometria

Telas da engine usam somente componentes de
`internal/adapter/inbound/tui/component` e cores de `theme`. O SDK público
oferece os equivalentes para processos externos. Não escreva cor literal.

Prefira:

| Precisa de | Use |
|---|---|
| Moldura com título | `component.Panel` |
| Lista rótulo/valor | `component.FieldList` |
| Interruptor / número | `component.Toggle`, `component.Stepper` |
| Percentual | `component.Meter` |
| Comparação | `component.BarChart` |
| Série temporal | `component.Sparkline` |
| Esquerda/direita | `component.Spread` |
| Carregando, erro ou vazio | `component.Center` |
| Truncamento ANSI-safe | `component.TruncateTail` |

Armadilhas conhecidas:

1. `lipgloss.Width(n)` dimensiona o conteúdo, não borda e padding;
2. `lipgloss.Place` posiciona, mas não recorta;
3. `MaxWidth` e `MaxHeight` devem estar no mesmo estilo do `Padding`;
4. largura visual é medida com `lipgloss.Width`, nunca `len`.

Toda tela administrativa entra em
`internal/adapter/inbound/tui/screen/screens_test.go` e é renderizada nas nove
geometrias. Use fakes das portas de entrada, nunca adapters reais. Tela externa
é testada no repositório que a publica.

## 8. Testes mínimos

Uma capacidade da engine precisa, na proporção do risco, de:

- regras puras e validação do core;
- caso de uso com fakes das portas de saída;
- parser ou tradução do adapter com fixture fixa;
- comportamento da CLI ou tela com fake da porta de entrada;
- geometria quando houver tela;
- composição e fronteiras arquiteturais;
- falha parcial, cancelamento e timeout quando houver I/O;
- race detector quando houver goroutine, canal, cache ou escrita concorrente.

O protocolo e o instalador exigem também casos hostis: framing parcial,
mensagem grande, ANSI proibido, processo que cai, incompatibilidade de versão,
checksum inválido, travessia de caminho, symlink e shutdown.

## 9. Verificação final

Rode, nesta ordem, e não pare no primeiro sucesso:

```sh
make fmt
make vet
make test
make cross
make render SIZE=150x42
make render SIZE=60x20
git diff --check
git status --short
```

Use `make race` quando a mudança tocar concorrência. Olhe os renders de
verdade: teste de largura não percebe hierarquia ruim, texto confuso ou foco
invisível. Nenhum binário, log, fixture temporária ou credencial entra no diff.

## 10. Publicação da engine

Desenvolvimento normal termina com mudanças commitadas e enviadas à `main`.
Não crie tag local nem commit artificial de release. Somente quando o usuário
pedir explicitamente para publicar:

```sh
make release VERSION=vX.Y.Z
```

O alvo solicita `.github/workflows/release.yml`. Acompanhe o workflow até o
fim e confira os artefatos e `checksums.txt` antes de afirmar que a versão foi
publicada.

Nunca mova, apague ou recrie tag enviada. Se uma publicação falhar, corrija a
causa e publique uma versão nova. Nunca use `--no-verify`.

## 11. Convenções

- comentários em português; identificadores em inglês;
- comente o porquê, não o que o código repete;
- nada de `panic` fora de `init`;
- nada de dependência global escondida;
- erros descem; não escreva diagnóstico em stdout;
- mantenha operações destrutivas explícitas, confirmadas e recuperáveis quando
  possível;
- preserve IDs persistidos e compatibilidade de protocolo;
- uma mudança no SDK considera consumidores externos e negociação de versão;
- mudanças no contrato público atualizam `docs/tool-development.md`;
- a engine nunca passa a depender do conteúdo atual da origem padrão.

## 12. Onde olhar

| Dúvida | Arquivo |
|---|---|
| Guia normativo para desenvolver e publicar tools | `docs/tool-development.md` |
| Fronteiras executáveis | `internal/architecture/dependencies_test.go` |
| Portas de entrada compartilhadas | `internal/core/port/inbound/inbound.go` |
| Portas de saída compartilhadas | `internal/core/port/outbound/outbound.go` |
| Casos de uso | `internal/core/service/` |
| Manifest | `internal/toolmanifest/` |
| DTOs e framing | `sdk/protocol/` |
| Runtime Go externo | `sdk/screen/` |
| Componentes públicos | `sdk/component/` |
| Plataforma, processos e arquivos públicos | `sdk/machine/` |
| Tela genérica de extensão | `internal/adapter/inbound/tui/screen/plugin/` |
| Processo e handshake | `internal/adapter/outbound/pluginprocess/` |
| Catálogo instalado | `internal/adapter/outbound/externalcatalog/` |
| Registry e busca | `internal/adapter/outbound/registry/` · `search/` |
| Marketplace remoto/local | `internal/adapter/outbound/marketplacehttp/` · `marketplacefile/` |
| Instalação e rollback | `internal/adapter/outbound/toolstore/` |
| Ativação e remoção | `internal/core/toolmanage/` · `adapter/outbound/toolstate/` |
| Configuração | `internal/core/settings/` · `adapter/outbound/settingsstore/` |
| Sincronização | `internal/core/usersync/` |
| Escolha por sistema | `internal/bootstrap/platform.go` |
| Composition root | `internal/bootstrap/bootstrap.go` |
| Geometria | `internal/adapter/inbound/tui/screen/screens_test.go` |

Antes de concluir, confirme:

- a mudança pertence à engine, e não a uma tool concreta?
- o core continua independente de framework e infraestrutura?
- driving adapters chamam somente portas de entrada?
- todo I/O da TUI está em `tea.Cmd` com timeout e cancelamento?
- dependências variáveis entram por construtor?
- plataforma é escolhida somente no bootstrap?
- descoberta e validação continuam sem spawn?
- manifest, framing, permissões e conteúdo ANSI continuam validados?
- `docs/tool-development.md` continua descrevendo o contrato executável atual?
- testes proporcionais, `fmt`, `vet`, `test`, `cross` e renders passaram?
