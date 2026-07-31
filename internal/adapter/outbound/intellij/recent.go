// Package intellij registra projetos na lista de recentes do IntelliJ IDEA.
package intellij

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// RecentProjects escreve no arquivo de preferências da instalação mais nova.
type RecentProjects struct {
	home     string
	patterns []string
}

// NewRecentProjects monta o writer com os padrões nativos da plataforma.
func NewRecentProjects(home string, patterns ...string) *RecentProjects {
	return &RecentProjects{home: home, patterns: patterns}
}

// Add registra todos os diretórios sem alterar qual projeto estava aberto.
func (r *RecentProjects) Add(ctx context.Context, paths []string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	file, err := LatestRecentProjects(r.patterns...)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(file)
	if err != nil {
		return fmt.Errorf("ler recentes do IntelliJ: %w", err)
	}

	updated, changed, err := AddEntries(raw, r.home, paths, time.Now())
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}

	info, err := os.Stat(file)
	if err != nil {
		return fmt.Errorf("ler permissões dos recentes do IntelliJ: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), ".recentProjects-*.xml")
	if err != nil {
		return fmt.Errorf("preparar recentes do IntelliJ: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("preservar permissões dos recentes do IntelliJ: %w", err)
	}
	if _, err := tmp.Write(updated); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("gravar recentes do IntelliJ: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sincronizar recentes do IntelliJ: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("fechar recentes do IntelliJ: %w", err)
	}
	if err := os.Rename(tmpName, file); err != nil {
		return fmt.Errorf("substituir recentes do IntelliJ: %w", err)
	}
	return nil
}

// LatestRecentProjects escolhe o arquivo modificado mais recentemente.
func LatestRecentProjects(patterns ...string) (string, error) {
	var files []string
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return "", fmt.Errorf("padrão do IntelliJ inválido: %w", err)
		}
		files = append(files, matches...)
	}
	if len(files) == 0 {
		return "", errors.New("IntelliJ IDEA não encontrado; abra a IDE uma vez e tente novamente")
	}

	slices.SortFunc(files, func(a, b string) int {
		ai, aerr := os.Stat(a)
		bi, berr := os.Stat(b)
		switch {
		case aerr != nil && berr != nil:
			return strings.Compare(a, b)
		case aerr != nil:
			return 1
		case berr != nil:
			return -1
		default:
			return bi.ModTime().Compare(ai.ModTime())
		}
	})
	return files[0], nil
}

// AddEntries insere entradas no mapa de projetos preservando todo o XML que
// a versão instalada do IntelliJ escreveu e que esta tool não conhece.
func AddEntries(raw []byte, home string, paths []string, now time.Time) ([]byte, bool, error) {
	const closingMap = "</map>"
	at := strings.Index(string(raw), closingMap)
	if at < 0 {
		return nil, false, errors.New("formato de recentes do IntelliJ não reconhecido")
	}
	// Insere antes da indentação da tag de fechamento. Se entrasse exatamente
	// no "<", os espaços que já pertenciam ao </map> sobrariam antes da
	// primeira entrada e o arquivo ficaria torto a cada execução.
	if lineStart := strings.LastIndex(string(raw[:at]), "\n"); lineStart >= 0 {
		at = lineStart + 1
	}

	var entries strings.Builder
	changed := false
	for i, projectPath := range paths {
		key, err := projectKey(home, projectPath)
		if err != nil {
			return nil, false, err
		}
		escaped := escapeAttr(key)
		if strings.Contains(string(raw), `key="`+escaped+`"`) {
			continue
		}
		changed = true
		entries.WriteString(recentEntry(escaped, filepath.Base(projectPath), now.Add(time.Duration(i)*time.Millisecond)))
	}
	if !changed {
		return raw, false, nil
	}

	out := make([]byte, 0, len(raw)+entries.Len())
	out = append(out, raw[:at]...)
	out = append(out, entries.String()...)
	out = append(out, raw[at:]...)
	return out, true, nil
}

func projectKey(home, projectPath string) (string, error) {
	abs, err := filepath.Abs(projectPath)
	if err != nil {
		return "", fmt.Errorf("normalizar caminho do projeto: %w", err)
	}
	rel, err := filepath.Rel(home, abs)
	if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "$USER_HOME$/" + filepath.ToSlash(rel), nil
	}
	return filepath.ToSlash(abs), nil
}

func recentEntry(key, name string, at time.Time) string {
	stamp := at.UnixMilli()
	sum := sha256.Sum256([]byte(key + at.UTC().Format(time.RFC3339Nano)))
	workspaceID := base64.RawURLEncoding.EncodeToString(sum[:])[:27]

	return fmt.Sprintf(`        <entry key="%s">
          <value>
            <RecentProjectMetaInfo frameTitle="%s" projectWorkspaceId="%s">
              <option name="activationTimestamp" value="%d" />
              <option name="projectOpenTimestamp" value="%d" />
            </RecentProjectMetaInfo>
          </value>
        </entry>
`, key, escapeAttr(name), workspaceID, stamp, stamp)
}

func escapeAttr(s string) string {
	var out strings.Builder
	_ = xml.EscapeText(&out, []byte(s))
	return strings.ReplaceAll(out.String(), "&#39;", "&apos;")
}
