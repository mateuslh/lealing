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
	marketplacescreen "github.com/mateuslh/lealing/internal/adapter/inbound/tui/screen/marketplace"
	"github.com/mateuslh/lealing/internal/adapter/inbound/tui/theme"
	"github.com/mateuslh/lealing/internal/core/domain"
	coremarket "github.com/mateuslh/lealing/internal/core/marketplace"
	"github.com/mateuslh/lealing/internal/core/toolinstall"
	"github.com/mateuslh/lealing/internal/core/toolmanage"
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
func (fakeMarketplace) Install(context.Context, string) (toolinstall.Installation, error) {
	return toolinstall.Installation{}, nil
}
func (f fakeMarketplace) Sources(context.Context) ([]coremarket.Origin, error) { return f.origins, nil }
func (fakeMarketplace) AddSource(context.Context, coremarket.Origin) error     { return nil }
func (fakeMarketplace) RemoveSource(context.Context, string) error             { return nil }
func (fakeMarketplace) SetSourceEnabled(context.Context, string, bool) error   { return nil }

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
	for _, key := range []tea.Msg{
		nil,
		tea.KeyMsg{Type: tea.KeyTab},
		tea.KeyMsg{Type: tea.KeyTab},
	} {
		for _, frame := range frames {
			model := marketplace()
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
}
