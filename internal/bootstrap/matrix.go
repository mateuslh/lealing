package bootstrap

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/mateuslh/lealing/internal/adapter/outbound/registry"
	"github.com/mateuslh/lealing/internal/catalog"
	"github.com/mateuslh/lealing/internal/core/domain"
)

// SupportMatrix descreve em quais sistemas cada tool do acervo roda.
//
// É gerada do próprio catálogo, e não escrita à mão em um documento: uma
// tabela em markdown envelhece na primeira tool nova, esta não. O registry
// aqui é montado sem filtro de plataforma de propósito — a matriz precisa
// mostrar o acervo inteiro, não o recorte da máquina que a imprime.
func SupportMatrix(ctx context.Context) (string, error) {
	repo := registry.New(catalog.Providers())
	tools, err := repo.All(ctx)
	if err != nil {
		return "", err
	}

	sorted := make([]domain.Tool, len(tools))
	copy(sorted, tools)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })

	columns := []struct {
		bit   domain.Platform
		title string
	}{
		{domain.Darwin, "macOS"},
		{domain.Windows, "Windows"},
		{domain.Linux, "Linux"},
	}

	current := currentPlatform()
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)

	header := "TOOL"
	for _, c := range columns {
		title := c.title
		if c.bit == current {
			title += " *"
		}
		header += "\t" + title
	}
	fmt.Fprintln(w, header)

	for _, t := range sorted {
		line := string(t.ID)
		for _, c := range columns {
			mark := "—"
			if t.RunsOn(c.bit) {
				mark = "sim"
			}
			line += "\t" + mark
		}
		fmt.Fprintln(w, line)
	}
	if err := w.Flush(); err != nil {
		return "", err
	}

	if current != 0 {
		fmt.Fprintf(&b, "\n* esta máquina (%s)\n", current)
	}
	return b.String(), nil
}
