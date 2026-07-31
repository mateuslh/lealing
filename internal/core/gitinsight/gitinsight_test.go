package gitinsight_test

import (
	"testing"

	"github.com/mateuslh/lealing/internal/core/gitinsight"
)

func TestClassificacaoDeBranches(t *testing.T) {
	tests := []struct {
		name      string
		branch    gitinsight.Branch
		push      bool
		cleanup   bool
		untracked bool
		synced    bool
	}{
		{
			name:   "ahead precisa push",
			branch: gitinsight.Branch{Name: "feature", Upstream: "origin/feature", Remote: "origin", Ahead: 2},
			push:   true,
		},
		{
			name:    "sincronizada fora da atual pode limpar",
			branch:  gitinsight.Branch{Name: "feature", Upstream: "origin/feature", Remote: "origin"},
			cleanup: true,
			synced:  true,
		},
		{
			name:   "atual nunca pode limpar",
			branch: gitinsight.Branch{Name: "main", Upstream: "origin/main", Remote: "origin", Current: true},
			synced: true,
		},
		{
			name:      "sem upstream é incerta",
			branch:    gitinsight.Branch{Name: "rascunho"},
			untracked: true,
		},
		{
			name:      "upstream removido é incerto",
			branch:    gitinsight.Branch{Name: "antiga", Upstream: "origin/antiga", Remote: "origin", Gone: true},
			untracked: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.branch.NeedsPush(); got != tt.push {
				t.Errorf("NeedsPush = %v", got)
			}
			if got := tt.branch.CleanupCandidate(); got != tt.cleanup {
				t.Errorf("CleanupCandidate = %v", got)
			}
			if got := tt.branch.WithoutUpstream(); got != tt.untracked {
				t.Errorf("WithoutUpstream = %v", got)
			}
			if got := tt.branch.Synced(); got != tt.synced {
				t.Errorf("Synced = %v", got)
			}
		})
	}
}

func TestStatsAgregaRepositorios(t *testing.T) {
	report := gitinsight.Report{Repositories: []gitinsight.Repository{
		{
			DirtyFiles: 2,
			Branches: []gitinsight.Branch{
				{Name: "main", Upstream: "origin/main", Remote: "origin", Current: true, Ahead: 1},
				{Name: "pronta", Upstream: "origin/pronta", Remote: "origin"},
				{Name: "local"},
			},
		},
		{Err: "git falhou"},
	}}
	got := report.Stats()
	if got.Repositories != 2 || got.Branches != 3 || got.NeedPush != 1 ||
		got.UnpushedCommits != 1 || got.Cleanup != 1 || got.NoUpstream != 1 || got.DirtyRepos != 1 ||
		got.Errors != 1 {
		t.Fatalf("Stats = %#v", got)
	}
}

func TestUpdateReportResumeResultados(t *testing.T) {
	report := gitinsight.UpdateReport{Results: []gitinsight.UpdateResult{
		{State: gitinsight.UpdateUpdated},
		{State: gitinsight.UpdateCurrent},
		{State: gitinsight.UpdateSkipped},
		{State: gitinsight.UpdateFailed},
		{State: gitinsight.UpdateUpdated},
	}}
	got := report.Stats()
	if got.Updated != 2 || got.Current != 1 || got.Skipped != 1 || got.Failed != 1 {
		t.Fatalf("Stats = %#v", got)
	}
}
