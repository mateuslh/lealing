# Criando tools no lealing — contrato para agentes de IA

Este documento é o contrato operacional para alterar o lealing. Ele descreve
o fluxo completo, as fronteiras da arquitetura hexagonal e a definição de
pronto. Não improvise outra organização: `internal/architecture` protege as
dependências em CI, o bootstrap valida a ligação do catálogo e os testes de
geometria validam a TUI.

## 0. Protocolo obrigatório

Antes de editar qualquer arquivo:

1. rode `git status --short` e preserve alterações que já existiam;
2. rode `make test`;
3. se a linha de base falhar, corrija a causa ou reporte o bloqueio antes de
   adicionar a tool;
4. leia uma tool de referência inteira, incluindo core, adapter, tela,
   bootstrap e testes;
5. só então implemente de dentro para fora.

Não use um teste final para descobrir que a base já estava quebrada. Não
apague nem reverta alterações locais que não pertencem à tarefa.

## Fronteiras que nenhuma tool pode atravessar

```text
driving adapter (TUI)
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

As regras são literais:

- `internal/core/**` só importa biblioteca padrão e outros pacotes
  `internal/core/**`;
- a TUI pode importar domínio e portas de entrada, mas nunca
  `core/port/outbound` nem `adapter/outbound`;
- um adapter de saída implementa uma porta do core e nunca importa a TUI,
  o catálogo, o bootstrap ou outro adapter de saída;
- adapters não se constroem entre si: composição pertence a
  `internal/bootstrap`;
- `runtime.GOOS` e `runtime.GOARCH` só aparecem em
  `internal/bootstrap/platform.go`;
- caminhos, home, relógio, target e clientes variáveis entram por construtor;
  um adapter não deve esconder seleção de plataforma ou configuração global;
- `Update` e `View` são funções de estado/render: não leem arquivo, ambiente,
  rede ou processo e não chamam portas. Todo I/O fica dentro de `tea.Cmd`;
- não existe fallback que simula sucesso. Tool sem tela ou runner faz o
  bootstrap falhar em `validateWiring`.

Portas compartilhadas ficam separadas em:

- `internal/core/port/inbound`: casos de uso consumidos por driving adapters;
- `internal/core/port/outbound`: recursos consumidos pelo core e
  implementados por driven adapters.

Portas específicas de uma tool podem ficar em `internal/core/<tool>`, mas
nomeie e comente os dois lados sem ambiguidade: a tela consome a porta de
entrada; o serviço implementa essa porta e consome a porta de saída. Mesmo um
caso de uso fino é preferível a ligar TUI diretamente a disco ou processo.

---

## 1. Decida o tipo da tool

| Tipo | Quando | `Kind` | O que você escreve |
|---|---|---|---|
| **Nativa** | A tool tem interface própria dentro da TUI | `KindBuiltin` | Core + caso de uso + adapter de saída + tela + composição |
| **Externa screen-v1** | É uma tool instalável com conteúdo TUI rico | `KindProcess` + `ui.mode: screen-v1` | Executável + manifest + SDK; nenhuma edição no bootstrap |
| **Processo** | A tool é um binário externo | `KindProcess` | Catálogo + runner/resolver tipado + composição |
| **Script** | A tool é um script interpretado | `KindScript` | Catálogo + runner/resolver tipado + composição |
| **Remota** | A tool fala com um serviço | `KindRemote` | Core + cliente de saída + caso de uso + runner ou tela |

As tools históricas ainda são nativas. `token-usage` é a vertical de
referência para uma tool externa: domínio, adapters e tela vivem em
`github.com/mateuslh/lealing-tools`, e a engine conhece apenas seu manifest e
o protocolo.
Para uma nativa mínima, comece por `system-info`; para ver política e suporte
parcial, leia `power`; para orquestração com vários recursos externos, leia
`repoclone`.

---

## 2. Declare em quais sistemas a tool roda

O lealing roda em **macOS e Windows 10+**. Toda tool declara seu suporte no
catálogo:

```go
Platforms: domain.Darwin | domain.Windows,
```

**Omitir o campo significa "roda em todo lugar"** — é o certo para tools que
só leem arquivo ou falam com a rede, e é o caso da maioria. Declare quando a
tool depender de um adapter nativo. O registry esconde as demais: no Windows,
uma tool só de macOS não aparece na busca nem nas sugestões, em vez de abrir e
falhar no primeiro comando.

Confira a matriz do acervo a qualquer momento:

```sh
lealing -platforms
```

### Se a tool depende de outro executável

Declare cada ferramenta obrigatória no próprio item do catálogo:

```go
Requirements: []domain.Requirement{
    {
        Executable:  "gh",
        Name:        "GitHub CLI",
        InstallHint: "instale o GitHub CLI e rode `gh auth login`",
    },
},
```

`Executable` é somente o nome procurado no `PATH`: sem caminho, argumentos ou
comando de shell. `Name` é o rótulo amigável e pode ser omitido;
`InstallHint` diz como resolver a ausência. O registry recusa requisito vazio,
duplicado ou com argumentos.

A home pede essa decisão à porta de entrada `inbound.Prerequisites`. O
`PrerequisiteService` resolve a tool e consulta `outbound.RequirementChecker`;
a TUI nunca chama o checker diretamente. Se algo faltar, abre a tela genérica
de pré-requisitos com o nome da tool e todas as ferramentas ausentes.
**Não repita a checagem na tela ou no adapter e não tente instalar
automaticamente.**

Use esta spec para executáveis portáveis (`git`, `gh`, `docker`). Aplicações
descobertas por arquivo ou configuração — como uma IDE — continuam sendo
validadas pelo adapter no momento em que ele usa essa integração, porque não
há um nome de `PATH` estável entre macOS e Windows.

### Se a tool precisa de um adapter nativo

Três regras, nesta ordem:

1. **Um pacote por sistema, nenhum com build tag.** `outbound/macos` e
   `outbound/windows` compilam em qualquer lugar — o que é específico é o
   processo que eles disparam, não o código Go. É isso que permite testar o
   parser do Windows na mesma suíte que roda no Mac.
2. **O adapter não instancia outro adapter.** Se GitHub e IntelliJ participam
   do mesmo fluxo, o bootstrap constrói os dois e os entrega ao caso de uso.
3. **Registre em `bootstrap/platform.go`.** É o único switch por sistema
   operacional e o único arquivo que lê `runtime.GOOS`/`GOARCH`.
   `validateWiring` confere a composição antes de abrir a TUI.
4. **Injete configuração.** Home, diretórios, relógio, arquitetura e caminhos
   nativos são parâmetros; não chame `os.UserHomeDir` ou detecte a plataforma
   escondido num construtor.
5. **Exporte o parser e teste-o com uma amostra real.** `ParseCustom` (pmset),
   `ParseSettings` (powercfg) e `ParseSnapshot` (WMI) existem exatamente para
   isso. Cole a saída verdadeira do comando no teste; é a única forma de pegar
   um formato que mudou.

Prefira o que não depende do idioma da máquina. A leitura de energia no
Windows vai pelo CIM, e não pela saída de `powercfg /query`, porque os rótulos
dela são traduzidos — o parser quebraria em uma máquina em português.

### Quando o suporte é parcial

Nem toda plataforma expõe o mesmo painel: o `pmset` tem onze chaves de
energia, o `powercfg` cobre três. Nesse caso **a porta declara o que sabe
fazer** e a tela se ajusta — em vez de uma tela por sistema, ou de controles
que não ligam nada:

```go
// Features declara o que esta implementação sabe ler e escrever.
func (m *PowerManager) Features() power.Feature {
    return power.FeatureSleep | power.FeatureDisplaySleep | power.FeatureDiskSleep
}
```

O que isso exige do núcleo: **uma tabela única de campos** (`core/power`
`Fields()`), consultada por quem desenha a lista *e* por quem mescla presets.
Duas listas divergem no primeiro campo novo — e o sintoma é a tela dizer
"alterações não aplicadas" para um campo que a plataforma não grava, sem
mostrar nenhum controle diferente.

| Tool | macOS | Windows | Observação |
|---|:---:|:---:|---|
| `system-info` | ✓ | ✓ | `sysctl`/`sw_vers`/`pmset` · CIM |
| `power-control` | ✓ | parcial | `powercfg` não tem Power Nap, standby, `tcpkeepalive` nem hibernação; e não pede elevação, então não há senha a dispensar |
| `token-usage` | ✓ | ✓ | lê os logs das CLIs, iguais nos dois |
| `self-update` | ✓ | ✓ | releases do GitHub; `git` + `go build` no clone |
| `clone-repo-bradesco` | ✓ | ✓ | GitHub CLI + Git; registra clones no IntelliJ |
| `git-dev-radar` | ✓ | ✓ | leitura recursiva dos clones e branches em `~/dev` |

---

## 3. Tool de processo ou script

O caminho é curto, mas não é só catálogo. Adicione a entrada em
`internal/catalog/catalog.go`, dentro de `Builtin.Provide`:

```go
{
    ID:       "docker-prune",              // estável e único; nunca mude depois
    Name:     "Limpar Docker",             // aparece na lista
    Summary:  "Remove imagens, volumes e redes órfãs.", // UMA linha, com ponto final
    Detail:   "Roda `docker system prune -a`. Não afeta containers em execução.",
    Category: System.ID,                   // uma das categorias declaradas no topo do arquivo
    Kind:     domain.KindProcess,
    Risk:     domain.RiskDestructive,      // ver tabela de risco abaixo
    Glyph:    "▨",                         // um caractere; opcional
    Keywords: []string{"docker", "prune", "limpar", "espaço"}, // sinônimos p/ busca
    Tags:     []domain.Tag{"sistema", "limpeza"},
},
```

Depois implemente um `outbound.ToolRunner` ou configure `runner.Process` com
um `CommandResolver` que:

- resolva somente IDs conhecidos;
- devolva executável e argumentos como tokens separados;
- valide qualquer valor vindo do usuário;
- nunca monte `sh -c`, `cmd /c` ou uma string de shell;
- respeite o `context.Context`.

Registre o runner na fatia `toolRunners` de
`internal/bootstrap/bootstrap.go`. `validateWiring` recusa o arranque se uma
tool não nativa não tiver runner para o seu `Kind`; não há `Placeholder` nem
execução simulada.

Se a tool precisar de regra de negócio, confirmação específica, consulta
prévia ou mais de um recurso externo, ela deixou de ser “só processo”: crie
um caso de uso no core e um runner fino que o invoque.

### Escolhendo o `Risk`

| Valor | Critério | Efeito |
|---|---|---|
| `RiskSafe` | Somente leitura | Nenhum |
| `RiskCaution` | Escreve estado local recuperável | Badge âmbar |
| `RiskDestructive` | Apaga dados ou toca ambiente externo | Badge vermelho + `Launch` exige `Confirmed` |

**Errar para o lado seguro custa um badge; errar para o lado inseguro apaga
dados de alguém.** Na dúvida, suba um nível.

---

## 4. Tool nativa (o caminho completo)

Implemente nesta ordem: modelo e portas → caso de uso → adapter de saída →
tela → composição → catálogo e testes. Cada passo só conhece os anteriores,
por isso a direção de dependência permanece visível.

### 4.1 Núcleo — `internal/core/<tool>/<tool>.go`

Tipos, regras puras, porta de entrada, porta de saída e o caso de uso.
**Regra absoluta: este pacote só importa a biblioteca padrão ou outros
pacotes do core.** Sem `lipgloss`, `bubbletea`, `os/exec`, cliente HTTP ou
adapter.

```go
// Package disco é o domínio da tool "Uso de Disco".
package disco

import "context"

// Volume é um ponto de montagem com sua ocupação.
type Volume struct {
    Name       string
    Mount      string
    TotalBytes uint64
    FreeBytes  uint64
}

// UsedPercent é a fração ocupada, de 0 a 100.
func (v Volume) UsedPercent() float64 {
    if v.TotalBytes == 0 {
        return 0
    }
    return float64(v.TotalBytes-v.FreeBytes) / float64(v.TotalBytes) * 100
}

// Source é a porta de saída que o core pede ao sistema.
type Source interface {
    Volumes(ctx context.Context) ([]Volume, error)
}

// Reader é a porta de entrada que a tela pode pedir à aplicação.
type Reader interface {
    List(ctx context.Context) ([]Volume, error)
}

// Service implementa o caso de uso sem conhecer terminal nem sistema.
type Service struct {
    source Source
}

var _ Reader = (*Service)(nil)

func NewService(source Source) *Service {
    return &Service{source: source}
}

func (s *Service) List(ctx context.Context) ([]Volume, error) {
    volumes, err := s.source.Volumes(ctx)
    if err != nil {
        return nil, err
    }
    // Ordenação, filtragem e políticas pertencem aqui.
    return volumes, nil
}
```

Cálculos derivados (`UsedPercent`) ficam **aqui**, não na tela. É o que
permite testá-los sem renderizar nada. Orquestração, validação e política
ficam no `Service`, não no adapter.

### 4.2 Adapter — `internal/adapter/outbound/<plataforma>/<tool>.go`

Implementa **a porta de saída** falando com o mundo real. Um arquivo por
plataforma que a tool declara suportar — ver a seção 2.

```go
type DiskSource struct{}

var _ disco.Source = (*DiskSource)(nil) // trava o contrato em compile-time

func (d *DiskSource) Volumes(ctx context.Context) ([]disco.Volume, error) {
    out, err := run(ctx, "/bin/df", "-k")
    if err != nil {
        return nil, err
    }
    return ParseDF(out), nil    // exportado: é o que o teste exercita
}
```

**Exporte a função de parsing.** É a parte com bug em potencial, e exportá-la
permite testá-la com uma string fixa, sem tocar no sistema. Veja
`macos.ParseCustom` e `macos/power_test.go`.

Cinco regras para adapters:

1. **Campo ilegível vira valor padrão, não erro.** Uma tela que se recusa a
   abrir porque um `sysctl` sumiu é pior que uma tela com um traço.
2. **Todo comando recebe `ctx`.** Use `exec.CommandContext`, nunca
   `exec.Command`.
3. **Nada de entrada do usuário em linha de shell.** Se precisar montar um
   comando, gere os tokens você mesmo e valide o que vier de fora — veja
   `safeUserName` em `macos/power.go`.
4. **Nenhuma regra editorial ou de layout.** O adapter traduz formatos e
   devolve tipos do core; não escolhe cor, rótulo de painel ou atalho.
5. **Nenhuma composição.** Dependências adicionais entram no construtor e
   são ligadas pelo bootstrap.

### 4.3 Tela — `internal/adapter/inbound/tui/screen/<tool>/<tool>.go`

Implemente `tui.Screen`:

```go
type Model struct {
    deps   tui.Deps
    reader disco.Reader // porta de entrada, nunca adapter concreto
    // ...
}

var _ tui.Screen = (*Model)(nil)

func (m *Model) ID() tui.ScreenID   { return "tool/disk-usage" }
func (m *Model) Title() string      { return "uso de disco" }  // minúsculas: vai no breadcrumb
func (m *Model) Init() tea.Cmd      { return m.load() }
func (m *Model) Update(tea.Msg) (tui.Screen, tea.Cmd)
func (m *Model) View(tui.Frame) string
func (m *Model) Hints() []tui.Hint
```

**A regra que mais se quebra: nenhuma chamada de porta dentro de `Update` ou
`View`.** I/O vive só dentro de `tea.Cmd`, que roda em goroutine:

```go
// CERTO
func (m *Model) load() tea.Cmd {
    reader := m.reader          // captura antes: o Model pode mudar
    return func() tea.Msg {
        ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
        defer cancel()
        vols, err := reader.List(ctx)
        return loadedMsg{volumes: vols, err: err}
    }
}

// ERRADO — congela a interface inteira enquanto o df roda
func (m *Model) Update(msg tea.Msg) (tui.Screen, tea.Cmd) {
    vols, _ := m.reader.Volumes(context.Background())
    m.volumes = vols
    return m, nil
}
```

A tela pode importar `core/<tool>` e `core/port/inbound`. Ela não pode
importar `core/port/outbound`, `adapter/outbound`, `bootstrap` ou `catalog`.
Se você precisar de algo desses pacotes, falta um caso de uso ou uma
dependência no construtor.

Métodos opcionais que enriquecem o chrome:

| Método | Efeito |
|---|---|
| `Status() (string, lipgloss.TerminalColor)` | Texto à direita da barra de status |
| `Meta() []string` | Números na topbar |
| `Capturing() bool` | Retorne `true` enquanto um campo de texto tiver o foco — sem isso, teclar `q` fecha o programa |

`Hints()` **precisa incluir um hint com `esc`** — o teste
`TestTelasAnunciamAtalhosEIdentidade` verifica isso. Sem ele o usuário fica
sem saída visível.

### 4.4 Ligação — `internal/bootstrap/bootstrap.go`

```go
diskSource := macos.NewDiskSource()
diskService := disco.NewService(diskSource)

screens := tui.Screens{
    // ...
    "disk-usage": func() tui.Screen {
        return discoscreen.New(deps, diskService)
    },
}
```

A chave é exatamente o `ID` permanente da tool no catálogo. O bootstrap roda
`validateWiring` antes de construir a home: ID divergente, factory órfã, tool
nativa sem tela ou processo sem runner são erros de inicialização claros.

Por fim, declare a tool no catálogo (seção 3), com `Kind: domain.KindBuiltin`.

Se houver escolha por plataforma, construa a dependência em
`bootstrap/platform.go` e devolva a interface do core. Não espalhe
`if platform == ...` por `bootstrap.go`, telas ou adapters.

### 4.5 Testes por camada

Uma tool nativa precisa, no mínimo, de:

- teste puro das regras e cálculos do core;
- teste do caso de uso com fakes das portas de saída;
- teste do parser do adapter com uma amostra fixa e real;
- teste de comportamento da tela com fake da porta de entrada;
- caso no teste global de geometria;
- composição aceita por `validateWiring` e pelo teste arquitetural.

Teste adapter real apenas em smoke test separado. A suíte normal nunca depende
de rede, credencial, `pmset`, `powercfg`, `git` ou arquivos da máquina.

---

## 5. Desenhando a tela

Use **só** os componentes de `internal/adapter/inbound/tui/component` e as
cores de `theme`. Nunca escreva um hex literal nem um `lipgloss.Color("205")`.

| Precisa de | Use |
|---|---|
| Moldura com título | `component.Panel` |
| Lista rótulo/valor | `component.FieldList` |
| Interruptor / seletor numérico | `component.Toggle`, `component.Stepper` |
| Barra de percentual | `component.Meter` |
| Barras comparativas | `component.BarChart` |
| Série temporal em uma linha | `component.Sparkline` |
| Alinhar esquerda/direita | `component.Spread` |
| Carregando / erro / vazio | `component.Center` |
| Truncar preservando ANSI | `component.TruncateTail` |

### As três armadilhas de geometria

Todas já causaram bug neste repositório:

1. **`lipgloss.Width(n)` dimensiona o conteúdo, não o bloco.** Com borda e
   padding, o resultado sai 4 colunas mais largo. Desconte: `Width(n-4)`.
2. **`lipgloss.Place` posiciona mas não recorta.** Texto maior que o frame
   sai por fora. Use `component.Center`.
3. **`MaxWidth`/`MaxHeight` precisam vir no mesmo estilo do `Padding`.**
   Recortar só o miolo deixa as linhas de respiro estourando.

Meça sempre com `lipgloss.Width`, nunca com `len()` — `len()` conta os bytes
das sequências de cor e de cada caractere acentuado.

---

## 6. Registre a tela no teste de geometria

**Obrigatório.** Em `internal/adapter/inbound/tui/screen/screens_test.go`,
adicione um caso em `cases`:

```go
{
    name: "disco",
    build: func(t *testing.T) tui.Screen {
        return settle(t, disco.New(deps(), fakeReader{volumes: discoFixture()}))
    },
    keys: []string{"down", "tab"},   // interações que mudam o layout
},
```

O teste renderiza a tela em nove tamanhos, de 200×60 a 26×8, e falha se
qualquer linha exceder o frame. Foi ele que pegou a fila de KPIs
transbordando e o painel de energia estourando a largura.

Use um duplo (`fakeReader`), nunca o adapter real: o teste precisa rodar em
CI, sem `pmset` nem `~/.claude`.

---

## 7. Verificação final

Rode, nesta ordem, e não pare no primeiro sucesso:

```sh
make fmt
make vet
make test
make cross                                     # compila nas plataformas suportadas
make render SIZE=150x42                        # home com a tool nova
make render SIZE=150x42 KEYS='/disco[enter]'   # a tela da tool
make render SIZE=60x20  KEYS='/disco[enter]'   # e em janela estreita
```

`make cross` é rápido e pega o erro que a suíte não pega: um import que só
existe em um sistema. Rode-o sempre que mexer em `outbound/` ou em
`bootstrap/platform.go`.

`make render` imprime um frame estático — é como você *vê* o resultado sem
um terminal interativo. `KEYS` aceita caracteres literais e teclas especiais
entre colchetes: `[enter]`, `[esc]`, `[tab]`, `[up]`, `[down]`, `[left]`,
`[right]`, `[space]`, `[backspace]`.

**Olhe a saída de verdade.** Um teste verde só garante que nada transbordou;
ele não sabe se as colunas ficaram alinhadas ou se o texto faz sentido.

Se você adicionou goroutines, cache, canal ou escrita concorrente, rode também
`make race`. Antes de entregar, confira `git diff --check` e `git status
--short`; nenhum binário, log, fixture temporária ou credencial pode entrar no
diff.

---

## 8. Publicando uma versão

O desenvolvimento normal termina com as mudanças commitadas e enviadas à
`main`. Não crie tag no clone nem faça commit artificial de release. Quando o
usuário pedir explicitamente para publicar, acione:

```sh
make release VERSION=vX.Y.Z
```

O alvo local apenas solicita `.github/workflows/release.yml`. A pipeline roda
as validações e um snapshot completo, cria a tag anotada no commit remoto e só
então publica os binários e o `checksums.txt`. A IA deve acompanhar o workflow
até o fim e conferir a release, não apenas informar que o disparo funcionou.

Regras para agentes:

- nunca mova, apague ou recrie uma tag já enviada;
- se uma pipeline falhar, corrija a causa e publique uma versão nova;
- nunca use `--no-verify` para atravessar uma validação;
- confira a conclusão do workflow e os artefatos antes de afirmar que a
  release foi publicada.

## 9. Convenções que o revisor vai cobrar

- **`ID` é permanente.** Favoritos e estatísticas são gravados por ID em
  `~/.local/share/lealing/usage.json`. Renomear descarta o histórico do
  usuário.
- **`Summary` tem uma linha e termina com ponto.** Ele aparece na lista e na
  barra de status.
- **Comentários em português, identificadores em inglês.** É o padrão do
  repositório inteiro.
- **Comente o *porquê*, não o *quê*.** `// incrementa i` é ruído; `// o
  último absorve a sobra da divisão para a linha fechar exatamente na
  largura` é o que impede alguém de "simplificar" e quebrar o layout.
- **Nada de `panic` fora de `init`.** Uma tool quebrada degrada; ela não
  derruba o hub.
- **Erros descem, nunca sobem para `stdout`.** A TUI ocupa o terminal:
  qualquer `fmt.Println` corrompe o frame. Use `outbound.Logger`.
- **Driving adapter só chama porta de entrada.** Importar
  `core/port/outbound` na TUI é erro arquitetural, mesmo que compile.
- **Driven adapter não compõe driven adapter.** Receba interfaces no
  construtor e ligue as implementações no bootstrap.
- **Nada de `runtime.GOOS` fora de `bootstrap/platform.go`.** Quem decide o
  adapter é o composition root; espalhar o switch é como a lógica de
  plataforma vaza para o núcleo e para as telas.
- **Nada de dependência global escondida.** Construtores recebem caminhos,
  home, relógio e clientes relevantes; isso torna a escolha visível e
  testável.
- **Texto sem nome de sistema onde a tool serve os dois.** "a máquina", não
  "o Mac", no `Summary` de uma tool que roda nos dois — o `Detail` é o lugar
  de explicar a diferença.

## 10. Onde olhar quando travar

| Dúvida | Arquivo |
|---|---|
| Regras de dependência executáveis | `internal/architecture/dependencies_test.go` |
| Portas de entrada compartilhadas | `internal/core/port/inbound/inbound.go` |
| Portas de saída compartilhadas | `internal/core/port/outbound/outbound.go` |
| Caso de uso que orquestra portas | `internal/core/service/` |
| Tela mínima, do zero | `screen/sysinfo/sysinfo.go` |
| Tela com edição e confirmação | `screen/power/` |
| Vertical externa com gráficos e abas | `github.com/mateuslh/lealing-tools` · `cmd/token-usage/` |
| Contrato serializável | `sdk/protocol/` |
| Runtime Go de uma tool | `sdk/screen/` |
| Tela genérica da engine | `screen/plugin/` |
| Processo e handshake | `outbound/pluginprocess/` |
| Manifest e descoberta | `internal/toolmanifest/` · `outbound/externalcatalog/` |
| Parser testável isolado do sistema | `macos/power.go` + `macos/power_test.go` |
| Adapter de uma segunda plataforma | `windows/power.go` + `windows/power_test.go` |
| Porta com suporte parcial | `core/power/fields.go` (`Feature`, `Merge`) |
| Escolha do adapter por sistema | `internal/bootstrap/platform.go` |
| Cofre de credenciais por plataforma | `internal/platform/secrets/` |
| Ajuste novo na tela de configuração | `internal/core/settings/settings.go` (só declare o `Field`) |
| Estado do usuário e conflito | `internal/core/usersync/` |
| Validação catálogo ↔ tela ↔ runner | `internal/bootstrap/wiring.go` |
| Matriz de suporte do acervo | `internal/bootstrap/matrix.go` · `lealing -platforms` |
| Agregação e erro parcial externa | `lealing-tools/internal/tokenusage/tokens/` |
| Como tudo se conecta | `internal/bootstrap/bootstrap.go` |

## 11. Criando uma tool externa `screen-v1`

Este é o caminho padrão para uma tool nova que tenha interface própria. Uma
tool externa é instalada e atualizada sem recompilar a engine, mas continua
dentro do chrome dela. A engine nunca importa seu domínio, adapters ou model;
ela descobre o manifest e abre `screen/plugin` para qualquer ID. **Adicionar
uma externa não altera `internal/bootstrap` nem cria factory por ID.**

### 11.1 Quando usar externa e quando usar builtin

Use `screen-v1` para tabelas, filtros, ordenação, gráficos, abas, modais,
campos, autocomplete, seleção múltipla, scroll, progresso, mouse e operações
assíncronas. O model pode usar Bubble Tea e Bubbles por meio de `sdk/screen`.
Use builtin só para capacidades que pertencem à própria engine: home,
marketplace, instalador/rollback, requisitos, atualização da engine, chrome e
diagnóstico do runtime. Interfaces que tomam o terminal inteiro — SSH, Vim,
ncurses arbitrário, PTY de debugger — não cabem em `screen-v1`.

### 11.2 Estrutura e imports

No repositório próprio da tool:

```text
cmd/minha-tool/
  main.go
internal/minhatool/
  domain/
  adapter/
  ui/
manifests/minha-tool.yaml
```

O executável pode importar `github.com/mateuslh/lealing/sdk/protocol`,
`sdk/screen` e `sdk/component`. Nunca importe `github.com/mateuslh/lealing/internal`.
As oficiais usam `github.com/mateuslh/lealing-tools`; uma comunidade pode usar
seu próprio módulo. Um executável por tool é o contrato da v1.

### 11.3 Manifest

Comece por `manifests/token-usage.yaml` em `lealing-tools` e valide sem iniciar
o binário:

```sh
lealing -tool-validate ./manifests/minha-tool.yaml
```

O `apiVersion` é `lealing.dev/v1`; `runtime.kind` é `process`; o intervalo de
`runtime.protocol` precisa ser não vazio; `ui.mode` é `screen-v1`; e
`runtime.executable` é um nome simples, sem diretório, argumento ou shell. No
Windows o instalador acrescenta `.exe` ao nome do artefato. `summary` tem uma
linha e termina em ponto. ID, categoria, risk, SemVer, plataforma, requisitos,
capabilities e permissões são validados antes de a instalação ser ativada.

O ID é permanente. Favoritos e histórico continuam usando o mesmo `ToolID`
mesmo quando a implementação muda de repositório. Nunca publique uma nova
versão para corrigir um ID: isso cria outra tool.

### 11.4 Model e SDK

Implemente `screen.Model`:

```go
type Model interface {
    Init() tea.Cmd
    Update(tea.Msg) (screen.Model, tea.Cmd)
    View(protocol.Frame) string
}
```

Implemente opcionalmente `screen.Hinter`, `screen.Statuser`,
`screen.Capturer` e `screen.CursorProvider`. `sdk/component` contém tema,
painel, meter, barras, sparkline, alinhamento, truncamento e centralização.
Componentes Bubble Tea/Bubbles continuam disponíveis dentro do processo da
tool; a fronteira entre processos permanece formada apenas pelos DTOs JSON de
`sdk/protocol`.

O `main` reserva stdin/stdout para `screen.Run` e cria o model na `Factory`.
Use `screen.Session.Initialize` para frame, tema, plataforma, arquitetura,
diretórios, capabilities e permissões negociadas. Logs vão para stderr.

### 11.5 I/O assíncrono e progresso

`Update` e `View` continuam funções de estado/render. Arquivo, HTTP, processo,
cache e relógios lentos só rodam dentro de `tea.Cmd`, sempre com
`context.Context` e timeout. O comando devolve uma mensagem para `Update`,
que altera o estado; cada alteração gera um snapshot completo. Progresso é
uma sequência de mensagens do model, não RPC por célula. A v1 deliberadamente
não tem diff de frame.

Para ações do host, use `screen.Request` e declare a capability correspondente
no manifest: `navigation.back`, `notification.show`, `clipboard.write`,
`confirmation.request` ou `browser.open`. A engine rejeita requests não
negociadas. Confirmações destrutivas e o modal de `confirmation.request` são
da engine, nunca da tool.

### 11.6 Plataformas, requisitos e permissões

Declare cada artefato como `darwin-amd64`, `darwin-arm64`, `windows-amd64` ou
`windows-arm64`. Não detecte o sistema para escolher código da engine; dentro
da tool, prefira a plataforma recebida no handshake e injete a escolha nos
adapters. Um requisito de PATH usa somente `executable`, `name` e
`installHint`, sem argumentos.

Permissões são mínimas e explícitas:

```yaml
permissions:
  filesystem:
    read: [~/.minha-tool/dados]
    write: []
  network: false
  subprocess: false
```

Declare o que o binário realmente usa. A v1 transmite e apresenta essas
concessões, mas não promete sandbox de sistema operacional; uma tool maliciosa
ainda herda as permissões do usuário. Isso torna publicador, checksum e revisão
do artefato partes da fronteira de confiança.

### 11.7 ANSI, stdout e stderr

O `Body` pode conter Unicode, quebras de linha e somente SGR de cor, bold,
italic, underline e reset. OSC, clipboard, título, clear, cursor global,
alt-screen, mudança de modo e sequências desconhecidas são removidos pela
engine. Hints, status e texto de modal perdem todo ANSI. Teste payloads hostis,
não apenas layout feliz.

`stdout` pertence exclusivamente a `Content-Length: ...\r\n\r\n<json>`.
Um `fmt.Println` em stdout corrompe a sessão. Use stderr para logs; a engine o
captura no arquivo estruturado. Nunca use shell para iniciar subprocessos ou
interpolar entrada do usuário em uma linha de comando.

### 11.8 Testes e incompatibilidade

No mínimo, cubra domínio, adapters/parsers, model, as nove geometrias, estado
vazio/erro, tabs/scroll/capturing e o executável com o teste de conformidade do
protocolo. Renderize a vertical em 150×42 e 60×20. Para incompatibilidade,
teste um intervalo da tool sem interseção com o da engine; a mensagem precisa
mostrar os dois intervalos e nenhum processo deve ser iniciado quando o
manifest já prova a incompatibilidade.

Use helpers Go herméticos para testes de processo. Não dependa de shell, rede,
home real ou sleeps exatos. Pacotes com goroutines, pipes ou canais rodam com
`go test -race`.

### 11.9 Build, instalação local e rollback

No clone de `github.com/mateuslh/lealing-tools`, para a vertical oficial de
referência:

```sh
make build
lealing -tool-install ./bin
lealing -tools
lealing -tool-rollback token-usage
lealing -tool-remove token-usage
```

Depois de indexada, a mesma tool instala sem clone nem pacote manual:

```sh
lealing -marketplace
lealing -tool-install token-usage
lealing -tool-update token-usage
```

Na TUI, abra a builtin `marketplace`; ela consulta a mesma porta do core e
exibe canal, publicador, risco e permissões antes da confirmação global. A
consulta não participa do startup nem da busca da home.

Para outra tool, produza um diretório com `manifest.yaml` e o executável e
rode `lealing -tool-install DIRETORIO`. Instalar uma versão mais nova (ou
`-tool-update DIRETORIO`) preserva a ativa anterior. A troca de `active` é
atômica; rollback revalida manifest, plataforma e binário antes de apontar
para a versão anterior. Remoção move a instalação para `.trash` e informa o
caminho recuperável.

Gere os quatro artefatos oficiais com `CGO_ENABLED=0`; o workflow
`lealing-tools/.github/workflows/tools.yml` executa fmt, vet, test, race,
conformidade, cross-build, pacote por alvo e `checksums.txt`. Cada pacote contém
seu manifest.

### 11.10 Marketplace e versionamento

O índice usa o modelo de `internal/core/marketplace`: ID, versão, metadados de
exibição, permissões, publicador, URL do manifest, artefatos por alvo, SHA-256,
intervalo do protocolo, versão mínima da engine e canal `official`, `verified`
ou `community`. O seletor pega a versão mais nova compatível; nunca fixe a
engine a uma versão numérica da tool.

**Publicar não exige passar pelo registry oficial.** O marketplace é a soma
das origens habilitadas (`internal/core/marketplace/source.go`): além do índice
embutido, o usuário cadastra quantos repositórios quiser, remotos por HTTPS ou
locais em disco, com `lealing -source-add` ou pela aba de origens da tela.
Publicar de forma independente é hospedar um `index.json` que obedeça ao mesmo
contrato e divulgar o endereço.

Três invariantes sustentam isso e não podem ser afrouxadas ao mexer no core:

- **O canal pertence à engine.** `Origin.Trusted` só é marcado no composition
  root; entradas de qualquer outra origem são rebaixadas para `community` em
  `fetchOrigin`, mesmo que o JSON declare `official`.
- **Prioridade vence versão.** Em conflito de ID, `SelectLatest` ordena por
  `Origin.Priority` antes da versão, para que um índice paralelo não sequestre
  o nome de uma tool oficial publicando um número maior. A referência
  qualificada `origem/id` continua permitindo a escolha explícita.
- **Origem é unidade de falha.** Cada índice é buscado e validado isolado; um
  repositório fora do ar vira `SourceStatus.Err` e não impede os demais de
  aparecer.

Uma origem local (`OriginLocal`) aponta para um diretório com `index.json` e
artefatos relativos a ele. Ela não declara `sha256` — o artefato é o diretório
de build do próprio usuário —, e `marketplacefile` recusa travessia, symlink
para fora do repositório e artefato que não seja diretório. É o caminho de
desenvolvimento: instalar o build local pelo mesmo fluxo do índice público,
sem publicar release.

O registry público vive em `github.com/mateuslh/lealing-tools/marketplace`.
Para publicar nele, crie uma entrada imutável por versão em `marketplace/tools/`,
comece no canal `community`, rode `go run ./cmd/marketplace-index` e envie a
entrada mais o `index.json` gerado por pull request. `publishers.json` reserva
`official` e `verified`; CODEOWNERS exige revisão da política. A CI valida
SemVer, duplicatas, categorias, plataformas, HTTPS, checksums, protocolo,
permissões e a reprodução determinística do índice sem baixar ou executar o
artefato. Veja `lealing-tools/marketplace/README.md` e `schema.json`.

Versione tool e protocolo separadamente. Mudança compatível incrementa a tool;
mudança de fio aditiva mantém o intervalo; quebra de contrato cria outra
versão do protocolo e preserva negociação com versões antigas quando possível.
O cliente da engine não depende de GitHub: recebe uma URL HTTPS de índice,
baixa em streaming com limites, verifica o checksum do pacote, recusa
traversal e links na extração e só então entrega o diretório temporário ao
instalador local. A origem embutida usa o arquivo consolidado do repositório
das tools; `-marketplace-url` troca esse endereço, e `-source-add` acrescenta
outros repositórios sem substituir nenhum. As origens do usuário ficam em
`~/.config/lealing/marketplace-sources.json`, escritas atomicamente; confiança
e caráter embutido nunca são serializados, para que editar o arquivo à mão não
promova um índice de terceiro.

`token-usage` já foi extraída para `github.com/mateuslh/lealing-tools`. Migre as
outras verticais em mudanças independentes, conserve os IDs e importe apenas o
SDK público. O workflow do repositório das tools cria tag e release somente
depois de toda a matriz verde. Não publique nova versão sem autorização.

Antes de concluir, responda “sim” a todas:

- o core compila sem importar framework ou infraestrutura?
- a tela conhece só domínio e portas de entrada?
- todo I/O da tela está dentro de `tea.Cmd` com timeout?
- o caso de uso contém a política e o adapter apenas traduz o mundo externo?
- toda dependência variável entra pelo construtor?
- toda escolha de plataforma está em `bootstrap/platform.go`?
- catálogo, factory e runner usam o mesmo ID e `Kind`?
- parser, core, caso de uso, tela e geometria têm testes proporcionais?
- uma externa importa somente `sdk/` e seu próprio código, nunca `internal/`?
- a engine não tem import, factory nem composição concreta da externa?
- descoberta lê apenas manifests e o spawn acontece só ao abrir a tela?
- stdout da externa contém só protocolo e todo texto não-Body foi sanitizado?
- `fmt`, `vet`, `test`, `cross` e os dois renders passaram?
