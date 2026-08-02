// Package usersync guarda as preferências do usuário em um repositório
// privado dele no GitHub, para que uma máquina nova chegue configurada.
//
// O núcleo não conhece HTTP, git nem chaveiro: identidade, estado remoto e
// estado local entram por portas. O que ele contém é a política — o que pode
// ser sincronizado, como um conflito é detectado e o que nunca sai da
// máquina.
package usersync

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// StateVersion marca o formato do documento. Ler um documento de versão
// futura é recusado em vez de interpretado pela metade: preferências
// entendidas errado voltam para o disco erradas, e o push seguinte propaga o
// estrago para as outras máquinas.
const StateVersion = 1

// RemotePath é onde o documento mora dentro do repositório.
const RemotePath = "state.json"

// DefaultRepository é o repositório privado criado na primeira sincronização.
const DefaultRepository = "lealing-state"

var (
	ErrNotAuthenticated = errors.New("nenhuma conta do GitHub conectada")
	// ErrConflict indica que o remoto mudou desde a última leitura. Nunca é
	// resolvido sozinho: fundir contadores de uso produz números que não
	// aconteceram, e escolher um lado em silêncio descarta o trabalho do
	// outro.
	ErrConflict = errors.New("o estado remoto mudou desde a última leitura")
	ErrNoRemote = errors.New("ainda não há estado publicado")
)

// ToolUsage é a estatística de uma tool, no formato do documento. O domínio
// da engine não é reaproveitado aqui de propósito: este é um contrato
// serializado que precisa sobreviver a refatorações internas.
type ToolUsage struct {
	ID       string    `json:"id"`
	Runs     int       `json:"runs"`
	LastRun  time.Time `json:"lastRun,omitempty"`
	Favorite bool      `json:"favorite"`
}

// InstalledTool é uma tool externa presente na máquina de origem. Só a
// referência viaja; binário nenhum é sincronizado.
type InstalledTool struct {
	ID      string `json:"id"`
	Version string `json:"version"`
	Origin  string `json:"origin,omitempty"`
}

// MarketplaceSource é um repositório de tools cadastrado.
type MarketplaceSource struct {
	Name    string `json:"name"`
	Label   string `json:"label,omitempty"`
	Kind    string `json:"kind"`
	Ref     string `json:"ref"`
	Enabled bool   `json:"enabled"`
}

// State é o documento inteiro. Cada seção é opcional: um usuário pode
// desligar a sincronização de origens e manter a de favoritos.
type State struct {
	Version int `json:"version"`
	// UpdatedAt e Device dizem de onde veio a última escrita. É o que a tela
	// mostra antes de sobrescrever algo — "enviado ontem pelo mac-do-trabalho"
	// responde à única pergunta que importa na hora de decidir.
	UpdatedAt time.Time `json:"updatedAt"`
	Device    string    `json:"device,omitempty"`
	Engine    string    `json:"engine,omitempty"`

	Usage   []ToolUsage         `json:"usage,omitempty"`
	Sources []MarketplaceSource `json:"sources,omitempty"`
	// DisabledBuiltins acompanha as origens: desligar o índice oficial é uma
	// preferência tanto quanto adicionar um repositório.
	DisabledBuiltins []string        `json:"disabledBuiltins,omitempty"`
	Tools            []InstalledTool `json:"tools,omitempty"`
}

// Section nomeia uma parte sincronizável. O usuário liga e desliga cada uma.
type Section string

const (
	SectionUsage   Section = "usage"
	SectionSources Section = "sources"
	SectionTools   Section = "tools"
)

// AllSections é a ordem em que as seções aparecem na interface.
var AllSections = []Section{SectionUsage, SectionSources, SectionTools}

func (s Section) Label() string {
	switch s {
	case SectionUsage:
		return "favoritos e uso"
	case SectionSources:
		return "origens do marketplace"
	case SectionTools:
		return "tools instaladas"
	default:
		return string(s)
	}
}

