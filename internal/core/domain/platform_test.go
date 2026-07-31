package domain_test

import (
	"testing"

	"github.com/mateuslh/lealing/internal/core/domain"
)

func TestToolSemPlataformaDeclaradaEPortavel(t *testing.T) {
	tool := domain.Tool{ID: "portavel", Name: "Portável", Category: "system"}

	if got := tool.SupportedPlatforms(); got != domain.AllPlatforms {
		t.Errorf("SupportedPlatforms = %v, quero todas", got)
	}
	for _, p := range []domain.Platform{domain.Darwin, domain.Windows, domain.Linux} {
		if !tool.RunsOn(p) {
			t.Errorf("tool sem Platforms recusada em %v", p)
		}
	}
	if err := tool.Validate(); err != nil {
		t.Errorf("Validate rejeitou o zero-value: %v", err)
	}
}

func TestToolRodaSoOndeDeclara(t *testing.T) {
	tool := domain.Tool{
		ID: "power-control", Name: "Energia", Category: "system",
		Platforms: domain.Darwin | domain.Windows,
	}

	if !tool.RunsOn(domain.Darwin) || !tool.RunsOn(domain.Windows) {
		t.Error("tool recusada em plataforma declarada")
	}
	if tool.RunsOn(domain.Linux) {
		t.Error("tool aceita em plataforma não declarada")
	}
	// GOOS desconhecido vira o conjunto vazio, e nada roda nele: o lado
	// seguro é a tool sumir, não abrir e falhar no primeiro comando.
	if tool.RunsOn(domain.ParsePlatform("plan9")) {
		t.Error("tool aceita em plataforma desconhecida")
	}
}

func TestValidateRecusaBitDesconhecido(t *testing.T) {
	tool := domain.Tool{
		ID: "torta", Name: "Torta", Category: "system",
		Platforms: domain.Platform(1 << 6),
	}
	if err := tool.Validate(); err == nil {
		t.Error("Validate aceitou um bit fora dos declarados")
	}
}

func TestPlatformExibicao(t *testing.T) {
	tests := map[domain.Platform]string{
		domain.Darwin:                     "macOS",
		domain.Darwin | domain.Windows:    "macOS · Windows",
		domain.AllPlatforms:               "macOS · Windows · Linux",
		domain.ParsePlatform("windows"):   "Windows",
		domain.ParsePlatform("qualquer?"): "nenhuma",
	}
	for p, want := range tests {
		if got := p.String(); got != want {
			t.Errorf("%b → %q, quero %q", p, got, want)
		}
	}
}

func TestCurrentPlatformCasaComOGOOS(t *testing.T) {
	// O binário de teste roda na mesma plataforma que o programa: se a
	// detecção falhar aqui, o catálogo inteiro some em produção.
	if got := domain.CurrentPlatform(); got == 0 {
		t.Error("CurrentPlatform não reconheceu o sistema em que os testes rodam")
	}
}
