// Package domain concentra o modelo de negócio do lealing.
//
// Este pacote é o centro do hexágono: não importa nada além da biblioteca
// padrão. Nenhum detalhe de terminal, de arquivo ou de processo entra aqui.
package domain

import (
	"sort"
	"strings"
	"time"
)

// ToolID identifica uma tool de forma estável e legível ("git/branch-cleanup").
// É o único identificador persistido: estatísticas, favoritos e histórico
// referenciam tools por ID, nunca por índice ou ponteiro.
type ToolID string

// String implementa fmt.Stringer.
func (id ToolID) String() string { return string(id) }

// Namespace devolve o prefixo antes da primeira barra ("git" em "git/status").
// Vazio quando a tool não usa namespace.
func (id ToolID) Namespace() string {
	if i := strings.IndexByte(string(id), '/'); i > 0 {
		return string(id)[:i]
	}
	return ""
}

// ToolRef identifica uma tool dentro do provider que a publicou. O ID sozinho
// não basta: dois providers podem declarar o mesmo valor e a prioridade do
// catálogo pode fazer outro deles vencer depois de uma recarga.
type ToolRef struct {
	Host string
	ID   ToolID
}

// String devolve a referência qualificada usada em diagnóstico.
func (r ToolRef) String() string {
	if r.Host == "" {
		return string(r.ID)
	}
	return r.Host + "/" + string(r.ID)
}

// Kind descreve como uma tool é executada. O catálogo é agnóstico quanto a
// isso; quem decide o runner apropriado é a porta de saída ToolRunner.
type Kind uint8

const (
	// KindProcess dispara um binário externo. O manifest decide se ele usa o
	// protocolo interativo da TUI ou uma execução simples.
	KindProcess Kind = iota
	// KindScript executa um script interpretado (shell, python, node...).
	KindScript
	// KindRemote fala com um serviço remoto (HTTP, gRPC, SSH).
	KindRemote
)

var kindNames = [...]string{"process", "script", "remote"}

// String implementa fmt.Stringer.
func (k Kind) String() string {
	if int(k) < len(kindNames) {
		return kindNames[k]
	}
	return "unknown"
}

// Risk classifica o potencial de estrago de uma tool. A camada de
// apresentação usa isso para pedir confirmação e para colorir badges.
type Risk uint8

const (
	// RiskSafe é somente leitura ou trivialmente reversível.
	RiskSafe Risk = iota
	// RiskCaution escreve em disco ou muda estado local recuperável.
	RiskCaution
	// RiskDestructive apaga dados ou toca em ambientes externos.
	RiskDestructive
)

var riskNames = [...]string{"safe", "caution", "destructive"}

// String implementa fmt.Stringer.
func (r Risk) String() string {
	if int(r) < len(riskNames) {
		return riskNames[r]
	}
	return "unknown"
}

// NeedsConfirmation indica se a execução deve passar por um diálogo.
func (r Risk) NeedsConfirmation() bool { return r >= RiskDestructive }

// Tool é a unidade central do catálogo: uma capacidade que o usuário pode
// invocar. É um agregado imutável — o registry monta, o resto só lê.
type Tool struct {
	// Host é a identidade estável da origem que publicou a tool. Providers
	// simples herdam ToolProvider.Name; o provider de instalações lê o valor
	// que o instalador persistiu a partir do índice escolhido.
	Host     string
	ID       ToolID
	Name     string
	Summary  string // uma linha, cabe em lista
	Detail   string // markdown livre, mostrado no painel de inspeção
	Category CategoryID
	Kind     Kind
	Risk     Risk

	// Platforms são os sistemas em que a tool roda. O zero-value significa
	// "todos": a maioria das tools é portável, e obrigar cada uma a repetir
	// AllPlatforms só faria o campo ser copiado sem pensar. Quem depende de
	// um adapter nativo declara — e some do catálogo nas outras plataformas.
	Platforms Platform

	// Requirements são executáveis externos que precisam estar no PATH antes
	// de iniciar a tool. Declarar aqui permite à TUI explicar a ausência antes
	// que uma tela abra e falhe no primeiro comando.
	Requirements []Requirement

	// Glyph é o ícone exibido na lista. Vazio herda o glyph da categoria.
	Glyph string
	// Keywords alimentam a busca sem poluir o nome exibido (sinônimos,
	// nomes de binários, siglas).
	Keywords []string
	Tags     []Tag
	// Version é opcional e puramente informativo.
	Version string
	// Experimental marca tools instáveis, destacadas visualmente.
	Experimental bool

	// Runtime existe somente para tools instaladas fora da engine. O catálogo
	// continua declarativo: guardar o caminho não inicia nem carrega o binário.
	Runtime *ExternalRuntime
}

// Ref devolve a identidade completa da tool no catálogo.
func (t Tool) Ref() ToolRef { return ToolRef{Host: t.Host, ID: t.ID} }