func (s Section) Valid() bool {
	for _, known := range AllSections {
		if known == s {
			return true
		}
	}
	return false
}

// Selection são as seções habilitadas. O zero-value não sincroniza nada, o
// que é o padrão certo para um recurso que envia dados para fora da máquina.
type Selection map[Section]bool

// DefaultSelection liga o que o usuário escolheu ao ativar a sincronização.
func DefaultSelection() Selection {
	selection := make(Selection, len(AllSections))
	for _, section := range AllSections {
		selection[section] = true
	}
	return selection
}

func (s Selection) Enabled(section Section) bool { return s != nil && s[section] }

func (s Selection) Empty() bool {
	for _, section := range AllSections {
		if s.Enabled(section) {
			return false
		}
	}
	return true
}

// Validate recusa um documento que a engine não saberia aplicar.
func (s State) Validate() error {
	if s.Version > StateVersion {
		return fmt.Errorf(
			"o estado remoto foi escrito por uma versão mais nova do lealing (formato %d)", s.Version)
	}
	seenUsage := make(map[string]bool, len(s.Usage))
	for _, usage := range s.Usage {
		switch {
		case strings.TrimSpace(usage.ID) == "":
			return errors.New("estado remoto tem uso sem ID de tool")
		case usage.Runs < 0:
			return fmt.Errorf("contagem de uso negativa em %s", usage.ID)
		case seenUsage[usage.ID]:
			return fmt.Errorf("uso duplicado no estado remoto: %s", usage.ID)
		}
		seenUsage[usage.ID] = true
	}
	seenSource := make(map[string]bool, len(s.Sources))
	for _, source := range s.Sources {
		if strings.TrimSpace(source.Name) == "" || strings.TrimSpace(source.Ref) == "" {
			return errors.New("estado remoto tem origem sem nome ou endereço")
		}
		if seenSource[source.Name] {
			return fmt.Errorf("origem duplicada no estado remoto: %s", source.Name)
		}
		seenSource[source.Name] = true
	}
	return nil
}

// Normalize ordena as coleções para que dois envios do mesmo conteúdo
// produzam bytes idênticos — sem isso, o histórico do repositório encheria de
// commits que só embaralham linhas.
func (s *State) Normalize() {
	sort.Slice(s.Usage, func(i, j int) bool { return s.Usage[i].ID < s.Usage[j].ID })
	sort.Slice(s.Sources, func(i, j int) bool { return s.Sources[i].Name < s.Sources[j].Name })
	sort.Slice(s.Tools, func(i, j int) bool { return s.Tools[i].ID < s.Tools[j].ID })
	sort.Strings(s.DisabledBuiltins)
}

// Filter devolve o documento com apenas as seções habilitadas. É aplicado
// tanto na saída quanto na entrada: desligar uma seção precisa impedir que
// ela seja enviada e que seja aplicada por cima do local.
func (s State) Filter(selection Selection) State {
	filtered := State{
		Version: s.Version, UpdatedAt: s.UpdatedAt, Device: s.Device, Engine: s.Engine,
	}
	if selection.Enabled(SectionUsage) {
		filtered.Usage = s.Usage
	}
	if selection.Enabled(SectionSources) {
		filtered.Sources = s.Sources
		filtered.DisabledBuiltins = s.DisabledBuiltins
	}
	if selection.Enabled(SectionTools) {
		filtered.Tools = s.Tools
	}
	return filtered
}

// Summary conta o que o documento carrega, para a tela dizer o tamanho do
// que está prestes a ser sobrescrito.
func (s State) Summary() map[Section]int {
	return map[Section]int{
		SectionUsage:   len(s.Usage),
		SectionSources: len(s.Sources),
		SectionTools:   len(s.Tools),
	}
}

// Empty informa se não há nada para enviar.
func (s State) Empty() bool {
	return len(s.Usage) == 0 && len(s.Sources) == 0 && len(s.Tools) == 0 &&
		len(s.DisabledBuiltins) == 0
}
