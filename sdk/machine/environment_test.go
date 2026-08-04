package machine_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/mateuslh/lealing/sdk/machine"
	"github.com/mateuslh/lealing/sdk/protocol"
)

func hostPathPlatform() string {
	// Os diretórios temporários seguem a sintaxe do host que executa o teste.
	if filepath.Separator == '\\' {
		return "windows"
	}
	return "linux"
}

func TestEnvironmentResolveCaminhosConcedidos(t *testing.T) {
	home := t.TempDir()
	data := filepath.Join(t.TempDir(), "data")
	environment := machine.NewEnvironment(protocol.Initialize{
		Platform: hostPathPlatform(), HomeDir: home, DataDir: data,
		Permissions: protocol.Permissions{Filesystem: protocol.FilesystemPermissions{
			Read: []string{filepath.Join(home, ".codex", "sessions")},
		}},
	})

	got, err := environment.ResolveRead(".codex/sessions")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, ".codex", "sessions"); got != want {
		t.Fatalf("ResolveRead = %q, quero %q", got, want)
	}
	if !environment.CanWrite(filepath.Join(data, "state.json")) {
		t.Error("DataDir privado não recebeu escrita implícita")
	}
	private, err := environment.DataPath("nested", "state.json")
	if err != nil || private != filepath.Join(data, "nested", "state.json") {
		t.Fatalf("DataPath = %q, %v", private, err)
	}
	if _, err := environment.DataPath(".."); !errors.Is(err, machine.ErrPermissionDenied) {
		t.Fatalf("travessia privada = %v", err)
	}
	if _, err := environment.ResolveRead(".ssh/id_ed25519"); !errors.Is(err, machine.ErrPermissionDenied) {
		t.Fatalf("erro fora da concessão = %v", err)
	}
}

func TestEnvironmentTrataDiretorioDeTrabalhoConformeONivelPedido(t *testing.T) {
	working := t.TempDir()
	pom := filepath.Join(working, "pom.xml")
	newEnvironment := func(level string, dir string) machine.Environment {
		return machine.NewEnvironment(protocol.Initialize{
			Platform: hostPathPlatform(), HomeDir: t.TempDir(), WorkingDir: dir,
			Permissions: protocol.Permissions{WorkingDir: level},
		})
	}

	semPermissao := newEnvironment("", working)
	if semPermissao.CanUseWorkingDir() {
		t.Error("diretório entregue sem permissão declarada virou concessão")
	}
	if _, err := semPermissao.ResolveRead(pom); !errors.Is(err, machine.ErrPermissionDenied) {
		t.Errorf("leitura sem permissão = %v", err)
	}
	if _, err := semPermissao.WorkingPath("pom.xml"); !errors.Is(err, machine.ErrPermissionDenied) {
		t.Errorf("WorkingPath sem permissão = %v", err)
	}

	leitura := newEnvironment(protocol.WorkingDirRead, working)
	if got, err := leitura.WorkingPath("pom.xml"); err != nil || got != pom {
		t.Fatalf("WorkingPath = %q, %v", got, err)
	}
	if _, err := leitura.ResolveWrite(filepath.Join(working, ".run", "app.run.xml")); !errors.Is(err, machine.ErrPermissionDenied) {
		t.Errorf("read não pode conceder escrita: %v", err)
	}

	escrita := newEnvironment(protocol.WorkingDirWrite, working)
	if _, err := escrita.ResolveWrite(filepath.Join(working, ".run", "app.run.xml")); err != nil {
		t.Errorf("write negou escrita no próprio diretório: %v", err)
	}
	if _, err := escrita.ResolveRead(filepath.Join(working, "..", "vizinho")); !errors.Is(err, machine.ErrPermissionDenied) {
		t.Errorf("escape por .. foi aceito: %v", err)
	}

	// Engine antiga: nível declarado, caminho ausente.
	semCaminho := newEnvironment(protocol.WorkingDirWrite, "")
	if semCaminho.CanUseWorkingDir() {
		t.Error("nível sem caminho não deveria virar concessão")
	}

	// Nível desconhecido de uma engine futura não vira concessão silenciosa.
	desconhecido := newEnvironment("total", working)
	if desconhecido.CanUseWorkingDir() || desconhecido.CanRead(pom) {
		t.Error("nível desconhecido virou concessão")
	}
}

func TestEnvironmentComparaWindowsSemDependerDoHostDoTeste(t *testing.T) {
	environment := machine.NewEnvironment(protocol.Initialize{
		Platform: "WINDOWS", HomeDir: `C:\Users\Ana`,
		Permissions: protocol.Permissions{Filesystem: protocol.FilesystemPermissions{
			Read: []string{`C:\Users\Ana\AppData\Roaming\JetBrains`},
		}},
	})

	if environment.Platform != machine.Windows || environment.Platform.Unix() {
		t.Fatalf("plataforma = %q", environment.Platform)
	}
	if !environment.CanRead(`c:/users/ana/appdata/roaming/jetbrains/IdeaIC2026.1/options`) {
		t.Error("caminho Windows equivalente não foi aceito")
	}
	got, err := environment.ResolveRead(`~/AppData/Roaming/JetBrains`)
	if err != nil {
		t.Fatal(err)
	}
	if got != `C:\Users\Ana\AppData\Roaming\JetBrains` {
		t.Fatalf("ResolveRead Windows = %q", got)
	}
}

func TestEnvironmentAceitaConcessaoDeEngineAntigaPorSufixo(t *testing.T) {
	environment := machine.NewEnvironment(protocol.Initialize{
		Platform: "linux",
		Permissions: protocol.Permissions{Filesystem: protocol.FilesystemPermissions{
			Read: []string{"/home/ana/.claude/projects"},
		}},
	})
	environment.HomeDir = ""

	got, err := environment.ResolveRead(".claude/projects")
	if err != nil || got != "/home/ana/.claude/projects" {
		t.Fatalf("fallback = %q, %v", got, err)
	}
}

func TestParsePlatform(t *testing.T) {
	for _, platform := range []machine.Platform{machine.Darwin, machine.Linux, machine.Windows} {
		if got := machine.ParsePlatform(string(platform)); got != platform || !got.Valid() {
			t.Errorf("ParsePlatform(%q) = %q", platform, got)
		}
	}
	if machine.ParsePlatform("plan9").Valid() {
		t.Error("plataforma desconhecida aceita")
	}
	if machine.ParseArchitecture("ARM64") != machine.ARM64 || machine.ParseArchitecture("386") != "" {
		t.Error("arquitetura não foi normalizada")
	}
}

func TestSelectRecusaFallbackDePlataforma(t *testing.T) {
	got, err := machine.Select(machine.Linux, map[machine.Platform]func() string{
		machine.Darwin:  func() string { return "mac" },
		machine.Windows: func() string { return "windows" },
	})
	if err == nil || got != "" {
		t.Fatalf("Select = %q, %v", got, err)
	}
	got, err = machine.Select(machine.Darwin, map[machine.Platform]func() string{
		machine.Darwin: func() string { return "mac" },
	})
	if err != nil || got != "mac" {
		t.Fatalf("Select Darwin = %q, %v", got, err)
	}
}
