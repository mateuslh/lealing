// Package screen_test verifica a geometria das telas administrativas da engine.
// Tools de domínio possuem a própria matriz nos repositórios externos.
package screen_test

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	accountsyncscreen "github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/accountsync"
	marketplacescreen "github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/marketplace"
	settingsscreen "github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/settings"
	updatescreen "github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/update"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	"github.com/mateuslh/lealing/internal/core/domain"
	coremarket "github.com/mateuslh/lealing/internal/core/marketplace"
	"github.com/mateuslh/lealing/internal/core/selfupdate"
	coresettings "github.com/mateuslh/lealing/internal/core/settings"
	"github.com/mateuslh/lealing/internal/core/toolinstall"
	"github.com/mateuslh/lealing/internal/core/toolmanage"
	"github.com/mateuslh/lealing/internal/core/usersync"
)

var frames = []tui.Frame{
	{Width: 200, Height: 60}, {Width: 150, Height: 42}, {Width: 120, Height: 36},
	{Width: 100, Height: 30}, {Width: 84, Height: 26}, {Width: 70, Height: 22},
	{Width: 50, Height: 16}, {Width: 34, Height: 12}, {Width: 26, Height: 8},
}

type fakeMarketplace struct {
	tools   []coremarket.Listing
	origins []coremarket.Origin
}

func (f fakeMarketplace) Catalog(context.Context) (coremarket.Catalog, error) {
	return coremarket.Catalog{Tools: f.tools}, nil
}
func (f fakeMarketplace) List(context.Context) ([]coremarket.Listing, error) { return f.tools, nil }
func (fakeMarketplace) Install(context.Context, string, coremarket.InstallOptions) (toolinstall.Installation, error) {
	return toolinstall.Installation{}, nil
}
func (f fakeMarketplace) Sources(context.Context) ([]coremarket.Origin, error) { return f.origins, nil }
func (fakeMarketplace) AddSource(context.Context, coremarket.Origin) error     { return nil }
func (fakeMarketplace) RemoveSource(_ context.Context, name string) (coremarket.SourceRemoval, error) {
	return coremarket.SourceRemoval{Name: name}, nil
}
func (fakeMarketplace) SetSourceEnabled(context.Context, string, bool) error { return nil }

type fakeManagement []toolmanage.Item

func (f fakeManagement) List(context.Context) ([]toolmanage.Item, error)     { return f, nil }
func (fakeManagement) SetEnabled(context.Context, domain.ToolID, bool) error { return nil }
func (fakeManagement) Remove(_ context.Context, id domain.ToolID) (toolinstall.Removal, error) {
	return toolinstall.Removal{ID: string(id), RecoveryDir: "/tools/.trash/" + string(id)}, nil
}

func marketplace() tui.Screen {
	origin := coremarket.Origin{
		Name: "lealing", Label: "índice padrão", Kind: coremarket.OriginRemote,
		Ref: "https://example.test/index.json", Builtin: true, Trusted: true, Enabled: true,
	}
	listing := coremarket.Listing{Entry: coremarket.Entry{
		ID: "example-tool", Version: "1.0.1", Name: "Example Tool",
		Summary: "Demonstra uma extensão externa.", Publisher: "example",
		DistributionTier: coremarket.ChannelOfficial, Risk: "safe", Origin: origin,
		Protocol: coremarket.VersionRange{Min: 1, Max: 1},
	}, InstalledVersion: "1.0.0", UpdateAvailable: true}
	model := tui.Screen(marketplacescreen.New(
		tui.Deps{Theme: theme.Default()},
		fakeMarketplace{tools: []coremarket.Listing{listing}, origins: []coremarket.Origin{origin}},
		fakeManagement{
			{Tool: domain.Tool{ID: "example-tool", Name: "Example Tool", Kind: domain.KindProcess}, Enabled: true, Installed: true, ActiveVersion: "1.0.0"},
			{Tool: domain.Tool{ID: "another-tool", Name: "Another Tool", Kind: domain.KindProcess}, Installed: true, ActiveVersion: "1.0.0"},
		},
	))
	return drain(model, model.Init())
}

type fakeSync struct {
	status usersync.Status
	code   usersync.DeviceCode
}

func (f fakeSync) Status(context.Context) (usersync.Status, error) { return f.status, nil }
func (f fakeSync) StartLogin(context.Context) (usersync.DeviceCode, error) {
	return f.code, nil
}
func (fakeSync) CompleteLogin(context.Context, usersync.DeviceCode) (usersync.Identity, error) {
	return usersync.Identity{}, nil
}
func (fakeSync) Logout(context.Context) error { return nil }
func (fakeSync) Push(context.Context, bool) (usersync.Result, error) {
	return usersync.Result{}, nil
}
func (fakeSync) Pull(context.Context, bool, bool) (usersync.Result, error) {
	return usersync.Result{}, nil
}
func (fakeSync) SetSection(context.Context, usersync.Section, bool) error { return nil }

