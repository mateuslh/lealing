package marketplace_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mateuslh/lealing/internal/core/marketplace"
	"github.com/mateuslh/lealing/internal/core/toolinstall"
)

func TestSelecionaMaisNovaCompativelComEngineProtocoloEPlataforma(t *testing.T) {
	artifact := func(platform string) []marketplace.Artifact {
		return []marketplace.Artifact{{Platform: platform, URL: "https://example.test/tool", SHA256: strings.Repeat("a", 64)}}
	}
	index := marketplace.Index{Tools: []marketplace.Entry{
		{ID: "demo", Version: "1.0.0", Protocol: marketplace.VersionRange{Min: 1, Max: 1}, MinimumEngine: "1.0.0", Artifacts: artifact("darwin-arm64")},
		{ID: "demo", Version: "1.2.0", Protocol: marketplace.VersionRange{Min: 1, Max: 1}, MinimumEngine: "1.1.0", Artifacts: artifact("darwin-arm64")},
		{ID: "demo", Version: "2.0.0", Protocol: marketplace.VersionRange{Min: 2, Max: 2}, MinimumEngine: "2.0.0", Artifacts: artifact("darwin-arm64")},
	}}
	entry, _, err := index.SelectLatest("demo", "darwin-arm64", "1.5.0", marketplace.VersionRange{Min: 1, Max: 1})
	if err != nil {
		t.Fatal(err)
	}
	if entry.Version != "1.2.0" {
		t.Fatalf("versão = %s", entry.Version)
	}
}

func validEntry() marketplace.Entry {
	return marketplace.Entry{
		ID: "demo", Version: "1.2.0", Name: "Demo", Summary: "Demonstra uma tool.",
		Category: "utilities", Risk: "safe", Publisher: "example", DistributionTier: marketplace.ChannelCommunity,
		Permissions: marketplace.Permissions{Filesystem: marketplace.FilesystemPermissions{Read: []string{"~/.demo"}}},
		ManifestURL: "https://example.test/manifest.yaml", MinimumEngine: "1.0.0",
		Protocol: marketplace.VersionRange{Min: 1, Max: 1},
		Artifacts: []marketplace.Artifact{{
			Platform: "darwin-arm64", URL: "https://example.test/demo.tar.gz", SHA256: strings.Repeat("a", 64),
		}},
	}
}

func validationOptions() marketplace.ValidationOptions {
	return marketplace.ValidationOptions{
		Categories: map[string]bool{"utilities": true},
		Platforms:  map[string]bool{"darwin-arm64": true},
	}
}

func TestValidaIndicePublico(t *testing.T) {
	index := marketplace.Index{APIVersion: marketplace.APIVersion, Tools: []marketplace.Entry{validEntry()}}
	if err := index.Validate(validationOptions()); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	duplicate := validEntry()
	index.Tools = append(index.Tools, duplicate)
	if err := index.Validate(validationOptions()); err == nil || !strings.Contains(err.Error(), "duplicada") {
		t.Fatalf("duplicata = %v", err)
	}
}

func TestIndiceRejeitaURLSemHTTPSEChecksumInvalido(t *testing.T) {
	entry := validEntry()
	entry.Artifacts[0].URL = "http://example.test/demo.tar.gz"
	if err := entry.Validate(validationOptions()); err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("URL insegura = %v", err)
	}

	entry = validEntry()
	entry.Artifacts[0].SHA256 = "abc"
	if err := entry.Validate(validationOptions()); err == nil || !strings.Contains(err.Error(), "SHA-256") {
		t.Fatalf("checksum inválido = %v", err)
	}
}

func TestBuildDevAceitaMinimumEngineDeRelease(t *testing.T) {
	entry := validEntry()
	entry.MinimumEngine = "99.0.0"
	index := marketplace.Index{Tools: []marketplace.Entry{entry}}
	if _, _, err := index.SelectLatest("demo", "darwin-arm64", "dev", marketplace.VersionRange{Min: 1, Max: 1}); err != nil {
		t.Fatalf("SelectLatest dev: %v", err)
	}
}

func TestSelecionaReleaseEstavelAcimaDePreRelease(t *testing.T) {
	entry := validEntry()
	entry.Version = "1.2.0-rc.2"
	stable := validEntry()
	index := marketplace.Index{Tools: []marketplace.Entry{entry, stable}}
	selected, _, err := index.SelectLatest("demo", "darwin-arm64", "1.5.0", marketplace.VersionRange{Min: 1, Max: 1})
	if err != nil || selected.Version != "1.2.0" {
		t.Fatalf("selecionada = %s, %v", selected.Version, err)
	}
}

