package selfupdate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	core "github.com/mateuslh/lealing/internal/core/selfupdate"
)

// Repo identifica o repositório de onde vêm as releases.
type Repo struct {
	Owner string
	Name  string
}

// String devolve "owner/name".
func (r Repo) String() string { return r.Owner + "/" + r.Name }

// Endereços públicos do GitHub. São campos, e não constantes embutidas nas
// funções, para que o teste possa apontar os dois para um servidor local e
// exercitar download, checksum e troca do binário de verdade — que é o
// caminho onde um erro custa o executável do usuário.
const (
	defaultAPIHost      = "https://api.github.com"
	defaultDownloadHost = "https://github.com"
)

// downloadURL monta a URL de um artefato de release. O caminho é estável e
// público, então não precisamos da API só para descobrir onde baixar.
func (r Repo) downloadURL(host, tag, asset string) string {
	return fmt.Sprintf("%s/%s/%s/releases/download/%s/%s", host, r.Owner, r.Name, tag, asset)
}

// GitHub implementa core.Releases sobre a API pública do GitHub.
type GitHub struct {
	repo   Repo
	api    string
	client *http.Client
}

var _ core.Releases = (*GitHub)(nil)

// NewGitHub monta o cliente de releases.
//
// O timeout é do cliente, e não só do contexto, porque uma conexão que abre e
// nunca responde não é cancelada pelo contexto da tela — e a tool ficaria
// "verificando…" para sempre.
func NewGitHub(repo Repo) *GitHub {
	return &GitHub{repo: repo, api: defaultAPIHost, client: &http.Client{Timeout: 15 * time.Second}}
}

// releaseJSON é o recorte da resposta da API que nos interessa.
type releaseJSON struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	HTMLURL     string    `json:"html_url"`
	PublishedAt time.Time `json:"published_at"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
}

// Latest implementa core.Releases.
func (g *GitHub) Latest(ctx context.Context) (core.Release, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/releases/latest", g.api, g.repo.Owner, g.repo.Name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return core.Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "lealing-selfupdate")

	resp, err := g.client.Do(req)
	if err != nil {
		return core.Release{}, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		// 404 aqui é o repositório sem nenhuma release publicada, não um
		// erro de rede: dizer isso poupa o usuário de procurar problema
		// onde não há.
		return core.Release{}, fmt.Errorf("%s ainda não tem releases publicadas", g.repo)
	default:
		return core.Release{}, fmt.Errorf("github respondeu %s", resp.Status)
	}

	var rel releaseJSON
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return core.Release{}, err
	}
	if rel.TagName == "" {
		return core.Release{}, fmt.Errorf("release sem tag em %s", g.repo)
	}

	return core.Release{
		Tag:         rel.TagName,
		Notes:       CleanNotes(rel.Body),
		PublishedAt: rel.PublishedAt,
		URL:         rel.HTMLURL,
	}, nil
}

// CleanNotes reduz as notas de release ao que cabe num painel de terminal:
// tira o rodapé de changelog automático e as linhas em branco repetidas.
//
// Exportada porque é a parte com regra de negócio embutida — e a que ganha um
// teste sem precisar de rede.
func CleanNotes(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")

	var kept []string
	blank := false
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		// O GoReleaser fecha as notas com o link de comparação entre tags;
		// numa tela de 60 colunas ele é uma URL gigante e nada mais.
		if strings.HasPrefix(trimmed, "**Full Changelog**") {
			break
		}
		if trimmed == "" {
			if blank || len(kept) == 0 {
				continue
			}
			blank = true
		} else {
			blank = false
		}
		kept = append(kept, trimmed)
	}

	return strings.TrimSpace(strings.Join(kept, "\n"))
}
