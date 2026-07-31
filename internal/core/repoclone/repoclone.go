// Package repoclone é o domínio da tool "Clone Repo Bradesco".
package repoclone

import (
	"context"
	"errors"
	"net/url"
	"path"
	"strings"
	"time"
)

// Protocol preserva a forma de autenticação escolhida no link de entrada.
type Protocol uint8

const (
	ProtocolHTTPS Protocol = iota
	ProtocolSSH
)

// Source identifica o repositório usado como semente da descoberta.
type Source struct {
	Owner      string
	Repository string
	Prefix     string
	Protocol   Protocol
}

// Repository é um repositório encontrado no mesmo owner do GitHub.
type Repository struct {
	Owner         string
	Name          string
	CloneURL      string
	Description   string
	Visibility    string
	Language      string
	DefaultBranch string
	UpdatedAt     time.Time
	DiskUsageKB   int
	Archived      bool
}

// Plan é a operação revisável antes de qualquer escrita em disco.
type Plan struct {
	Source       Source
	Destination  string
	Repositories []Repository
}

// Outcome descreve o que aconteceu com um repositório.
type Outcome struct {
	Name     string
	Path     string
	Existing bool
	Err      error
}

// Result agrega clones completos e falhas parciais.
type Result struct {
	Destination   string
	Outcomes      []Outcome
	RecentWarning string
}

// Manager é a porta de saída da tool.
type Manager interface {
	Discover(ctx context.Context, rawURL string) (Plan, error)
	Resolve(ctx context.Context, source Source, raw string) (Repository, error)
	Clone(ctx context.Context, plan Plan) (Result, error)
}

var (
	// ErrEmptyURL informa que nenhum link foi digitado.
	ErrEmptyURL = errors.New("informe o link do repositório")
	// ErrNotGitHub evita mandar entradas arbitrárias para processos externos.
	ErrNotGitHub = errors.New("o link precisa apontar para github.com")
	// ErrInvalidRepository cobre owner ou nome ausentes e caminhos perigosos.
	ErrInvalidRepository = errors.New("link de repositório inválido")
	// ErrDifferentOwner mantém os projetos adicionais dentro da organização
	// que foi revisada na descoberta.
	ErrDifferentOwner = errors.New("o repositório adicional precisa pertencer ao mesmo owner")
)

// ParseSource aceita URL de página, clone HTTPS e clone SSH do GitHub.
func ParseSource(raw string) (Source, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Source{}, ErrEmptyURL
	}

	var (
		host     string
		repoPath string
		protocol = ProtocolHTTPS
	)

	if strings.HasPrefix(raw, "git@") {
		protocol = ProtocolSSH
		before, after, ok := strings.Cut(strings.TrimPrefix(raw, "git@"), ":")
		if !ok {
			return Source{}, ErrInvalidRepository
		}
		host, repoPath = before, after
	} else {
		candidate := raw
		if !strings.Contains(candidate, "://") {
			candidate = "https://" + candidate
		}
		u, err := url.Parse(candidate)
		if err != nil || u.Hostname() == "" {
			return Source{}, ErrInvalidRepository
		}
		host, repoPath = u.Hostname(), u.Path
		if strings.EqualFold(u.Scheme, "ssh") {
			protocol = ProtocolSSH
		}
	}

	if !strings.EqualFold(host, "github.com") {
		return Source{}, ErrNotGitHub
	}

	parts := strings.Split(strings.Trim(repoPath, "/"), "/")
	if len(parts) < 2 {
		return Source{}, ErrInvalidRepository
	}
	owner := parts[0]
	name := strings.TrimSuffix(parts[1], ".git")
	if !safeSegment(owner) || !safeSegment(name) {
		return Source{}, ErrInvalidRepository
	}

	prefix := name
	if strings.HasSuffix(strings.ToLower(name), "-config") {
		prefix = name[:len(name)-len("-config")]
	}
	if prefix == "" {
		return Source{}, ErrInvalidRepository
	}
	return Source{Owner: owner, Repository: name, Prefix: prefix, Protocol: protocol}, nil
}

// ParseAdditionalSource aceita o nome simples ou uma URL do mesmo owner.
func ParseAdditionalSource(raw string, base Source) (Source, error) {
	raw = strings.TrimSpace(raw)
	if safeSegment(raw) {
		return Source{
			Owner: base.Owner, Repository: raw,
			Prefix: raw, Protocol: base.Protocol,
		}, nil
	}

	source, err := ParseSource(raw)
	if err != nil {
		return Source{}, err
	}
	if !strings.EqualFold(source.Owner, base.Owner) {
		return Source{}, ErrDifferentOwner
	}
	// Toda a família usa o protocolo escolhido na primeira URL, para não
	// misturar duas formas de autenticação no mesmo clone.
	source.Protocol = base.Protocol
	return source, nil
}

// MatchesPrefix segue a semântica literal pedida pela tool: todo repositório
// cujo nome começa pelo projeto, independentemente de maiúsculas.
func MatchesPrefix(name, prefix string) bool {
	return strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix))
}

// safeSegment restringe os dois segmentos que também viram nomes de pasta.
func safeSegment(s string) bool {
	if s == "" || s == "." || s == ".." || path.Base(s) != s {
		return false
	}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return false
	}
	return true
}
