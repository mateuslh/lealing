// Command lealing-tool-token-usage executa a tool Uso de Tokens via screen-v1.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	coretokens "github.com/mateuslh/lealing/cmd/tools/token-usage/internal/tokens"
	ui "github.com/mateuslh/lealing/cmd/tools/token-usage/internal/ui"
	"github.com/mateuslh/lealing/cmd/tools/token-usage/internal/usage"
	"github.com/mateuslh/lealing/sdk/component"
	"github.com/mateuslh/lealing/sdk/protocol"
	"github.com/mateuslh/lealing/sdk/screen"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx); err != nil {
		// stdout pertence exclusivamente ao framing; qualquer diagnóstico da
		// tool precisa permanecer em stderr.
		fmt.Fprintln(os.Stderr, "token-usage:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	return screen.Run(ctx, screen.Config{
		ToolVersion: version,
		Protocol:    protocol.VersionRange{Min: protocol.Version1, Max: protocol.Version1},
		Factory: func(session screen.Session) screen.Model {
			var providers []coretokens.Provider
			if root := grantedRead(session.Initialize, ".claude/projects"); root != "" {
				credentialsPath := grantedRead(session.Initialize, ".claude/.credentials.json")
				credentials := usage.NewLocalCredentials(credentialsPath,
					session.Initialize.Platform == "darwin" && session.Initialize.Permissions.Subprocess && credentialsPath != "")
				claude := usage.NewClaudeCode(root, credentials)
				if !session.Initialize.Permissions.Network || credentialsPath == "" {
					claude.Quota = nil
				}
				providers = append(providers, claude)
			}
			if root := grantedRead(session.Initialize, ".codex/sessions"); root != "" {
				credentialsPath := grantedRead(session.Initialize, ".codex/auth.json")
				codex := usage.NewCodex(root, usage.NewCodexFile(credentialsPath))
				if !session.Initialize.Permissions.Network || credentialsPath == "" {
					codex.Quota = nil
				}
				providers = append(providers, codex)
			}
			service := coretokens.NewService(nil, providers...)
			return ui.New(component.ThemeFrom(session.Initialize.Theme), service, nil)
		},
	})
}

func grantedRead(initialize protocol.Initialize, suffix string) string {
	want := filepath.Clean(filepath.FromSlash(suffix))
	for _, granted := range initialize.Permissions.Filesystem.Read {
		clean := filepath.Clean(granted)
		if clean == want || strings.HasSuffix(clean, string(filepath.Separator)+want) {
			return clean
		}
	}
	return ""
}
