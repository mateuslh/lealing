// Package devkit contém os contratos das ferramentas de engenharia.
//
// As transformações e integrações ficam atrás de Runner para que a TUI não
// conheça HTTP, DNS, TLS, aleatoriedade ou formatos de serialização.
package devkit

import (
	"context"
	"fmt"
)

// Tool identifica uma operação da bancada.
type Tool string

const (
	ToolHTTP     Tool = "http-probe"
	ToolNetwork  Tool = "network-inspector"
	ToolJSON     Tool = "json-lab"
	ToolJWT      Tool = "jwt-inspector"
	ToolCIDR     Tool = "cidr-calculator"
	ToolCodec    Tool = "codec-lab"
	ToolChecksum Tool = "checksum-lab"
	ToolUUID     Tool = "uuid-generator"
)

// Mode é uma variação selecionável da mesma operação.
type Mode struct {
	ID    string
	Label string
}

// Definition descreve a interação sem acoplar o domínio a componentes TUI.
type Definition struct {
	Tool        Tool
	ToolID      string
	Name        string
	Title       string
	Summary     string
	Detail      string
	Glyph       string
	InputLabel  string
	Placeholder string
	Action      string
	Modes       []Mode
}

var definitions = []Definition{
	{
		Tool:        ToolHTTP,
		ToolID:      "http-probe",
		Name:        "Sonda HTTP",
		Title:       "sonda HTTP",
		Summary:     "Inspecione status, latência, headers e uma prévia segura de uma URL.",
		Detail:      "Executa GET ou HEAD com timeout, limite de corpo e redirecionamentos controlados. Mostra status, protocolo, destino final, tipo, tamanho e headers relevantes sem salvar nada em disco.",
		Glyph:       "↗",
		InputLabel:  "URL",
		Placeholder: "https://api.exemplo.com/health",
		Action:      "consultar",
		Modes:       []Mode{{ID: "GET", Label: "GET"}, {ID: "HEAD", Label: "HEAD"}},
	},
	{
		Tool:        ToolNetwork,
		ToolID:      "network-inspector",
		Name:        "Inspetor DNS e TLS",
		Title:       "inspetor DNS e TLS",
		Summary:     "Resolva DNS ou examine validade, emissor e nomes de um certificado TLS.",
		Detail:      "No modo DNS resolve endereços IP e nomes canônicos. No modo TLS abre uma conexão somente para ler o certificado apresentado, sua cadeia, validade, emissor e SANs.",
		Glyph:       "◎",
		InputLabel:  "Host",
		Placeholder: "api.exemplo.com",
		Action:      "inspecionar",
		Modes:       []Mode{{ID: "dns", Label: "DNS"}, {ID: "tls", Label: "TLS :443"}},
	},
	{
		Tool:        ToolJSON,
		ToolID:      "json-lab",
		Name:        "Laboratório JSON",
		Title:       "laboratório JSON",
		Summary:     "Valide, formate ou compacte JSON com diagnóstico preciso de sintaxe.",
		Detail:      "Processa o conteúdo localmente com o parser da biblioteca padrão. Preserva números sem convertê-los para ponto flutuante e informa a posição de erros de sintaxe.",
		Glyph:       "{",
		InputLabel:  "JSON",
		Placeholder: `{"servico":"pagamentos","replicas":3}`,
		Action:      "processar",
		Modes:       []Mode{{ID: "format", Label: "formatar"}, {ID: "minify", Label: "compactar"}},
	},
	{
		Tool:        ToolJWT,
		ToolID:      "jwt-inspector",
		Name:        "Inspetor JWT",
		Title:       "inspetor JWT",
		Summary:     "Decodifique header, claims e validade temporal de um JWT sem enviar o token.",
		Detail:      "Decodifica Base64URL e apresenta algoritmo, issuer, subject, audience, emissão e expiração. Não verifica a assinatura: a tela deixa essa limitação explícita para evitar uma falsa garantia de autenticidade.",
		Glyph:       "◈",
		InputLabel:  "Token",
		Placeholder: "eyJhbGciOi...eyJzdWIiOi...assinatura",
		Action:      "decodificar",
		Modes:       []Mode{{ID: "claims", Label: "claims"}, {ID: "raw", Label: "JSON bruto"}},
	},
	{
		Tool:        ToolCIDR,
		ToolID:      "cidr-calculator",
		Name:        "Calculadora CIDR",
		Title:       "calculadora CIDR",
		Summary:     "Calcule rede, faixa, broadcast e capacidade de blocos IPv4 ou IPv6.",
		Detail:      "Aceita um endereço com prefixo CIDR e calcula a rede normalizada, primeiro e último endereço, máscara e quantidade total. Em IPv4 também mostra broadcast e hosts tradicionalmente utilizáveis.",
		Glyph:       "▦",
		InputLabel:  "Bloco",
		Placeholder: "10.42.16.25/20",
		Action:      "calcular",
		Modes:       []Mode{{ID: "details", Label: "detalhes"}},
	},
	{
		Tool:        ToolCodec,
		ToolID:      "codec-lab",
		Name:        "Codecs Base64 e URL",
		Title:       "codecs Base64 e URL",
		Summary:     "Codifique e decodifique Base64, Base64URL e componentes de URL.",
		Detail:      "Transforma texto localmente entre UTF-8, Base64 padrão, Base64URL sem padding e percent-encoding de query. A saída nunca é executada nem interpretada como comando.",
		Glyph:       "⇄",
		InputLabel:  "Texto",
		Placeholder: "texto ou valor codificado",
		Action:      "transformar",
		Modes: []Mode{
			{ID: "b64-encode", Label: "Base64 +"}, {ID: "b64-decode", Label: "Base64 −"},
			{ID: "b64url-encode", Label: "Base64URL +"}, {ID: "b64url-decode", Label: "Base64URL −"},
			{ID: "url-encode", Label: "URL +"}, {ID: "url-decode", Label: "URL −"},
		},
	},
	{
		Tool:        ToolChecksum,
		ToolID:      "checksum-lab",
		Name:        "Gerador de Checksums",
		Title:       "gerador de checksums",
		Summary:     "Gere hashes SHA-256, SHA-512 ou SHA-1 de texto sem tocar em serviços externos.",
		Detail:      "Calcula o digest do texto exatamente como digitado. SHA-1 é oferecido apenas para compatibilidade com sistemas legados e aparece acompanhado de um aviso para não ser usado como assinatura ou proteção de senha.",
		Glyph:       "#",
		InputLabel:  "Texto",
		Placeholder: "conteúdo para calcular o digest",
		Action:      "gerar",
		Modes:       []Mode{{ID: "sha256", Label: "SHA-256"}, {ID: "sha512", Label: "SHA-512"}, {ID: "sha1", Label: "SHA-1 legado"}},
	},
	{
		Tool:        ToolUUID,
		ToolID:      "uuid-generator",
		Name:        "Gerador de UUID",
		Title:       "gerador de UUID",
		Summary:     "Gere lotes de UUID v4 criptograficamente aleatórios ou UUID v7 ordenáveis.",
		Detail:      "Usa crypto/rand. UUID v4 é totalmente aleatório; UUID v7 incorpora o timestamp em milissegundos e preserva aleatoriedade no restante, sendo adequado a chaves distribuídas ordenáveis.",
		Glyph:       "✣",
		InputLabel:  "Quantidade",
		Placeholder: "1",
		Action:      "gerar",
		Modes:       []Mode{{ID: "v4", Label: "UUID v4"}, {ID: "v7", Label: "UUID v7"}},
	},
}

