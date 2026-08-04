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

func TestSelecionaVersaoExataParaReproduzirSnapshot(t *testing.T) {
	older, newer := validEntry(), validEntry()
	older.Version, newer.Version = "1.0.0", "2.0.0"
	index := marketplace.Index{Tools: []marketplace.Entry{newer, older}}

	entry, _, err := index.SelectVersion(
		"demo", "1.0.0", "darwin-arm64", "2.0.0", marketplace.VersionRange{Min: 1, Max: 1})
	if err != nil {
		t.Fatal(err)
	}
	if entry.Version != "1.0.0" {
		t.Fatalf("versão selecionada = %s", entry.Version)
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

func TestIndiceAceitaWorkingDirConhecidoERejeitaNivelInventado(t *testing.T) {
	entry := validEntry()
	entry.Permissions.WorkingDir = "read"
	if err := entry.Validate(validationOptions()); err != nil {
		t.Fatalf("workingDir read = %v", err)
	}

	entry.Permissions.WorkingDir = "total"
	if err := entry.Validate(validationOptions()); err == nil || !strings.Contains(err.Error(), "workingDir") {
		t.Fatalf("nível inventado = %v", err)
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

// fakeIndex devolve um índice por origem, para exercitar a agregação sem
// nenhuma rede.
type fakeIndex struct {
	byOrigin map[string]marketplace.Index
	failures map[string]error
}

func (f fakeIndex) Fetch(_ context.Context, origin marketplace.Origin) (marketplace.Index, error) {
	if err, failed := f.failures[origin.Name]; failed {
		return marketplace.Index{}, err
	}
	return f.byOrigin[origin.Name], nil
}

type fakePackages struct {
	prepared marketplace.PreparedPackage
	artifact marketplace.Artifact
	origin   marketplace.Origin
}

func (f *fakePackages) Prepare(_ context.Context, origin marketplace.Origin, artifact marketplace.Artifact) (marketplace.PreparedPackage, error) {
	f.origin, f.artifact = origin, artifact
	return f.prepared, nil
}

// memorySources é o SourceStore em memória usado pelos testes de origem.
type memorySources struct {
	state   marketplace.SourceState
	saveErr error
}

func (m *memorySources) Load(context.Context) (marketplace.SourceState, error) { return m.state, nil }
func (m *memorySources) Save(_ context.Context, state marketplace.SourceState) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.state = state
	return nil
}

type fakeInstaller struct {
	installed   []toolinstall.Installed
	request     toolinstall.InstallRequest
	previous    []toolinstall.Installed
	reconciled  int
	restored    bool
	onReconcile func()
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
func (f *fakeInstaller) Reconcile(_ context.Context, requests []toolinstall.InstallRequest) (toolinstall.Reconciliation, error) {
	if f.onReconcile != nil {
		f.onReconcile()
	}
	f.previous = append([]toolinstall.Installed(nil), f.installed...)
	next := make([]toolinstall.Installed, 0, len(requests))
	for _, request := range requests {
		next = append(next, toolinstall.Installed{
			Host: request.Host, ID: request.ExpectedID, ActiveVersion: request.ExpectedVersion,
		})
	}
	f.reconciled++
	left := make(map[string]string, len(f.installed))
	for _, item := range f.installed {
		left[item.ID] = item.Host + "\x00" + item.ActiveVersion
	}
	right := make(map[string]string, len(next))
	for _, item := range next {
		right[item.ID] = item.Host + "\x00" + item.ActiveVersion
	}
	changed := 0
	for id, value := range left {
		if right[id] != value {
			changed++
		}
	}
	for id, value := range right {
		if left[id] == "" && value != "" {
			changed++
		}
	}
	f.installed = next
	return toolinstall.Reconciliation{Changed: changed, RecoveryDir: "/tmp/anterior", PreviousPresent: true}, nil
}
func (f *fakeInstaller) Restore(context.Context, toolinstall.Reconciliation) error {
	f.installed = append([]toolinstall.Installed(nil), f.previous...)
	f.restored = true
	return nil
}

type fakeReloader struct{ calls int }

func (f *fakeReloader) Reload(context.Context) error { f.calls++; return nil }

func officialOrigin() marketplace.Origin {
	return marketplace.Origin{
		Name: "lealing", Label: "índice padrão", Kind: marketplace.OriginRemote,
		Ref: "https://example.test/index.json", Trusted: true, Builtin: true, Enabled: true,
	}
}

func indexWith(entries ...marketplace.Entry) marketplace.Index {
	return marketplace.Index{APIVersion: marketplace.APIVersion, Tools: entries}
}

func testService(installer *fakeInstaller, packages *fakePackages, reloader *fakeReloader) *marketplace.Service {
	return newService(marketplace.Config{
		Index:    fakeIndex{byOrigin: map[string]marketplace.Index{"lealing": indexWith(validEntry())}},
		Packages: packages, Installer: installer, CatalogReloader: reloader,
	})
}

// newService preenche o que todo teste repetiria e deixa o caso destacar
// apenas o que ele está exercitando.
func newService(config marketplace.Config) *marketplace.Service {
	config.Platform = "darwin-arm64"
	config.EngineVersion = "1.5.0"
	config.Protocol = marketplace.VersionRange{Min: 1, Max: 1}
	config.Validation = validationOptions()
	if config.BuiltinSources == nil {
		config.BuiltinSources = []marketplace.Origin{officialOrigin()}
	}
	if config.Sources == nil {
		config.Sources = &memorySources{}
	}
	if config.Installer == nil {
		config.Installer = &fakeInstaller{}
	}
	if config.Packages == nil {
		config.Packages = &fakePackages{}
	}
	return marketplace.NewService(config)
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
	if installation.ID != "demo" || installer.request.Host != "lealing" || installer.request.ExpectedID != "demo" || installer.request.ExpectedVersion != "1.2.0" || installer.request.ExpectedManifest == nil || installer.request.ExpectedManifest.Risk != "safe" {
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

func communityOrigin(name string) marketplace.Origin {
	return marketplace.Origin{
		Name: name, Kind: marketplace.OriginRemote,
		Ref: "https://" + name + ".test/index.json", Enabled: true,
	}
}

func entryFrom(id, version, channel string) marketplace.Entry {
	entry := validEntry()
	entry.ID, entry.Version, entry.Name = id, version, id
	entry.DistributionTier = marketplace.Channel(channel)
	return entry
}

func TestCatalogAgregaOrigensEDegradaPorOrigem(t *testing.T) {
	service := newService(marketplace.Config{
		BuiltinSources: []marketplace.Origin{officialOrigin()},
		Sources: &memorySources{state: marketplace.SourceState{
			Custom: []marketplace.Origin{communityOrigin("parceiro"), communityOrigin("offline")},
		}},
		Index: fakeIndex{
			byOrigin: map[string]marketplace.Index{
				"lealing":  indexWith(entryFrom("demo", "1.2.0", "official")),
				"parceiro": indexWith(entryFrom("extra", "0.1.0", "community")),
			},
			failures: map[string]error{"offline": errors.New("sem rede")},
		},
	})

	catalog, err := service.Catalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Tools) != 2 {
		t.Fatalf("tools = %d, quero as duas origens que responderam", len(catalog.Tools))
	}
	if !catalog.Degraded() {
		t.Fatal("a origem fora do ar não foi sinalizada")
	}
	var failed int
	for _, status := range catalog.Sources {
		if status.Err != nil {
			failed++
			if status.Name != "offline" {
				t.Fatalf("origem com erro = %s", status.Name)
			}
		}
	}
	if failed != 1 {
		t.Fatalf("origens com erro = %d", failed)
	}
}

func TestOrigemNaoConfiavelPerdeOSeloDeCanal(t *testing.T) {
	service := newService(marketplace.Config{
		// Sem origem embutida: a única consultada é a do usuário.
		BuiltinSources: []marketplace.Origin{},
		Sources: &memorySources{state: marketplace.SourceState{
			Custom: []marketplace.Origin{communityOrigin("terceiro")},
		}},
		Index: fakeIndex{byOrigin: map[string]marketplace.Index{
			"terceiro": indexWith(entryFrom("demo", "1.2.0", "official")),
		}},
	})

	listings, err := service.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(listings) != 1 || listings[0].DistributionTier != marketplace.ChannelCommunity {
		t.Fatalf("canal = %+v; um índice de terceiro não pode se declarar oficial", listings)
	}
}

func TestOrigemDeMaiorPrioridadeVenceConflitoDeID(t *testing.T) {
	packages := &fakePackages{prepared: marketplace.PreparedPackage{Directory: "/tmp/demo"}}
	service := newService(marketplace.Config{
		Sources: &memorySources{state: marketplace.SourceState{
			Custom: []marketplace.Origin{communityOrigin("impostor")},
		}},
		Index: fakeIndex{byOrigin: map[string]marketplace.Index{
			"lealing":  indexWith(entryFrom("demo", "1.2.0", "official")),
			"impostor": indexWith(entryFrom("demo", "9.9.9", "community")),
		}},
		Packages: packages,
	})

	listings, err := service.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(listings) != 1 {
		t.Fatalf("listings = %d", len(listings))
	}
	if listings[0].Version != "1.2.0" || listings[0].Origin.Name != "lealing" {
		t.Fatalf("vencedor = %s@%s", listings[0].Origin.Name, listings[0].Version)
	}
	if len(listings[0].Shadowed) != 1 || listings[0].Shadowed[0] != "impostor" {
		t.Fatalf("shadowed = %v", listings[0].Shadowed)
	}

	// A referência qualificada continua permitindo instalar a outra de
	// propósito: a prioridade protege o padrão, não proíbe a escolha.
	if _, err := service.Install(context.Background(), "impostor/demo"); err != nil {
		t.Fatal(err)
	}
	if packages.origin.Name != "impostor" {
		t.Fatalf("origem preparada = %s", packages.origin.Name)
	}
}

func TestSourcesAdicionaRemoveEDesliga(t *testing.T) {
	store := &memorySources{}
	service := newService(marketplace.Config{Sources: store})
	ctx := context.Background()

	origin, err := marketplace.NewOrigin("", "", "https://github.com/alguem/tools/raw/main/index.json")
	if err != nil {
		t.Fatal(err)
	}
	if origin.Name != "alguem-tools" {
		t.Fatalf("nome derivado = %q", origin.Name)
	}
	if err := service.AddSource(ctx, origin); err != nil {
		t.Fatal(err)
	}
	if err := service.AddSource(ctx, origin); !errors.Is(err, marketplace.ErrSourceExists) {
		t.Fatalf("origem duplicada = %v", err)
	}

	origins, err := service.Sources(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(origins) != 2 || origins[0].Name != "lealing" || origins[1].Priority != 1 {
		t.Fatalf("origens = %+v", origins)
	}

	if err := service.SetSourceEnabled(ctx, "lealing", false); err != nil {
		t.Fatal(err)
	}
	origins, _ = service.Sources(ctx)
	if origins[0].Enabled {
		t.Fatal("a origem embutida continuou habilitada")
	}
	if _, err := service.RemoveSource(ctx, "lealing"); !errors.Is(err, marketplace.ErrSourceBuiltin) {
		t.Fatalf("remoção da embutida = %v", err)
	}
	if _, err := service.RemoveSource(ctx, "alguem-tools"); err != nil {
		t.Fatal(err)
	}
	if origins, _ = service.Sources(ctx); len(origins) != 1 {
		t.Fatalf("origens após remoção = %+v", origins)
	}
}

func TestRemoverOrigemRemoveEmCascataSomenteToolsDela(t *testing.T) {
	store := &memorySources{state: marketplace.SourceState{
		Custom: []marketplace.Origin{communityOrigin("parceiro")},
	}}
	installer := &fakeInstaller{installed: []toolinstall.Installed{
		{Host: "parceiro", ID: "extra", ActiveVersion: "1.0.0"},
		{Host: "lealing", ID: "demo", ActiveVersion: "1.2.0"},
	}}
	service := newService(marketplace.Config{Sources: store, Installer: installer})

	removed, err := service.RemoveSource(context.Background(), "parceiro")
	if err != nil {
		t.Fatal(err)
	}
	if removed.RemovedTools != 1 || removed.RecoveryDir == "" {
		t.Fatalf("resultado da remoção = %+v", removed)
	}
	if len(store.state.Custom) != 0 {
		t.Fatalf("origem ainda persistida: %+v", store.state.Custom)
	}
	if len(installer.installed) != 1 || installer.installed[0].Host != "lealing" {
		t.Fatalf("tools após cascata = %+v", installer.installed)
	}
}

func TestFalhaAoSalvarOrigemRestauraConjuntoDeTools(t *testing.T) {
	store := &memorySources{state: marketplace.SourceState{
		Custom: []marketplace.Origin{communityOrigin("parceiro")},
	}, saveErr: errors.New("disco cheio")}
	installer := &fakeInstaller{installed: []toolinstall.Installed{{
		Host: "parceiro", ID: "extra", ActiveVersion: "1.0.0",
	}}}
	service := newService(marketplace.Config{Sources: store, Installer: installer})

	if _, err := service.RemoveSource(context.Background(), "parceiro"); err == nil {
		t.Fatal("falha do store foi escondida")
	}
	if !installer.restored || len(installer.installed) != 1 || installer.installed[0].Host != "parceiro" {
		t.Fatalf("tools não foram restauradas: %+v, restored=%v", installer.installed, installer.restored)
	}
	if len(store.state.Custom) != 1 {
		t.Fatalf("origem mudou apesar da falha: %+v", store.state.Custom)
	}
}

func TestReconcilePublicaOrigemAntesDeAtivarToolNova(t *testing.T) {
	store := &memorySources{}
	partner := communityOrigin("parceiro")
	installer := &fakeInstaller{}
	bridgeVisible := false
	installer.onReconcile = func() {
		bridgeVisible = len(store.state.Custom) == 1 && store.state.Custom[0].Name == "parceiro"
	}
	entry := entryFrom("extra", "1.2.0", "community")
	service := newService(marketplace.Config{
		BuiltinSources: []marketplace.Origin{},
		Sources:        store,
		Installer:      installer,
		Index: fakeIndex{byOrigin: map[string]marketplace.Index{
			"parceiro": indexWith(entry),
		}},
		Packages: &fakePackages{prepared: marketplace.PreparedPackage{Directory: "/tmp/extra"}},
	})

	_, err := service.ReconcileState(context.Background(), marketplace.StateReconcileRequest{
		Sources: &marketplace.SourceState{Custom: []marketplace.Origin{partner}},
		Tools: []marketplace.DesiredTool{{
			Host: "parceiro", ID: "extra", Version: "1.2.0",
		}},
		ExactTools: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !bridgeVisible {
		t.Fatal("tool foi ativada antes de sua origem ficar persistida")
	}
}

func TestOrigemAdicionadaNuncaNasceConfiavel(t *testing.T) {
	store := &memorySources{}
	service := newService(marketplace.Config{Sources: store})
	forged := marketplace.Origin{
		Name: "forjada", Kind: marketplace.OriginRemote,
		Ref: "https://forjada.test/index.json", Trusted: true, Builtin: true,
	}
	if err := service.AddSource(context.Background(), forged); err != nil {
		t.Fatal(err)
	}
	origins, err := service.Sources(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, origin := range origins {
		if origin.Name == "forjada" && (origin.Trusted || origin.Builtin) {
			t.Fatalf("origem do usuário entrou confiável: %+v", origin)
		}
	}
}

func TestNewOriginRecusaEnderecoInseguroEAceitaCaminhoLocal(t *testing.T) {
	if _, err := marketplace.NewOrigin("", "", "http://exemplo.test/index.json"); err == nil {
		t.Fatal("HTTP simples foi aceito")
	}
	if _, err := marketplace.NewOrigin("", "", "exemplo.test/index.json"); err == nil {
		t.Fatal("endereço sem esquema foi aceito")
	}
	local, err := marketplace.NewOrigin("", "", "/Users/alguem/dev/minhas-tools")
	if err != nil {
		t.Fatal(err)
	}
	if local.Kind != marketplace.OriginLocal || local.Name != "minhas-tools" {
		t.Fatalf("origem local = %+v", local)
	}
}

func TestIndiceLocalDispensaChecksumEURLRemota(t *testing.T) {
	entry := validEntry()
	entry.ManifestURL = ""
	entry.Artifacts = []marketplace.Artifact{{Platform: "darwin-arm64", URL: "dist/darwin-arm64"}}

	local := marketplace.ValidationOptions{
		Categories: map[string]bool{"utilities": true},
		Platforms:  map[string]bool{"darwin-arm64": true},
		Local:      true,
	}
	if err := entry.Validate(local); err != nil {
		t.Fatalf("índice local recusado: %v", err)
	}
	if err := entry.Validate(validationOptions()); err == nil {
		t.Fatal("índice remoto aceitou artefato sem HTTPS")
	}

	entry.Artifacts[0].URL = "../fora"
	if err := entry.Validate(local); err == nil {
		t.Fatal("travessia de diretório foi aceita")
	}
}

func TestSemOrigemHabilitadaOMarketplaceExplicaOMotivo(t *testing.T) {
	service := newService(marketplace.Config{
		Sources: &memorySources{state: marketplace.SourceState{DisabledBuiltins: []string{"lealing"}}},
		Index:   fakeIndex{},
	})
	_, err := service.List(context.Background())
	if err == nil || !strings.Contains(err.Error(), "nenhuma origem") {
		t.Fatalf("List = %v", err)
	}
}
