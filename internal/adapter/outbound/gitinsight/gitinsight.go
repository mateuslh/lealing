// Package gitinsight lê os clones Git abaixo do diretório dev.
package gitinsight

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	core "github.com/mateuslh/lealing/internal/core/gitinsight"
)

// Scanner implementa core.Manager usando somente o executável git.
type Scanner struct {
	root string
	now  func() time.Time
	run  func(context.Context, string, ...string) ([]byte, error)
}

var _ core.Manager = (*Scanner)(nil)

const gitWorkers = 10

// New monta o scanner.
func New(root string, now func() time.Time) *Scanner {
	if now == nil {
		now = time.Now
	}
	return &Scanner{root: root, now: now, run: run}
}

// Scan implementa core.Scanner.
func (s *Scanner) Scan(ctx context.Context) (core.Report, error) {
	paths, err := FindRepositories(s.root)
	if err != nil {
		return core.Report{Root: s.root}, err
	}

	report := core.Report{
		Root:         s.root,
		Repositories: make([]core.Repository, len(paths)),
		ScannedAt:    s.now(),
	}

	// Dez processos aproveitam SSDs atuais sem abrir uma quantidade irrestrita
	// de git.exe quando a árvore corporativa contém centenas de clones.
	sem := make(chan struct{}, gitWorkers)
	var wg sync.WaitGroup
	for i, path := range paths {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				report.Repositories[i] = core.Repository{Path: path, Err: ctx.Err().Error()}
				return
			}
			report.Repositories[i] = s.scanRepository(ctx, path)
		}()
	}
	wg.Wait()
	report.WithRelativeNames()

	if err := ctx.Err(); err != nil {
		return report, err
	}
	return report, nil
}

// UpdateAll atualiza os remotos e avança main/master sem trocar a branch em
// uso, criar merge commit ou sobrescrever commits locais.
func (s *Scanner) UpdateAll(ctx context.Context) (core.UpdateReport, error) {
	paths, err := FindRepositories(s.root)
	if err != nil {
		return core.UpdateReport{}, err
	}

	report := core.UpdateReport{Results: make([]core.UpdateResult, len(paths))}
	sem := make(chan struct{}, gitWorkers)
	var wg sync.WaitGroup
	for i, path := range paths {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				report.Results[i] = s.updateResult(path, "", core.UpdateFailed, ctx.Err().Error())
				return
			}
			report.Results[i] = s.updateRepository(ctx, path)
		}()
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return report, err
	}
	return report, nil
}

func (s *Scanner) updateRepository(ctx context.Context, path string) core.UpdateResult {
	if err := s.Fetch(ctx, path); err != nil {
		return s.updateResult(path, "", core.UpdateFailed, err.Error())
	}

	repo := s.scanRepository(ctx, path)
	if repo.Err != "" {
		return s.updateResult(path, "", core.UpdateFailed, repo.Err)
	}
	branch, ok := baseBranch(repo.Branches)
	if !ok {
		return s.updateResult(path, "", core.UpdateSkipped, "sem branch main ou master")
	}
	if branch.WithoutUpstream() || branch.RemoteRef == "" {
		return s.updateResult(path, branch.Name, core.UpdateSkipped, "branch sem upstream válido")
	}
	if branch.Ahead > 0 {
		return s.updateResult(path, branch.Name, core.UpdateSkipped,
			commitLabel(branch.Ahead)+" local pendente")
	}
	if branch.Behind == 0 {
		return s.updateResult(path, branch.Name, core.UpdateCurrent, "já estava em dia")
	}
	if repo.DirtyFiles > 0 {
		return s.updateResult(path, branch.Name, core.UpdateSkipped,
			"working tree com "+changeLabel(repo.DirtyFiles))
	}

	var out []byte
	var err error
	if branch.Current {
		out, err = s.run(ctx, "git", "-C", path, "merge", "--ff-only", "--", branch.Upstream)
	} else {
		refspec := branch.RemoteRef + ":refs/heads/" + branch.Name
		out, err = s.run(ctx, "git", "-C", path, "fetch", "--no-tags", "--",
			branch.Remote, refspec)
	}
	if err != nil {
		return s.updateResult(path, branch.Name, core.UpdateFailed, commandError(out, err))
	}
	return s.updateResult(path, branch.Name, core.UpdateUpdated,
		"avançou "+commitLabel(branch.Behind))
}

func commitLabel(count int) string {
	if count == 1 {
		return "1 commit"
	}
	return strconv.Itoa(count) + " commits"
}

func changeLabel(count int) string {
	if count == 1 {
		return "1 alteração"
	}
	return strconv.Itoa(count) + " alterações"
}

func baseBranch(branches []core.Branch) (core.Branch, bool) {
	for _, name := range []string{"main", "master"} {
		for _, branch := range branches {
			if branch.Name == name {
				return branch, true
			}
		}
	}
	return core.Branch{}, false
}

func (s *Scanner) updateResult(
	path, branch string,
	state core.UpdateState,
	detail string,
) core.UpdateResult {
	relative, err := filepath.Rel(s.root, path)
	if err != nil {
		relative = filepath.Base(path)
	}
	return core.UpdateResult{
		Repository: filepath.ToSlash(relative),
		Path:       path,
		Branch:     branch,
		State:      state,
		Detail:     detail,
	}
}

// FindRepositories encontra clones recursivamente e não entra no conteúdo de
// um clone já reconhecido.
func FindRepositories(root string) ([]string, error) {
	if _, err := os.Stat(root); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("diretório dev não encontrado: %s", root)
		}
		return nil, fmt.Errorf("ler diretório dev: %w", err)
	}

	var repos []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			// Uma pasta ilegível não esconde as irmãs; o git dela aparecerá
			// como ausente, não como falha total da auditoria.
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.IsDir() {
			return nil
		}
		if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
			repos = append(repos, path)
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("varrer diretório dev: %w", err)
	}
	sort.Strings(repos)
	return repos, nil
}

