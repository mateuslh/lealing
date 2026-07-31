// Package githubclone descobre e clona famílias de repositórios do GitHub.
package githubclone

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/mateuslh/lealing/internal/core/repoclone"
)

// RecentWriter registra diretórios clonados na IDE.
type RecentWriter interface {
	Add(ctx context.Context, paths []string) error
}

// Manager implementa o fluxo usando gh para descoberta e git para clone.
type Manager struct {
	home   string
	recent RecentWriter
	run    func(context.Context, string, ...string) ([]byte, error)
}

var _ repoclone.Manager = (*Manager)(nil)

// New monta o manager.
func New(home string, recent RecentWriter) *Manager {
	return &Manager{home: home, recent: recent, run: run}
}

// Discover implementa repoclone.Manager.
func (m *Manager) Discover(ctx context.Context, rawURL string) (repoclone.Plan, error) {
	source, err := repoclone.ParseSource(rawURL)
	if err != nil {
		return repoclone.Plan{}, err
	}

	out, err := m.run(ctx, "gh", "repo", "list", source.Owner,
		"--limit", "10000", "--json", repositoryFields)
	if err != nil {
		return repoclone.Plan{}, fmt.Errorf(
			"não foi possível listar os repositórios de %s; confira `gh auth status`: %s",
			source.Owner, commandError(out, err))
	}

	repos, err := ParseRepositories(out, source)
	if err != nil {
		return repoclone.Plan{}, err
	}
	if len(repos) == 0 {
		return repoclone.Plan{}, fmt.Errorf(
			"nenhum repositório com o prefixo %q foi encontrado em %s",
			source.Prefix, source.Owner)
	}

	return repoclone.Plan{
		Source:       source,
		Destination:  filepath.Join(m.home, "dev", source.Prefix),
		Repositories: repos,
	}, nil
}

const repositoryFields = "name,url,sshUrl,description,visibility,primaryLanguage,defaultBranchRef,updatedAt,diskUsage,isArchived"

type rawRepository struct {
	Name             string      `json:"name"`
	URL              string      `json:"url"`
	SSHURL           string      `json:"sshUrl"`
	Description      string      `json:"description"`
	Visibility       string      `json:"visibility"`
	PrimaryLanguage  *namedValue `json:"primaryLanguage"`
	DefaultBranchRef *namedValue `json:"defaultBranchRef"`
	UpdatedAt        time.Time   `json:"updatedAt"`
	DiskUsage        int         `json:"diskUsage"`
	IsArchived       bool        `json:"isArchived"`
}

type namedValue struct {
	Name string `json:"name"`
}

