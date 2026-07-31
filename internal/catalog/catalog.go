// Package catalog declara as tools embutidas do lealing.
//
// Cada domínio é um port.ToolProvider independente. Essa granularidade é o
// que permite o acervo crescer para centenas de itens sem virar um arquivo
// de milhares de linhas — e é o que permitirá, depois, carregar tools de
// manifestos externos com exatamente o mesmo contrato.
//
// Para adicionar uma tool, veja AGENTS.md na raiz do repositório.
package catalog

import (
	"context"

	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/port"
)

// Categorias do acervo. A ordem de declaração é a ordem na sidebar.
var (
	System      = domain.Category{ID: "system", Name: "Sistema", Glyph: "⌬", Accent: 0, Order: 10, Description: "a máquina local: energia, hardware, diagnóstico"}
	AI          = domain.Category{ID: "ai", Name: "IA", Glyph: "✧", Accent: 1, Order: 20, Description: "consumo, custos e ferramentas de modelos"}
	Network     = domain.Category{ID: "network", Name: "Rede", Glyph: "⇄", Accent: 2, Order: 30, Description: "conectividade e diagnóstico de rede"}
	Media       = domain.Category{ID: "media", Name: "Mídia", Glyph: "◈", Accent: 3, Order: 40, Description: "áudio, vídeo e imagem"}
	Development = domain.Category{ID: "dev", Name: "Desenvolvimento", Glyph: "⚙", Accent: 4, Order: 50, Description: "build, testes e fluxo de trabalho"}
	Utilities   = domain.Category{ID: "utilities", Name: "Utilitários", Glyph: "▤", Accent: 5, Order: 60, Description: "ferramentas de uso geral"}
)

// categories é o conjunto declarado ao registry. Categorias sem tool não
// aparecem na navegação — a sidebar filtra as vazias —, então declarar todas
// desde já custa nada e deixa o encaixe pronto para as próximas tools.
var categories = []domain.Category{System, AI, Network, Media, Development, Utilities}

// Builtin é o provider das tools nativas do lealing: as que têm tela própria
// dentro da TUI, em vez de dispararem um processo externo.
type Builtin struct{}

var _ port.ToolProvider = (*Builtin)(nil)

// Name implementa port.ToolProvider.
func (Builtin) Name() string { return "builtin" }

