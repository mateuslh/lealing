// Package gitinsight é o domínio da tool "Radar Git do dev".
package gitinsight

import (
	"context"
	"path/filepath"
	"time"
)

// Branch é o estado de uma branch local contra seu upstream conhecido.
type Branch struct {
	Name        string
	Upstream    string
	Remote      string
	RemoteRef   string
	Hash        string
	Subject     string
	CommittedAt time.Time
	Ahead       int
	Behind      int
	Current     bool
	Gone        bool
}

// NeedsPush informa que há commits locais ausentes no upstream.
func (b Branch) NeedsPush() bool {
	return b.Upstream != "" && b.Remote != "" && !b.Gone && b.Ahead > 0
}

// Synced informa que o commit local já está contido no upstream conhecido.
func (b Branch) Synced() bool {
	return b.Upstream != "" && b.Remote != "" && !b.Gone && b.Ahead == 0
}

// CleanupCandidate informa que a branch não é a atual e todo o seu conteúdo
// já está no upstream. A decisão não apaga nada: apenas classifica.
func (b Branch) CleanupCandidate() bool {
	return !b.Current && b.Synced()
}

// WithoutUpstream separa branches cuja publicação não pode ser comprovada.
func (b Branch) WithoutUpstream() bool {
	return b.Upstream == "" || b.Remote == "" || b.Gone
}

// Repository é um clone encontrado abaixo do diretório dev.
type Repository struct {
	Name       string
	Path       string
	Relative   string
	Branches   []Branch
	DirtyFiles int
	Err        string
}

// CurrentBranch devolve a branch marcada pelo Git.
func (r Repository) CurrentBranch() string {
	for _, branch := range r.Branches {
		if branch.Current {
			return branch.Name
		}
	}
	return "HEAD destacado"
}

// PushBranches devolve branches com commits ainda não enviados.
func (r Repository) PushBranches() []Branch {
	return selectBranches(r.Branches, Branch.NeedsPush)
}

// CleanupBranches devolve branches locais já contidas no upstream.
func (r Repository) CleanupBranches() []Branch {
	return selectBranches(r.Branches, Branch.CleanupCandidate)
}

// SyncedCurrentBranches devolve a branch em uso quando ela já está publicada.
func (r Repository) SyncedCurrentBranches() []Branch {
	return selectBranches(r.Branches, func(branch Branch) bool {
		return branch.Current && branch.Synced()
	})
}

// UntrackedBranches devolve branches sem upstream válido.
func (r Repository) UntrackedBranches() []Branch {
	return selectBranches(r.Branches, Branch.WithoutUpstream)
}

// NeedsAttention informa se o clone tem algo acionável ou ilegível.
func (r Repository) NeedsAttention() bool {
	return len(r.PushBranches()) > 0 || len(r.UntrackedBranches()) > 0 ||
		r.DirtyFiles > 0 || r.Err != ""
}

func selectBranches(branches []Branch, keep func(Branch) bool) []Branch {
	selected := make([]Branch, 0, len(branches))
	for _, branch := range branches {
		if keep(branch) {
			selected = append(selected, branch)
		}
	}
	return selected
}

// Stats resume a árvore inteira.
type Stats struct {
	Repositories    int
	Branches        int
	NeedPush        int
	UnpushedCommits int
	Cleanup         int
	NoUpstream      int
	DirtyRepos      int
	Errors          int
}

// Report é um retrato de todos os clones abaixo de Root.
type Report struct {
	Root         string
	Repositories []Repository
	ScannedAt    time.Time
}

// UpdateState classifica o resultado da atualização de um clone.
type UpdateState uint8

const (
	UpdateCurrent UpdateState = iota
	UpdateUpdated
	UpdateSkipped
	UpdateFailed
)

// UpdateResult descreve o que aconteceu com a main/master de um clone.
type UpdateResult struct {
	Repository string
	Path       string
	Branch     string
	State      UpdateState
	Detail     string
}

// UpdateReport agrega a atualização segura de todos os clones.
type UpdateReport struct {
	Results []UpdateResult
}

// UpdateStats resume o resultado em lote.
type UpdateStats struct {
	Updated int
	Current int
	Skipped int
	Failed  int
}

// Stats calcula os totais sem duplicar estado no adapter.
func (r UpdateReport) Stats() UpdateStats {
	var stats UpdateStats
	for _, result := range r.Results {
		switch result.State {
		case UpdateUpdated:
			stats.Updated++
		case UpdateSkipped:
			stats.Skipped++
		case UpdateFailed:
			stats.Failed++
		default:
			stats.Current++
		}
	}
	return stats
}

// Stats calcula os indicadores sem guardar uma segunda fonte de verdade.
func (r Report) Stats() Stats {
	stats := Stats{Repositories: len(r.Repositories)}
	for _, repo := range r.Repositories {
		stats.Branches += len(repo.Branches)
		stats.NeedPush += len(repo.PushBranches())
		for _, branch := range repo.PushBranches() {
			stats.UnpushedCommits += branch.Ahead
		}
		stats.Cleanup += len(repo.CleanupBranches())
		stats.NoUpstream += len(repo.UntrackedBranches())
		if repo.DirtyFiles > 0 {
			stats.DirtyRepos++
		}
		if repo.Err != "" {
			stats.Errors++
		}
	}
	return stats
}

// WithRelativeNames preenche os rótulos relativos depois da descoberta.
func (r *Report) WithRelativeNames() {
	for i := range r.Repositories {
		relative, err := filepath.Rel(r.Root, r.Repositories[i].Path)
		if err != nil {
			relative = filepath.Base(r.Repositories[i].Path)
		}
		r.Repositories[i].Relative = filepath.ToSlash(relative)
		r.Repositories[i].Name = filepath.Base(r.Repositories[i].Path)
	}
}

// Scanner é a porta de saída da tool.
type Scanner interface {
	Scan(ctx context.Context) (Report, error)
}

// Manager reúne a leitura e as ações explícitas oferecidas pela tool.
type Manager interface {
	Scanner
	Fetch(ctx context.Context, repoPath string) error
	Push(ctx context.Context, repoPath string, branch Branch) error
	DeleteLocalBranch(ctx context.Context, repoPath, branch string) error
	UpdateAll(ctx context.Context) (UpdateReport, error)
}
