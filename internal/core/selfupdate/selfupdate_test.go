package selfupdate_test

import (
	"context"
	"errors"
	"testing"

	"github.com/mateuslh/lealing/internal/core/selfupdate"
)

// --- Duplos ------------------------------------------------------------

type fakeLocator struct {
	install selfupdate.Install
	err     error
}

func (f fakeLocator) Locate(context.Context) (selfupdate.Install, error) {
	return f.install, f.err
}

type fakeReleases struct {
	release selfupdate.Release
	err     error
}

func (f fakeReleases) Latest(context.Context) (selfupdate.Release, error) {
	return f.release, f.err
}

type fakeApplier struct {
	called  bool
	outcome selfupdate.Outcome
	err     error
}

func (f *fakeApplier) Apply(context.Context, selfupdate.Install, selfupdate.Release) (selfupdate.Outcome, error) {
	f.called = true
	return f.outcome, f.err
}

// --- Versões -----------------------------------------------------------

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in     string
		known  bool
		major  int
		minor  int
		patch  int
		suffix string
	}{
		{in: "v1.4.0", known: true, major: 1, minor: 4},
		{in: "1.4.0", known: true, major: 1, minor: 4},
		{in: "v2.0", known: true, major: 2},
		{in: "v1.4.0-3-gaf21c9", known: true, major: 1, minor: 4, suffix: "3-gaf21c9"},
		{in: "v1.4.0-3-gaf21c9-dirty", known: true, major: 1, minor: 4, suffix: "3-gaf21c9-dirty"},
		{in: "dev"},
		{in: ""},
		{in: "af21c9d"},
		{in: "v1.2.3.4"},
	}

	for _, tc := range cases {
		got := selfupdate.ParseVersion(tc.in)
		if got.Known != tc.known {
			t.Errorf("%q: Known=%v, queria %v", tc.in, got.Known, tc.known)
			continue
		}
		if !tc.known {
			continue
		}
		if got.Major != tc.major || got.Minor != tc.minor || got.Patch != tc.patch {
			t.Errorf("%q: %d.%d.%d, queria %d.%d.%d",
				tc.in, got.Major, got.Minor, got.Patch, tc.major, tc.minor, tc.patch)
		}
		if got.Suffix != tc.suffix {
			t.Errorf("%q: sufixo %q, queria %q", tc.in, got.Suffix, tc.suffix)
		}
	}
}

func TestCompareVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v1.4.0", "v1.4.0", 0},
		{"v1.4.0", "v1.5.0", -1},
		{"v2.0.0", "v1.9.9", 1},
		{"v1.4.1", "v1.4.0", 1},
		// O sufixo do git describe significa "commits depois da tag", então
		// o build local está à frente — e não atrás, como diria o semver.
		{"v1.4.0-3-gaf21c9", "v1.4.0", 1},
		{"v1.4.0", "v1.4.0-3-gaf21c9", -1},
	}

	for _, tc := range cases {
		got := selfupdate.ParseVersion(tc.a).Compare(selfupdate.ParseVersion(tc.b))
		if got != tc.want {
			t.Errorf("%s vs %s: %d, queria %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// --- Serviço -----------------------------------------------------------

func TestCheckClassificaOEstado(t *testing.T) {
	cases := []struct {
		name    string
		current string
		latest  string
		want    selfupdate.State
	}{
		{"em dia", "v1.4.0", "v1.4.0", selfupdate.StateUpToDate},
		{"desatualizado", "v1.3.0", "v1.4.0", selfupdate.StateOutdated},
		{"à frente", "v1.4.0-2-gabc", "v1.4.0", selfupdate.StateAhead},
		{"sem versão", "dev", "v1.4.0", selfupdate.StateUnknown},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := selfupdate.NewService(tc.current,
				fakeLocator{install: selfupdate.Install{Mode: selfupdate.ModeRelease}},
				fakeReleases{release: selfupdate.Release{Tag: tc.latest}},
				&fakeApplier{},
			)

			st, err := svc.Check(context.Background())
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if st.State != tc.want {
				t.Errorf("estado %v, queria %v", st.State, tc.want)
			}
		})
	}
}

func TestCheckSobreviveASemLocalizarAInstalacao(t *testing.T) {
	// Saber que há versão nova é útil mesmo sem saber como aplicá-la: um
	// erro de localização não pode derrubar a verificação inteira.
	svc := selfupdate.NewService("v1.3.0",
		fakeLocator{err: errors.New("sem executável")},
		fakeReleases{release: selfupdate.Release{Tag: "v1.4.0"}},
		&fakeApplier{},
	)

	st, err := svc.Check(context.Background())
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if st.State != selfupdate.StateOutdated {
		t.Errorf("estado %v, queria StateOutdated", st.State)
	}
	if st.Install.Mode != selfupdate.ModeUnknown {
		t.Errorf("modo %v, queria ModeUnknown", st.Install.Mode)
	}
	if st.CanApply() {
		t.Error("instalação desconhecida não pode se dizer aplicável")
	}
}

func TestCheckPropagaFalhaDaConsulta(t *testing.T) {
	svc := selfupdate.NewService("v1.3.0",
		fakeLocator{},
		fakeReleases{err: errors.New("sem rede")},
		&fakeApplier{},
	)

	if _, err := svc.Check(context.Background()); err == nil {
		t.Fatal("erro de rede foi engolido: a tela mostraria 'em dia' sem ter consultado nada")
	}
}

func TestCanApply(t *testing.T) {
	cases := []struct {
		name  string
		mode  selfupdate.Mode
		state selfupdate.State
		tag   string
		want  bool
	}{
		{"release atrasado", selfupdate.ModeRelease, selfupdate.StateOutdated, "v1.4.0", true},
		{"release em dia", selfupdate.ModeRelease, selfupdate.StateUpToDate, "v1.4.0", false},
		{"release à frente", selfupdate.ModeRelease, selfupdate.StateAhead, "v1.4.0", false},
		{"release sem versão local", selfupdate.ModeRelease, selfupdate.StateUnknown, "v1.4.0", true},
		{"release sem tag publicada", selfupdate.ModeRelease, selfupdate.StateUnknown, "", false},
		// O clone puxa commits da branch, que não dependem de haver release.
		{"fonte em dia", selfupdate.ModeSource, selfupdate.StateUpToDate, "v1.4.0", true},
		{"fonte à frente", selfupdate.ModeSource, selfupdate.StateAhead, "v1.4.0", true},
		{"origem desconhecida", selfupdate.ModeUnknown, selfupdate.StateOutdated, "v1.4.0", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st := selfupdate.Status{
				Install: selfupdate.Install{Mode: tc.mode},
				State:   tc.state,
				Latest:  selfupdate.Release{Tag: tc.tag},
			}
			if got := st.CanApply(); got != tc.want {
				t.Errorf("CanApply()=%v, queria %v", got, tc.want)
			}
		})
	}
}

func TestApplyRecusaOQueNaoSeAplica(t *testing.T) {
	applier := &fakeApplier{}
	svc := selfupdate.NewService("v1.4.0", fakeLocator{}, fakeReleases{}, applier)

	st := selfupdate.Status{
		Install: selfupdate.Install{Mode: selfupdate.ModeRelease},
		State:   selfupdate.StateUpToDate,
		Latest:  selfupdate.Release{Tag: "v1.4.0"},
	}
	if _, err := svc.Apply(context.Background(), st); !errors.Is(err, selfupdate.ErrNotApplicable) {
		t.Fatalf("erro %v, queria ErrNotApplicable", err)
	}
	if applier.called {
		t.Error("o applier foi chamado para uma instalação já em dia")
	}
}

func TestApplyPreencheAVersaoDeOrigem(t *testing.T) {
	applier := &fakeApplier{outcome: selfupdate.Outcome{To: "v1.4.0", Restart: true}}
	svc := selfupdate.NewService("v1.3.0", fakeLocator{}, fakeReleases{}, applier)

	st := selfupdate.Status{
		Install: selfupdate.Install{Mode: selfupdate.ModeRelease},
		Current: selfupdate.ParseVersion("v1.3.0"),
		State:   selfupdate.StateOutdated,
		Latest:  selfupdate.Release{Tag: "v1.4.0"},
	}
	out, err := svc.Apply(context.Background(), st)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if out.From != "v1.3.0" {
		t.Errorf("From=%q, queria v1.3.0", out.From)
	}
}