// Provide implementa port.ToolProvider.
//
// Os IDs são estáveis e casam com os do Arteus Tools, de onde estas tools
// vieram: favoritos e estatísticas seguem o ID, então mudá-lo descartaria o
// histórico de uso.
func (Builtin) Provide(context.Context) ([]domain.Tool, []domain.Category, error) {
	tools := []domain.Tool{
		{
			ID:        "power-control",
			Name:      "Controle de Energia",
			Summary:   "Defina se a máquina dorme na bateria e no carregador, e ajustes avançados de energia.",
			Detail:    "No macOS lê e escreve o pmset, com o painel completo — Power Nap, standby, hibernação — e oferece dispensar a senha com uma regra de sudoers restrita ao pmset. No Windows edita o plano de energia ativo via powercfg, que cobre só os três tempos de inatividade e não pede elevação; a tela esconde o que a plataforma não grava.",
			Category:  System.ID,
			Kind:      domain.KindBuiltin,
			Risk:      domain.RiskCaution,
			Platforms: domain.Darwin | domain.Windows,
			Glyph:     "⏻",
			Keywords:  []string{"pmset", "powercfg", "energia", "bateria", "dormir", "sleep", "power", "hibernar"},
			Tags:      []domain.Tag{"sistema", "energia"},
		},
		{
			ID:        "system-info",
			Name:      "Informações do Sistema",
			Summary:   "Veja versão do sistema, chip, memória, tempo ligado e estado da bateria.",
			Detail:    "Leitura somente-consulta: sysctl, sw_vers e pmset no macOS; CIM/WMI no Windows. Nenhum dado sai da máquina.",
			Category:  System.ID,
			Kind:      domain.KindBuiltin,
			Risk:      domain.RiskSafe,
			Platforms: domain.Darwin | domain.Windows,
			Glyph:     "◎",
			Keywords:  []string{"sysctl", "wmi", "hardware", "cpu", "memória", "uptime", "bateria", "macos", "windows"},
			Tags:      []domain.Tag{"sistema", "leitura"},
		},
		{
			// Sem Platforms: as CLIs de IA escrevem os mesmos JSONL em
			// qualquer sistema, e a tool só lê arquivo.
			ID:       "token-usage",
			Name:     "Uso de Tokens",
			Summary:  "Consumo de tokens e custo estimado, somando todas as sessões, por modelo, dia e projeto.",
			Detail:   "Varre ~/.claude/projects e ~/.codex/sessions, normaliza o consumo relatado por cada CLI e estima o custo pela tabela de preços. Os preços da OpenAI são estimativas.",
			Category: AI.ID,
			Kind:     domain.KindBuiltin,
			Risk:     domain.RiskSafe,
			Glyph:    "◔",
			Keywords: []string{"claude", "codex", "custo", "tokens", "gasto", "usage", "preço"},
			Tags:     []domain.Tag{"ia", "leitura", "custos"},
		},
		{
			// Sem Platforms: o cofre muda de sistema para sistema — chaveiro
			// no macOS, arquivo no Windows e no Linux —, mas isso é escolha
			// do adapter, e a tool funciona nos três.
			ID:       "claude-accounts",
			Name:     "Contas do Claude Code",
			Summary:  "Guarde as sessões de várias contas do Claude Code e alterne entre elas.",
			Detail:   "Salva a credencial da conta em uso junto com a identidade gravada em ~/.claude.json e devolve o par escolhido no lugar de onde a CLI lê — chaveiro no macOS, ~/.claude/.credentials.json no Windows e no Linux. Os tokens ficam no cofre do sistema; o índice em disco guarda só e-mail, plano e data. Feche as sessões do `claude` antes de trocar: ao sair, a CLI regrava a conta em que estava.",
			Category: AI.ID,
			Kind:     domain.KindBuiltin,
			Risk:     domain.RiskCaution,
			Glyph:    "⇆",
			Keywords: []string{"claude", "conta", "login", "sessão", "trocar", "alternar", "switch", "account", "perfil", "credencial"},
			Tags:     []domain.Tag{"ia", "conta"},
		},
		{
			// Sem Platforms: trocar o próprio binário é HTTP, checksum e
			// rename — a única tool do acervo que é igual nos três sistemas
			// sem precisar de um adapter por plataforma.
			ID:       "self-update",
			Name:     "Atualizar o lealing",
			Summary:  "Compare a versão instalada com o último release e atualize sem sair da TUI.",
			Detail:   "Consulta a última release publicada no GitHub e atualiza pelo caminho por onde o lealing foi instalado: um binário de release é baixado, conferido contra o checksums.txt publicado e substituído em disco; um clone recebe `git pull --ff-only` e é recompilado. Nunca instala um arquivo cujo checksum não bate, e mantém o binário anterior quando o build falha.",
			Category: Utilities.ID,
			Kind:     domain.KindBuiltin,
			Risk:     domain.RiskCaution,
			Glyph:    "⇪",
			Keywords: []string{"update", "atualizar", "upgrade", "versão", "release", "github"},
			Tags:     []domain.Tag{"manutenção"},
		},
	}

	return tools, categories, nil
}

// Providers devolve todos os providers embutidos.
//
// Novos providers entram aqui. Um provider pode vir de qualquer lugar —
// código, manifesto em disco, serviço remoto —, desde que satisfaça
// port.ToolProvider.
func Providers() []port.ToolProvider {
	return []port.ToolProvider{Builtin{}}
}