// ExternalRuntime descreve como abrir uma tool instalada. O caminho já foi
// confinado pelo provider ao diretório versionado; a existência só é conferida
// no spawn, para descoberta nunca executar nem depender do binário.
type ExternalRuntime struct {
	InstallDir   string
	Executable   string
	ProtocolMin  int
	ProtocolMax  int
	UIMode       string
	Capabilities []string
	Permissions  ToolPermissions
}

// ToolPermissions são as concessões declaradas no manifest e apresentadas à
// tool no handshake.
type ToolPermissions struct {
	ReadPaths  []string
	WritePaths []string
	Network    bool
	Subprocess bool
}

// Interactive informa se a tool usa uma sessão screen-v1 dentro do chrome.
func (t Tool) Interactive() bool {
	return t.Runtime != nil && t.Runtime.UIMode == "screen-v1"
}

// SupportedPlatforms resolve o zero-value de Platforms para AllPlatforms.
// Use este método, nunca o campo cru: ler o campo direto trata uma tool
// portável como uma tool que não roda em lugar nenhum.
func (t Tool) SupportedPlatforms() Platform {
	if t.Platforms == 0 {
		return AllPlatforms
	}
	return t.Platforms
}

// RunsOn informa se a tool roda na plataforma indicada.
func (t Tool) RunsOn(p Platform) bool { return t.SupportedPlatforms().Has(p) }

// Title devolve o rótulo de exibição, caindo para o ID quando não há nome.
func (t Tool) Title() string {
	if t.Name != "" {
		return t.Name
	}
	return string(t.ID)
}

// SearchCorpus concatena os campos indexáveis. O Searcher recebe esta string
// já normalizada, o que mantém a lógica de ranqueamento fora do domínio sem
// espalhar conhecimento sobre a estrutura de Tool.
func (t Tool) SearchCorpus() string {
	var b strings.Builder
	b.Grow(len(t.Name) + len(t.Summary) + 64)
	b.WriteString(t.Name)
	b.WriteByte(' ')
	b.WriteString(string(t.ID))
	b.WriteByte(' ')
	b.WriteString(t.Summary)
	for _, k := range t.Keywords {
		b.WriteByte(' ')
		b.WriteString(k)
	}
	for _, requirement := range t.Requirements {
		b.WriteByte(' ')
		b.WriteString(requirement.Executable)
		b.WriteByte(' ')
		b.WriteString(requirement.Name)
	}
	for _, tag := range t.Tags {
		b.WriteByte(' ')
		b.WriteString(string(tag))
	}
	return strings.ToLower(b.String())
}

// Requirement descreve uma ferramenta externa necessária.
type Requirement struct {
	// Executable é o nome procurado no PATH, sem caminho nem argumentos.
	Executable string
	// Name é o nome amigável mostrado ao usuário. Vazio usa Executable.
	Name string
	// InstallHint explica como resolver a ausência sem tentar instalar nada
	// automaticamente.
	InstallHint string
}

// Label devolve o nome de exibição do requisito.
func (r Requirement) Label() string {
	if r.Name != "" {
		return r.Name
	}
	return r.Executable
}

// HasTag informa se a tool carrega a tag indicada.
func (t Tool) HasTag(want Tag) bool {
	for _, got := range t.Tags {
		if got == want {
			return true
		}
	}
	return false
}

// Tag é um rótulo transversal às categorias ("git", "aws", "leitura").
type Tag string

// CategoryID identifica uma categoria do catálogo.
type CategoryID string

// Category agrupa tools na navegação. A ordenação é explícita (Order) para
// que a home não dependa de ordem alfabética nem de mapa iterado.
type Category struct {
	ID          CategoryID
	Name        string
	Description string
	Glyph       string
	// Accent é um índice na paleta do tema, não uma cor concreta: o domínio
	// não conhece terminal nem hex.
	Accent int
	Order  int
}

// Usage guarda o comportamento observado de uma tool para um usuário.
// É estado mutável, vive fora do agregado Tool e é persistido em separado.
type Usage struct {
	// Host é a origem qualificada da tool. Mantê-lo junto do ID impede que
	// favoritos e histórico atravessem para uma implementação homônima.
	Host     string
	ToolID   ToolID
	Runs     int
	LastRun  time.Time
	Favorite bool
}

// Score combina frequência e recência (decaimento exponencial com meia-vida
// de uma semana) para ordenar as sugestões da home. Favoritas recebem um
// piso alto para nunca sumirem do topo.
func (u Usage) Score(now time.Time) float64 {
	var score float64
	if u.Runs > 0 && !u.LastRun.IsZero() {
		const halfLife = 7 * 24 * time.Hour
		age := now.Sub(u.LastRun)
		if age < 0 {
			age = 0
		}
		decay := 1.0
		for age >= halfLife {
			decay /= 2
			age -= halfLife
		}
		score = float64(u.Runs) * decay
	}
	if u.Favorite {
		score += 1000
	}
	return score
}

// SortCategories ordena in-place por Order e, em empate, por nome.
func SortCategories(cs []Category) {
	sort.SliceStable(cs, func(i, j int) bool {
		if cs[i].Order != cs[j].Order {
			return cs[i].Order < cs[j].Order
		}
		return cs[i].Name < cs[j].Name
	})
}
