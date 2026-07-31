// Package usage implementa os provedores de consumo de tokens lendo os logs
// que as CLIs de IA deixam no disco.
package usage

import (
	"bufio"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// maxLineBytes é o teto de uma linha do JSONL. Mensagens com imagens ou
// arquivos grandes embutidos passam de um megabyte, e o scanner padrão do
// bufio corta em 64 KB — o que descartaria silenciosamente o uso do turno.
const maxLineBytes = 16 << 20

// scanJSONL percorre todos os .jsonl sob root, entregando cada linha já
// decodificada ao visitor.
//
// onFile é chamado ao abrir cada arquivo e pode ser nil. Provedores cujo
// estado é por sessão (o Codex guarda o modelo corrente fora do evento de
// consumo) usam esse sinal para não vazar contexto de um arquivo no
// seguinte.
//
// Linhas malformadas são puladas em silêncio: são comuns quando a CLI está
// escrevendo no arquivo enquanto lemos, e abortar a varredura por causa
// disso zeraria o painel inteiro.
func scanJSONL(ctx context.Context, root string, onFile func(), visit func(map[string]any)) error {
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		// Diretório ausente significa que a CLI não está instalada. Isso é
		// um estado normal, não um erro a reportar.
		return nil
	}

	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // diretório ilegível: segue o baile
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		if onFile != nil {
			onFile()
		}
		scanFile(path, visit)
		return nil
	})
}

// scanFile lê um arquivo linha a linha.
func scanFile(path string, visit func(map[string]any)) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64<<10), maxLineBytes)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal(line, &obj); err != nil {
			continue
		}
		visit(obj)
	}
}

// --- Acesso tolerante ao JSON ------------------------------------------
//
// Os logs são de ferramentas de terceiros e mudam de versão para versão.
// Estes helpers tratam campo ausente e tipo inesperado como zero, para que
// uma mudança de formato degrade o número em vez de derrubar a leitura.

func str(obj map[string]any, key string) string {
	v, _ := obj[key].(string)
	return v
}

func obj(m map[string]any, key string) map[string]any {
	v, _ := m[key].(map[string]any)
	return v
}

// num converte um campo numérico. O JSON do Go decodifica todo número como
// float64, então a conversão passa sempre por lá.
func num(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return 0
	}
}

func float(m map[string]any, key string) (float64, bool) {
	v, ok := m[key].(float64)
	return v, ok
}

// parseTime aceita os dois formatos ISO-8601 que as CLIs usam (com e sem
// fração de segundo).
func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// dayOf extrai "YYYY-MM-DD" do início de um timestamp ISO.
func dayOf(ts string) string {
	if len(ts) < 10 {
		return ""
	}
	return ts[:10]
}

// projectOf reduz um caminho de trabalho ao nome da pasta.
func projectOf(cwd string) string {
	if cwd == "" {
		return "—"
	}
	if base := filepath.Base(cwd); base != "." && base != string(filepath.Separator) {
		return base
	}
	return "—"
}
