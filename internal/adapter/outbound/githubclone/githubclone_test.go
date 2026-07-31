package githubclone_test

import (
	"testing"

	"github.com/mateuslh/lealing/internal/adapter/outbound/githubclone"
	"github.com/mateuslh/lealing/internal/core/repoclone"
)

func TestParseRepositoriesFiltraOrdenaEPreservaSSH(t *testing.T) {
	raw := []byte(`[
		{"name":"pix-worker","url":"https://github.com/bradesco/pix-worker","sshUrl":"git@github.com:bradesco/pix-worker.git","description":"worker de eventos","visibility":"PRIVATE","primaryLanguage":{"name":"Go"},"defaultBranchRef":{"name":"main"},"updatedAt":"2026-07-31T12:00:00Z","diskUsage":2048},
		{"name":"pixel","url":"https://github.com/bradesco/pixel","sshUrl":"git@github.com:bradesco/pixel.git"},
		{"name":"pix-config","url":"https://github.com/bradesco/pix-config","sshUrl":"git@github.com:bradesco/pix-config.git"},
		{"name":"pix","url":"https://github.com/bradesco/pix","sshUrl":"git@github.com:bradesco/pix.git"}
	]`)

	got, err := githubclone.ParseRepositories(raw, repoclone.Source{
		Prefix: "pix", Protocol: repoclone.ProtocolSSH,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 {
		t.Fatalf("len = %d, quero 4", len(got))
	}
	if got[0].Name != "pix" || got[1].Name != "pix-config" ||
		got[2].Name != "pix-worker" || got[3].Name != "pixel" {
		t.Fatalf("ordem = %#v", got)
	}
	if got[1].CloneURL != "git@github.com:bradesco/pix-config.git" {
		t.Fatalf("clone URL = %q", got[1].CloneURL)
	}
	if got[2].Description != "worker de eventos" || got[2].Language != "Go" ||
		got[2].DefaultBranch != "main" || got[2].DiskUsageKB != 2048 {
		t.Fatalf("metadados = %#v", got[2])
	}
}