// Definitions devolve cópias das definições na ordem editorial.
func Definitions() []Definition {
	out := make([]Definition, len(definitions))
	copy(out, definitions)
	for i := range out {
		out[i].Modes = append([]Mode(nil), definitions[i].Modes...)
	}
	return out
}

// DefinitionFor resolve a definição de uma tool.
func DefinitionFor(tool Tool) (Definition, bool) {
	for _, definition := range definitions {
		if definition.Tool == tool {
			return definition, true
		}
	}
	return Definition{}, false
}

// Request é uma execução imutável capturada pela tela.
type Request struct {
	Tool  Tool
	Mode  string
	Input string
}

// Row é um campo estruturado do resultado.
type Row struct {
	Label string
	Value string
}

// Result é uma resposta renderizável sem conhecimento de estilos.
type Result struct {
	Title   string
	Summary string
	Rows    []Row
	Body    string
	Warning string
}

// Runner é a porta de saída única da bancada. Mesmo transformações locais
// passam por ela para que a tela nunca saiba qual operação toca a rede.
type Runner interface {
	Run(ctx context.Context, request Request) (Result, error)
}

// ValidateRequest protege o dispatch contra perfis e modos inventados.
func ValidateRequest(request Request) error {
	definition, ok := DefinitionFor(request.Tool)
	if !ok {
		return fmt.Errorf("tool desconhecida: %q", request.Tool)
	}
	for _, mode := range definition.Modes {
		if mode.ID == request.Mode {
			return nil
		}
	}
	return fmt.Errorf("modo %q não existe em %s", request.Mode, definition.Name)
}
