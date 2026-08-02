// Package catalog declara as tools embutidas do lealing.
//
// Cada domínio é um outbound.ToolProvider independente. Essa granularidade é o
// que permite o acervo crescer para centenas de itens sem virar um arquivo
// de milhares de linhas — e é o que permitirá, depois, carregar tools de
// manifestos externos com exatamente o mesmo contrato.
//
// Para adicionar uma tool, veja AGENTS.md na raiz do repositório.
package catalog

import (
	"context"

	"github.com/mateuslh/lealing/internal/core/devkit"
	"github.com/mateuslh/lealing/internal/core/domain"
	"github.com/mateuslh/lealing/internal/core/port/outbound"
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

var _ outbound.ToolProvider = (*Builtin)(nil)

// Name implementa outbound.ToolProvider.
func (Builtin) Name() string { return "builtin" }

// Provide implementa outbound.ToolProvider.
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
		},
		{
			ID:       "account-sync",
			Name:     "Conta e Sincronização",
			Summary:  "Conecte sua conta do GitHub e leve suas preferências para outra máquina.",
			Detail:   "Entra pelo device flow do GitHub — nenhum segredo fica no binário — e guarda favoritos, estatísticas de uso, origens do marketplace e a lista de tools instaladas em um repositório privado da sua conta. Cada seção liga e desliga separadamente, o envio e a descida são explícitos, e divergência entre máquinas vira pergunta em vez de sobrescrita silenciosa. Credenciais de outras ferramentas nunca são sincronizadas.",
			Category: Utilities.ID,
			Kind:     domain.KindBuiltin,
			Risk:     domain.RiskCaution,
			Glyph:    "☁",
			Keywords: []string{"conta", "login", "github", "sync", "sincronizar", "backup", "preferências", "perfil"},
		},
		{
			ID:        "clone-repo-bradesco",
			Name:      "Clone Repo Bradesco",
			Summary:   "Clone no diretório dev toda a família GitHub de um projeto e adicione-a aos recentes do IntelliJ.",
			Detail:    "Aceita link de página ou URL HTTPS/SSH de clone. Usa a sessão autenticada do GitHub CLI (`gh auth login`) para listar no mesmo owner o projeto principal, o -config e todos os repositórios com o mesmo prefixo. Antes de clonar, permite incluir, excluir, remover da proposta e adicionar outros repos, mostrando descrição, visibilidade, linguagem, branch, atualização e tamanho. Cria ~/dev/<projeto>/<repositório> sem sobrescrever pastas existentes e registra cada diretório no IntelliJ IDEA.",
			Category:  Development.ID,
			Kind:      domain.KindBuiltin,
			Risk:      domain.RiskCaution,
			Platforms: domain.Darwin | domain.Windows,
			Requirements: []domain.Requirement{
				{Executable: "git", Name: "Git", InstallHint: "instale o Git e confirme que `git` está no PATH"},
				{Executable: "gh", Name: "GitHub CLI", InstallHint: "instale o GitHub CLI e rode `gh auth login`"},
			},
			Glyph:    "⇣",
			Keywords: []string{"bradesco", "github", "git", "clone", "repo", "repositório", "config", "intellij", "idea", "dev"},
			Tags:     []domain.Tag{"bradesco"},
		},
		{
			ID:       "git-dev-radar",
			Name:     "Radar Git do dev",
			Summary:  "Encontre commits não enviados, branches locais já publicadas e clones alterados em todo o diretório dev.",
			Detail:   "Varre recursivamente ~/dev no macOS e %USERPROFILE%\\dev no Windows, usando até dez processos Git em paralelo. Cada clone mostra branches para push, locais já publicadas, branches sem upstream e arquivos alterados; navegue entre esses tipos com ←/→ ou 1–5. Permite atualizar remotos, publicar uma branch pendente, remover com segurança uma branch local já publicada ou atualizar a main/master de todos os clones com `u`. A atualização geral usa somente fast-forward, não troca a branch em uso e ignora clones alterados ou com commits locais. Push, remoção e atualização geral abrem um modal de confirmação; a remoção usa somente `git branch -d`.",
			Category: Development.ID,
			Kind:     domain.KindBuiltin,
			Risk:     domain.RiskDestructive,
			Requirements: []domain.Requirement{
				{Executable: "git", Name: "Git", InstallHint: "instale o Git e confirme que `git` está no PATH"},
			},
			Glyph:    "⑂",
			Keywords: []string{"git", "branch", "push", "pull", "fetch", "main", "master", "atualizar todos", "ahead", "upstream", "remote", "cleanup", "limpeza", "dev", "repositórios"},
		},
	}
	tools = append(tools, devkitTools()...)

	return tools, categories, nil
}

// devkitTools traduz as definições funcionais do núcleo para itens do
// catálogo. A identidade fica em um só lugar, enquanto categoria e busca
// continuam sendo responsabilidade editorial do acervo.
func devkitTools() []domain.Tool {
	keywords := map[devkit.Tool][]string{
		devkit.ToolHTTP:     {"http", "https", "api", "rest", "status", "header", "latência", "health"},
		devkit.ToolNetwork:  {"dns", "tls", "ssl", "certificado", "cname", "ip", "san", "rede"},
		devkit.ToolJSON:     {"json", "formatar", "validar", "pretty", "minify", "compactar"},
		devkit.ToolJWT:      {"jwt", "token", "claims", "oauth", "oidc", "bearer", "base64url"},
		devkit.ToolCIDR:     {"cidr", "subnet", "rede", "máscara", "broadcast", "ipv4", "ipv6"},
		devkit.ToolCodec:    {"base64", "base64url", "url encode", "decode", "percent encoding", "codec"},
		devkit.ToolChecksum: {"sha256", "sha512", "sha1", "hash", "digest", "checksum"},
		devkit.ToolUUID:     {"uuid", "guid", "v4", "v7", "identificador", "aleatório"},
	}

	definitions := devkit.Definitions()
	tools := make([]domain.Tool, 0, len(definitions))
	for _, definition := range definitions {
		category := Development.ID
		if definition.Tool == devkit.ToolHTTP || definition.Tool == devkit.ToolNetwork || definition.Tool == devkit.ToolCIDR {
			category = Network.ID
		}
		tools = append(tools, domain.Tool{
			ID:       domain.ToolID(definition.ToolID),
			Name:     definition.Name,
			Summary:  definition.Summary,
			Detail:   definition.Detail,
			Category: category,
			Kind:     domain.KindBuiltin,
			Risk:     domain.RiskSafe,
			Glyph:    definition.Glyph,
			Keywords: keywords[definition.Tool],
		})
	}
	return tools
}

// Providers devolve todos os providers embutidos.
//
// Novos providers entram aqui. Um provider pode vir de qualquer lugar —
// código, manifesto em disco, serviço remoto —, desde que satisfaça
// outbound.ToolProvider.
func Providers() []outbound.ToolProvider {
	return []outbound.ToolProvider{Builtin{}}
}

// Categories devolve uma cópia das categorias aceitas por manifests externos.
func Categories() []domain.Category {
	return append([]domain.Category(nil), categories...)
}

// ReservedIDs são os IDs que uma instalação externa nunca pode sombrear.
func ReservedIDs() []domain.ToolID {
	tools, _, _ := (Builtin{}).Provide(context.Background())
	ids := make([]domain.ToolID, len(tools))
	for i, tool := range tools {
		ids[i] = tool.ID
	}
	return ids
}
