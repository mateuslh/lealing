# Criando tools no lealing — guia para agentes

Este documento é o contrato. Siga-o literalmente e a tool funciona; desvie e
o `registry` recusa a carga ou o teste de geometria falha.

**Antes de começar, rode `make test`.** Se já estiver quebrado, conserte ou
reporte antes de adicionar código — você precisa de uma linha de base verde
para saber que foi você quem quebrou algo.

---

## 1. Decida o tipo da tool

| Tipo | Quando | `Kind` | O que você escreve |
|---|---|---|---|
| **Nativa** | A tool tem interface própria dentro da TUI | `KindBuiltin` | Um pacote de núcleo, um adapter e uma tela |
| **Processo** | A tool é um binário externo | `KindProcess` | Só a declaração no catálogo |
| **Script** | A tool é um script interpretado | `KindScript` | Só a declaração no catálogo |
| **Remota** | A tool fala com um serviço | `KindRemote` | Declaração + adapter da porta |

As tools atuais são todas nativas. **Se em dúvida, leia `system-info` inteiro
primeiro** — é a mais simples e tem todas as peças.

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

### Se a tool precisa de um adapter nativo

Três regras, nesta ordem:

1. **Um pacote por sistema, nenhum com build tag.** `outbound/macos` e
   `outbound/windows` compilam em qualquer lugar — o que é específico é o
   processo que eles disparam, não o código Go. É isso que permite testar o
   parser do Windows na mesma suíte que roda no Mac.
2. **Registre o adapter em `bootstrap/platform.go`.** É o único switch por
   sistema operacional do programa. Um campo `nil` ali com a tool declarada
   como suportada no catálogo abre uma tela que estoura no primeiro `Read` —
   os dois lados precisam concordar.
3. **Exporte o parser e teste-o com uma amostra real.** `ParseCustom` (pmset),
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

---

## 3. Tool de processo (o caminho curto)

Adicione uma entrada em `internal/catalog/catalog.go`, dentro de
`Builtin.Provide`:

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

Depois **registre o runner** em `internal/bootstrap/bootstrap.go`. Sem isso a
tool aparece mas não faz nada (o `Placeholder` responde por ela).

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

Quatro arquivos, nesta ordem. Não pule etapas nem inverta a ordem — cada uma
depende da anterior compilar.

### 4.1 Núcleo — `internal/core/<tool>/<tool>.go`

Tipos e a porta de saída. **Regra absoluta: este pacote não importa nada além
da biblioteca padrão.** Sem `lipgloss`, sem `bubbletea`, sem `os/exec`.

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

// Reader é a porta de saída: alguém que sabe ler os volumes da máquina.
type Reader interface {
    Volumes(ctx context.Context) ([]Volume, error)
}
```

Cálculos derivados (`UsedPercent`) ficam **aqui**, não na tela. É o que
permite testá-los sem renderizar nada.

### 4.2 Adapter — `internal/adapter/outbound/<plataforma>/<tool>.go`

Implementa a porta falando com o mundo real. Um arquivo por plataforma que a
tool declara suportar — ver a seção 2.

```go
type DiskReader struct{}

var _ disco.Reader = (*DiskReader)(nil)   // trava o contrato em compile-time

func (d *DiskReader) Volumes(ctx context.Context) ([]disco.Volume, error) {
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

Três regras para adapters:

1. **Campo ilegível vira valor padrão, não erro.** Uma tela que se recusa a
   abrir porque um `sysctl` sumiu é pior que uma tela com um traço.
2. **Todo comando recebe `ctx`.** Use `exec.CommandContext`, nunca
   `exec.Command`.
3. **Nada de entrada do usuário em linha de shell.** Se precisar montar um
   comando, gere os tokens você mesmo e valide o que vier de fora — veja
   `safeUserName` em `macos/power.go`.

### 4.3 Tela — `internal/adapter/inbound/tui/screen/<tool>/<tool>.go`

Implemente `tui.Screen`:

```go
type Model struct {
    deps   tui.Deps
    reader disco.Reader     // a PORTA, nunca o adapter concreto
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
        vols, err := reader.Volumes(ctx)
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
diskReader := macos.NewDiskReader()

screens := tui.Screens{
    // ...
    "disk-usage": func() tui.Screen { return discoscreen.New(deps, diskReader) },
}
```

A chave é o `ID` da tool no catálogo. **Se não bater, a tool abre o
`Placeholder` e não faz nada** — e é exatamente esse o sintoma quando alguém
esquece este passo.

Por fim, declare a tool no catálogo (seção 3), com `Kind: domain.KindBuiltin`.

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
make cross                              # compila nas plataformas suportadas
make render SIZE=150x42                 # a home, com a tool nova na lista
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

---

## 8. Convenções que o revisor vai cobrar

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
  qualquer `fmt.Println` corrompe o frame. Use `port.Logger`.
- **Nada de `runtime.GOOS` fora de `bootstrap/platform.go`.** Quem decide o
  adapter é o composition root; espalhar o switch é como a lógica de
  plataforma vaza para o núcleo e para as telas.
- **Texto sem nome de sistema onde a tool serve os dois.** "a máquina", não
  "o Mac", no `Summary` de uma tool que roda nos dois — o `Detail` é o lugar
  de explicar a diferença.

## 9. Onde olhar quando travar

| Dúvida | Arquivo |
|---|---|
| Tela mínima, do zero | `screen/sysinfo/sysinfo.go` |
| Tela com edição e confirmação | `screen/power/` |
| Tela com dados agregados e gráficos | `screen/tokens/` |
| Parser testável isolado do sistema | `macos/power.go` + `macos/power_test.go` |
| Adapter de uma segunda plataforma | `windows/power.go` + `windows/power_test.go` |
| Porta com suporte parcial | `core/power/fields.go` (`Feature`, `Merge`) |
| Escolha do adapter por sistema | `internal/bootstrap/platform.go` |
| Matriz de suporte do acervo | `internal/bootstrap/matrix.go` · `lealing -platforms` |
| Agregação e erro parcial | `core/tokens/tokens.go` |
| Como tudo se conecta | `internal/bootstrap/bootstrap.go` |