const branchFormat = "%(HEAD)%00%(refname:short)%00%(upstream:short)%00%(upstream:remotename)%00%(upstream:remoteref)%00%(upstream:track)%00%(objectname:short)%00%(committerdate:unix)%00%(subject)"

func (s *Scanner) scanRepository(ctx context.Context, path string) core.Repository {
	repo := core.Repository{Path: path}

	branches, branchErr := s.run(ctx, "git", "-C", path, "for-each-ref",
		"--sort=refname", "--format="+branchFormat, "refs/heads/")
	if branchErr == nil {
		repo.Branches = ParseBranches(branches)
	}

	status, statusErr := s.run(ctx, "git", "-C", path, "status",
		"--porcelain=v1", "-z", "--untracked-files=normal")
	if statusErr == nil {
		repo.DirtyFiles = CountChanges(status)
	}

	switch {
	case branchErr != nil:
		repo.Err = commandError(branches, branchErr)
	case statusErr != nil:
		repo.Err = commandError(status, statusErr)
	}
	return repo
}

var (
	aheadPattern  = regexp.MustCompile(`ahead (\d+)`)
	behindPattern = regexp.MustCompile(`behind (\d+)`)
)

// ParseBranches converte a saída estável do for-each-ref.
func ParseBranches(raw []byte) []core.Branch {
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	branches := make([]core.Branch, 0, len(lines))
	for _, line := range lines {
		fields := strings.Split(strings.TrimSuffix(line, "\r"), "\x00")
		if len(fields) != 9 || fields[1] == "" {
			continue
		}
		branch := core.Branch{
			Current:   strings.TrimSpace(fields[0]) == "*",
			Name:      fields[1],
			Upstream:  fields[2],
			Remote:    fields[3],
			RemoteRef: fields[4],
			Hash:      fields[6],
			Subject:   fields[8],
			Gone:      strings.Contains(fields[5], "gone"),
		}
		branch.Ahead = trackCount(aheadPattern, fields[5])
		branch.Behind = trackCount(behindPattern, fields[5])
		if unix, err := strconv.ParseInt(fields[7], 10, 64); err == nil && unix > 0 {
			branch.CommittedAt = time.Unix(unix, 0)
		}
		branches = append(branches, branch)
	}
	return branches
}

// Fetch atualiza e poda as referências remotas do clone selecionado.
func (s *Scanner) Fetch(ctx context.Context, repoPath string) error {
	path, err := s.safeRepositoryPath(repoPath)
	if err != nil {
		return err
	}
	out, err := s.run(ctx, "git", "-C", path, "fetch", "--all", "--prune")
	if err != nil {
		return fmt.Errorf("atualizar remotos: %s", commandError(out, err))
	}
	return nil
}

// Push publica somente a branch escolhida no upstream já conhecido.
func (s *Scanner) Push(ctx context.Context, repoPath string, branch core.Branch) error {
	path, err := s.safeRepositoryPath(repoPath)
	if err != nil {
		return err
	}
	if branch.Remote == "" || branch.RemoteRef == "" ||
		!strings.HasPrefix(branch.RemoteRef, "refs/heads/") {
		return errors.New("branch sem upstream publicável")
	}
	refspec := "refs/heads/" + branch.Name + ":" + branch.RemoteRef
	out, err := s.run(ctx, "git", "-C", path, "push", "--", branch.Remote, refspec)
	if err != nil {
		return fmt.Errorf("publicar branch: %s", commandError(out, err))
	}
	return nil
}

// DeleteLocalBranch remove apenas branches que o próprio Git considera
// totalmente integradas; nunca força a exclusão.
func (s *Scanner) DeleteLocalBranch(ctx context.Context, repoPath, branch string) error {
	path, err := s.safeRepositoryPath(repoPath)
	if err != nil {
		return err
	}
	if strings.TrimSpace(branch) == "" {
		return errors.New("branch local vazia")
	}
	out, err := s.run(ctx, "git", "-C", path, "branch", "-d", "--", branch)
	if err != nil {
		return fmt.Errorf("remover branch local: %s", commandError(out, err))
	}
	return nil
}

func (s *Scanner) safeRepositoryPath(repoPath string) (string, error) {
	root, err := filepath.Abs(s.root)
	if err != nil {
		return "", fmt.Errorf("resolver raiz dev: %w", err)
	}
	path, err := filepath.Abs(repoPath)
	if err != nil {
		return "", fmt.Errorf("resolver caminho do clone: %w", err)
	}
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("repositório fora da raiz dev")
	}
	return path, nil
}

func trackCount(pattern *regexp.Regexp, track string) int {
	match := pattern.FindStringSubmatch(track)
	if len(match) != 2 {
		return 0
	}
	value, _ := strconv.Atoi(match[1])
	return value
}

// CountChanges conta registros do porcelain -z. O segundo caminho de um
// rename não começa com "XY " e portanto não vira uma alteração extra.
func CountChanges(raw []byte) int {
	count := 0
	for _, record := range strings.Split(string(raw), "\x00") {
		if len(record) >= 3 && record[2] == ' ' {
			count++
		}
	}
	return count
}

func run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	return cmd.CombinedOutput()
}

func commandError(out []byte, err error) string {
	text := strings.TrimSpace(string(out))
	if text == "" {
		return err.Error()
	}
	line, _, _ := strings.Cut(text, "\n")
	return strings.TrimSpace(line)
}