func TestPreReleaseNaoAtendeMinimumEngineEstavel(t *testing.T) {
	index := marketplace.Index{Tools: []marketplace.Entry{validEntry()}}
	if _, _, err := index.SelectLatest("demo", "darwin-arm64", "1.0.0-rc.1", marketplace.VersionRange{Min: 1, Max: 1}); !errors.Is(err, marketplace.ErrNotAvailable) {
		t.Fatalf("SelectLatest = %v", err)
	}
}

type fakeIndex struct{ index marketplace.Index }

func (f fakeIndex) Fetch(context.Context) (marketplace.Index, error) { return f.index, nil }

type fakePackages struct {
	prepared marketplace.PreparedPackage
	artifact marketplace.Artifact
}

func (f *fakePackages) Prepare(_ context.Context, artifact marketplace.Artifact) (marketplace.PreparedPackage, error) {
	f.artifact = artifact
	return f.prepared, nil
}

type fakeInstaller struct {
	installed []toolinstall.Installed
	request   toolinstall.InstallRequest
}

func (f *fakeInstaller) InstallLocal(_ context.Context, request toolinstall.InstallRequest) (toolinstall.Installation, error) {
	f.request = request
	return toolinstall.Installation{ID: request.ExpectedID, Version: request.ExpectedVersion}, nil
}
func (f *fakeInstaller) ListInstalled(context.Context) ([]toolinstall.Installed, error) {
	return f.installed, nil
}
func (*fakeInstaller) Rollback(context.Context, string) (toolinstall.Installation, error) {
	return toolinstall.Installation{}, errors.New("não usado")
}
func (*fakeInstaller) Remove(context.Context, string) (toolinstall.Removal, error) {
	return toolinstall.Removal{}, errors.New("não usado")
}

type fakeReloader struct{ calls int }

func (f *fakeReloader) Reload(context.Context) error { f.calls++; return nil }

func testService(installer *fakeInstaller, packages *fakePackages, reloader *fakeReloader) *marketplace.Service {
	return marketplace.NewService(marketplace.Config{
		Platform: "darwin-arm64", EngineVersion: "1.5.0", Protocol: marketplace.VersionRange{Min: 1, Max: 1},
		Validation: validationOptions(), Index: fakeIndex{index: marketplace.Index{
			APIVersion: marketplace.APIVersion, Tools: []marketplace.Entry{validEntry()},
		}},
		Packages: packages, Installer: installer, CatalogReloader: reloader,
	})
}

func TestServiceListaEstadoInstaladoEAtualizacao(t *testing.T) {
	installer := &fakeInstaller{installed: []toolinstall.Installed{{ID: "demo", ActiveVersion: "1.0.0"}}}
	service := testService(installer, &fakePackages{}, &fakeReloader{})
	listings, err := service.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(listings) != 1 || listings[0].InstalledVersion != "1.0.0" || !listings[0].UpdateAvailable {
		t.Fatalf("listings = %+v", listings)
	}
}

func TestServiceInstalaPacoteSelecionadoERecarregaCatalogo(t *testing.T) {
	cleanups := 0
	packages := &fakePackages{prepared: marketplace.PreparedPackage{
		Directory: "/tmp/demo", Cleanup: func() error { cleanups++; return nil },
	}}
	installer := &fakeInstaller{}
	reloader := &fakeReloader{}
	service := testService(installer, packages, reloader)

	installation, err := service.Install(context.Background(), "demo")
	if err != nil {
		t.Fatal(err)
	}
	if installation.ID != "demo" || installer.request.ExpectedID != "demo" || installer.request.ExpectedVersion != "1.2.0" || installer.request.ExpectedManifest == nil || installer.request.ExpectedManifest.Risk != "safe" {
		t.Fatalf("instalação=%+v request=%+v", installation, installer.request)
	}
	if cleanups != 1 || reloader.calls != 1 {
		t.Fatalf("cleanup=%d reload=%d", cleanups, reloader.calls)
	}
}

func TestServiceNaoBaixaVersaoJaAtiva(t *testing.T) {
	packages := &fakePackages{}
	installer := &fakeInstaller{installed: []toolinstall.Installed{{ID: "demo", ActiveVersion: "1.2.0"}}}
	service := testService(installer, packages, &fakeReloader{})
	_, err := service.Install(context.Background(), "demo")
	if !errors.Is(err, marketplace.ErrAlreadyLatest) {
		t.Fatalf("Install = %v", err)
	}
	if packages.artifact.URL != "" {
		t.Fatal("pacote foi preparado para uma versão já ativa")
	}
}
