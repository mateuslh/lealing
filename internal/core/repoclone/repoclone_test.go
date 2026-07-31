package repoclone_test

import (
	"errors"
	"testing"

	"github.com/mateuslh/lealing/internal/core/repoclone"
)

func TestParseSource(t *testing.T) {
	tests := []struct {
		raw      string
		owner    string
		repo     string
		prefix   string
		protocol repoclone.Protocol
	}{
		{"https://github.com/bradesco/pagamentos", "bradesco", "pagamentos", "pagamentos", repoclone.ProtocolHTTPS},
		{"http://github.com/bradesco/pagamentos.git", "bradesco", "pagamentos", "pagamentos", repoclone.ProtocolHTTPS},
		{"github.com/bradesco/pagamentos-config", "bradesco", "pagamentos-config", "pagamentos", repoclone.ProtocolHTTPS},
		{"git@github.com:bradesco/pagamentos.git", "bradesco", "pagamentos", "pagamentos", repoclone.ProtocolSSH},
		{"ssh://git@github.com/bradesco/pagamentos.git", "bradesco", "pagamentos", "pagamentos", repoclone.ProtocolSSH},
		{"https://github.com/bradesco/pagamentos/tree/main", "bradesco", "pagamentos", "pagamentos", repoclone.ProtocolHTTPS},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			got, err := repoclone.ParseSource(tt.raw)
			if err != nil {
				t.Fatal(err)
			}
			if got.Owner != tt.owner || got.Repository != tt.repo ||
				got.Prefix != tt.prefix || got.Protocol != tt.protocol {
				t.Fatalf("ParseSource() = %#v", got)
			}
		})
	}
}

func TestParseSourceRecusaHostETraversal(t *testing.T) {
	for _, raw := range []string{
		"", "https://gitlab.com/bradesco/pagamentos",
		"https://github.com/bradesco", "git@github.com:../pagamentos.git",
	} {
		if _, err := repoclone.ParseSource(raw); err == nil {
			t.Errorf("ParseSource(%q) aceitou entrada inválida", raw)
		}
	}

	if _, err := repoclone.ParseSource("https://gitlab.com/a/b"); !errors.Is(err, repoclone.ErrNotGitHub) {
		t.Fatalf("erro = %v, quero ErrNotGitHub", err)
	}
}

func TestMatchesPrefixIncluiTudoQueComecaPeloProjeto(t *testing.T) {
	for _, name := range []string{"pix", "pix-config", "pix-worker", "pixel", "PIX-legado"} {
		if !repoclone.MatchesPrefix(name, "pix") {
			t.Errorf("%q deveria casar", name)
		}
	}
	for _, name := range []string{"novo-pix", "pi"} {
		if repoclone.MatchesPrefix(name, "pix") {
			t.Errorf("%q não deveria casar", name)
		}
	}
}

func TestParseAdditionalSourceAceitaNomeOuURLDoMesmoOwner(t *testing.T) {
	base := repoclone.Source{Owner: "bradesco", Protocol: repoclone.ProtocolSSH}
	for _, raw := range []string{
		"pagamentos-extra",
		"https://github.com/bradesco/pagamentos-extra",
	} {
		got, err := repoclone.ParseAdditionalSource(raw, base)
		if err != nil {
			t.Fatalf("%q: %v", raw, err)
		}
		if got.Owner != "bradesco" || got.Repository != "pagamentos-extra" ||
			got.Protocol != repoclone.ProtocolSSH {
			t.Fatalf("%q: %#v", raw, got)
		}
	}
}

func TestParseAdditionalSourceRecusaOutroOwner(t *testing.T) {
	base := repoclone.Source{Owner: "bradesco"}
	_, err := repoclone.ParseAdditionalSource(
		"https://github.com/outra-org/pagamentos-extra", base)
	if !errors.Is(err, repoclone.ErrDifferentOwner) {
		t.Fatalf("erro = %v, quero ErrDifferentOwner", err)
	}
}