func syncStatus() usersync.Status {
	return usersync.Status{
		Connected: true, Identity: usersync.Identity{Login: "alguem", Name: "Alguém"},
		Repository: "lealing-state", Selection: usersync.DefaultSelection(),
		Local: usersync.State{
			Usage:   []usersync.ToolUsage{{Host: "lealing", ID: "example-tool"}},
			Sources: []usersync.MarketplaceSource{{Name: "example", Kind: "local", Ref: "/tmp/example"}},
			Tools:   []usersync.InstalledTool{{Host: "lealing", ID: "another-tool", Version: "1.0.0"}},
		},
		Remote: usersync.State{Usage: []usersync.ToolUsage{{Host: "lealing", ID: "example-tool"}}},
	}
}

func accountSync() tui.Screen {
	model := tui.Screen(accountsyncscreen.New(
		tui.Deps{Theme: theme.Default()}, fakeSync{status: syncStatus()}, nil,
	))
	return drain(model, model.Init())
}

func accountSyncDevice() tui.Screen {
	model := tui.Screen(accountsyncscreen.New(
		tui.Deps{Theme: theme.Default()}, fakeSync{code: usersync.DeviceCode{
			UserCode: "ABCD-1234", VerificationURL: "https://github.com/login/device",
		}}, nil,
	))
	model = drain(model, model.Init())
	model, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if command == nil {
		return model
	}
	model, _ = model.Update(command())
	return model
}

type fakeSettings struct{ values []coresettings.Value }

func (f fakeSettings) All() ([]coresettings.Value, error) { return f.values, nil }
func (f fakeSettings) Get(key coresettings.Key) (coresettings.Value, error) {
	for _, value := range f.values {
		if value.Key == key {
			return value, nil
		}
	}
	return coresettings.Value{}, coresettings.ErrUnknownField
}
func (fakeSettings) Set(coresettings.Key, string) error { return nil }
func (fakeSettings) Reset(coresettings.Key) error       { return nil }
func (fakeSettings) Info() []coresettings.InfoRow       { return nil }

func settings() tui.Screen {
	fields := coresettings.Fields()
	values := make([]coresettings.Value, 0, len(fields))
	for _, field := range fields {
		values = append(values, coresettings.Value{Field: field, Current: field.Default})
	}
	model := tui.Screen(settingsscreen.New(
		tui.Deps{Theme: theme.Default()}, fakeSettings{values: values},
		settingsscreen.Action{
			Section: coresettings.SectionAccount.ID, Label: "Sincronização do GitHub",
			Description: "Gerencie a conta e as preferências sincronizadas.",
			Screen:      func() tui.Screen { return accountSync() },
		},
	))
	return drain(model, model.Init())
}

type fakeSelfUpdate struct {
	status selfupdate.Status
}

func (f fakeSelfUpdate) Check(context.Context) (selfupdate.Status, error) { return f.status, nil }
func (fakeSelfUpdate) Apply(context.Context, selfupdate.Status) (selfupdate.Outcome, error) {
	return selfupdate.Outcome{}, nil
}

func updateScreen() tui.Screen {
	status := selfupdate.Status{
		Install: selfupdate.Install{Mode: selfupdate.ModeRelease, BinaryPath: "/opt/lealing/lealing", Writable: true},
		Current: selfupdate.ParseVersion("v1.0.0"),
		Latest:  selfupdate.Release{Tag: "v1.1.0", Notes: "Notas do último lançamento."},
		State:   selfupdate.StateOutdated,
	}
	model := tui.Screen(updatescreen.New(
		tui.Deps{Theme: theme.Default()}, fakeSelfUpdate{status: status}, "/home/alguem", nil,
	))
	return drain(model, model.Init())
}

func drain(model tui.Screen, command tea.Cmd) tui.Screen {
	if command == nil {
		return model
	}
	message := command()
	if batch, ok := message.(tea.BatchMsg); ok {
		for _, child := range batch {
			model = drain(model, child)
		}
		return model
	}
	if message != nil {
		model, _ = model.Update(message)
	}
	return model
}

func TestTelasAdministrativasNuncaEstouramOFrame(t *testing.T) {
	for name, factory := range map[string]func() tui.Screen{
		"marketplace":                 marketplace,
		"configuração":                settings,
		"sincronização":               accountSync,
		"sincronização durante login": accountSyncDevice,
		"atualização":                 updateScreen,
	} {
		t.Run(name, func(t *testing.T) {
			for _, key := range []tea.Msg{
				nil,
				tea.KeyMsg{Type: tea.KeyTab},
				tea.KeyMsg{Type: tea.KeyTab},
			} {
				for _, frame := range frames {
					model := factory()
					if key != nil {
						model, _ = model.Update(key)
					}
					view := model.View(frame)
					lines := strings.Split(view, "\n")
					if len(lines) > frame.Height {
						t.Fatalf("%dx%d: %d linhas", frame.Width, frame.Height, len(lines))
					}
					for row, line := range lines {
						if width := lipgloss.Width(line); width > frame.Width {
							t.Fatalf("%dx%d linha %d mede %d", frame.Width, frame.Height, row, width)
						}
					}
				}
			}
		})
	}
}