// ParseRepositories filtra a resposta do gh e preserva o protocolo escolhido.
func ParseRepositories(raw []byte, source repoclone.Source) ([]repoclone.Repository, error) {
	var listed []rawRepository
	if err := json.Unmarshal(raw, &listed); err != nil {
		return nil, fmt.Errorf("resposta inválida do GitHub: %w", err)
	}

	repos := make([]repoclone.Repository, 0, len(listed))
	for _, item := range listed {
		if !repoclone.MatchesPrefix(item.Name, source.Prefix) {
			continue
		}
		repo := repositoryOf(item, source)
		if repo.CloneURL == "" {
			continue
		}
		repos = append(repos, repo)
	}

	slices.SortFunc(repos, func(a, b repoclone.Repository) int {
		if strings.EqualFold(a.Name, source.Prefix) && !strings.EqualFold(b.Name, source.Prefix) {
			return -1
		}
		if strings.EqualFold(b.Name, source.Prefix) && !strings.EqualFold(a.Name, source.Prefix) {
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return repos, nil
}

// Resolve implementa repoclone.Manager, enriquecendo um repo adicionado na
// revisão com os mesmos metadados da descoberta inicial.
func (m *Manager) Resolve(ctx context.Context, source repoclone.Source, raw string) (repoclone.Repository, error) {
	additional, err := repoclone.ParseAdditionalSource(raw, source)
	if err != nil {
		return repoclone.Repository{}, err
	}
	fullName := additional.Owner + "/" + additional.Repository
	out, err := m.run(ctx, "gh", "repo", "view", fullName, "--json", repositoryFields)
	if err != nil {
		return repoclone.Repository{}, fmt.Errorf(
			"não foi possível consultar %s: %s", fullName, commandError(out, err))
	}

	var item rawRepository
	if err := json.Unmarshal(out, &item); err != nil {
		return repoclone.Repository{}, fmt.Errorf("resposta inválida do GitHub: %w", err)
	}
	repo := repositoryOf(item, additional)
	if repo.Name == "" || repo.CloneURL == "" {
		return repoclone.Repository{}, errors.New("o GitHub não devolveu dados de clone para o repositório")
	}
	return repo, nil
}

func repositoryOf(item rawRepository, source repoclone.Source) repoclone.Repository {
	cloneURL := item.URL
	if source.Protocol == repoclone.ProtocolSSH {
		cloneURL = item.SSHURL
	}
	repo := repoclone.Repository{
		Owner:       source.Owner,
		Name:        item.Name,
		CloneURL:    cloneURL,
		Description: item.Description,
		Visibility:  item.Visibility,
		UpdatedAt:   item.UpdatedAt,
		DiskUsageKB: item.DiskUsage,
		Archived:    item.IsArchived,
	}
	if item.PrimaryLanguage != nil {
		repo.Language = item.PrimaryLanguage.Name
	}
	if item.DefaultBranchRef != nil {
		repo.DefaultBranch = item.DefaultBranchRef.Name
	}
	return repo
}

// Clone implementa repoclone.Manager.
func (m *Manager) Clone(ctx context.Context, plan repoclone.Plan) (repoclone.Result, error) {
	result := repoclone.Result{Destination: plan.Destination}
	if err := os.MkdirAll(plan.Destination, 0o755); err != nil {
		return result, fmt.Errorf("criar %s: %w", plan.Destination, err)
	}

	projects := make([]string, 0, len(plan.Repositories))
	failures := 0
	for _, repo := range plan.Repositories {
		outcome := repoclone.Outcome{
			Name: repo.Name,
			Path: filepath.Join(plan.Destination, repo.Name),
		}

		switch _, err := os.Stat(outcome.Path); {
		case err == nil:
			if _, gitErr := os.Stat(filepath.Join(outcome.Path, ".git")); gitErr == nil {
				outcome.Existing = true
				projects = append(projects, outcome.Path)
			} else {
				outcome.Err = errors.New("a pasta já existe e não é um clone Git")
				failures++
			}
		case !errors.Is(err, os.ErrNotExist):
			outcome.Err = fmt.Errorf("consultar destino: %w", err)
			failures++
		default:
			out, cloneErr := m.run(ctx, "git", "clone", "--", repo.CloneURL, outcome.Path)
			if cloneErr != nil {
				// O diretório não existia antes deste comando. Remover só
				// esse clone incompleto permite tentar de novo sem confundi-lo
				// com um projeto já presente.
				_ = os.RemoveAll(outcome.Path)
				outcome.Err = errors.New(commandError(out, cloneErr))
				failures++
			} else {
				projects = append(projects, outcome.Path)
			}
		}
		result.Outcomes = append(result.Outcomes, outcome)
	}

	if len(projects) > 0 && m.recent != nil {
		if err := m.recent.Add(ctx, projects); err != nil {
			result.RecentWarning = err.Error()
		}
	}
	if failures > 0 {
		return result, fmt.Errorf("%d de %d repositórios não foram clonados",
			failures, len(plan.Repositories))
	}
	return result, nil
}

func run(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

func commandError(out []byte, err error) string {
	text := strings.TrimSpace(string(out))
	if text == "" {
		return err.Error()
	}
	if line, _, ok := strings.Cut(text, "\n"); ok {
		return strings.TrimSpace(line)
	}
	return text
}
