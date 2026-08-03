// Package settings implementa a tela de configuração da engine.
//
// A tela não conhece nenhum ajuste em particular: ela desenha o catálogo de
// campos que o core declara. Adicionar uma configuração nova é acrescentar um
// Field lá — nenhuma linha aqui.
package settings

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	core "github.com/mateuslh/lealing/internal/core/settings"
)

// ScreenID identifica a tela na navegação.
const ScreenID tui.ScreenID = "engine/settings"

// zone é a coluna que detém o teclado.
type zone uint8

const (
	zoneSections zone = iota
	zoneFields
)

type Model struct {
	deps    tui.Deps
	manager core.Manager
	actions []Action

	sections []core.Section
	values   []core.Value
	info     []core.InfoRow

	focus     zone
	section   int
	field     int
	editing   bool
	input     textinput.Model
	restarted map[core.Key]bool

	err     error
	message string
	success bool
}

// Action acrescenta à configuração um fluxo administrativo que não é um
// valor editável. A tela continua sem conhecer capacidades concretas: o
// composition root escolhe a seção, o texto e a factory do destino.
type Action struct {
	Section string
	// Glyph identifica a capacidade na lista, do mesmo jeito que uma seção se
	// identifica na barra lateral. Vazio cai no glifo genérico de navegação.
	Glyph       string
	Label       string
	Description string
	Value       string
	Screen      tui.ScreenFactory
}

type entry struct {
	value    core.Value
	action   Action
	isAction bool
}

var _ tui.Screen = (*Model)(nil)

func New(deps tui.Deps, manager core.Manager, actions ...Action) *Model {
	input := textinput.New()
	input.Prompt = ""
	input.CharLimit = 256

	return &Model{
		deps: deps, manager: manager, actions: append([]Action(nil), actions...), input: input,
		sections: core.Sections(), restarted: map[core.Key]bool{},
	}
}

func (*Model) ID() tui.ScreenID { return ScreenID }
func (*Model) Title() string    { return "configuração" }

func (m *Model) Init() tea.Cmd { return m.reload() }

// Capturing impede que os atalhos globais consumam letras do campo em edição.
func (m *Model) Capturing() bool { return m.editing }

type loadedMsg struct {
	values []core.Value
	info   []core.InfoRow
	err    error
}

// savedMsg fecha o ciclo de uma escrita. A recarga vem depois dela para que a
// tela nunca mostre um valor que o disco recusou.
type savedMsg struct {
	key     core.Key
	message string
	err     error
	restart bool
}

func (m *Model) reload() tea.Cmd {
	manager := m.manager
	return func() tea.Msg {
		if manager == nil {
			return loadedMsg{err: errNotConfigured}
		}
		values, err := manager.All()
		return loadedMsg{values: values, info: manager.Info(), err: err}
	}
}

func (m *Model) save(key core.Key, value string, restart bool) tea.Cmd {
	manager := m.manager
	return func() tea.Msg {
		if err := manager.Set(key, value); err != nil {
			return savedMsg{key: key, err: err}
		}
		return savedMsg{key: key, message: "ajuste salvo", restart: restart}
	}
}

func (m *Model) reset(key core.Key, restart bool) tea.Cmd {
	manager := m.manager
	return func() tea.Msg {
		if err := manager.Reset(key); err != nil {
			return savedMsg{key: key, err: err}
		}
		return savedMsg{key: key, message: "voltou ao padrão", restart: restart}
	}
}

// sectionFields recorta os campos da seção selecionada, preservando a ordem
// declarada no core.
func (m *Model) sectionFields() []core.Value {
	section, ok := m.currentSection()
	if !ok {
		return nil
	}
	fields := make([]core.Value, 0, len(m.values))
	for _, value := range m.values {
		if value.Section == section.ID {
			fields = append(fields, value)
		}
	}
	return fields
}

// sectionEntries lista capacidades antes de campos. É o oposto da ordem de
// declaração porque é o oposto da importância: um fluxo administrativo como a
// sincronização ou a atualização é o que essa seção tem de mais relevante, e
// enterrado depois de um punhado de ajustes é como ele passa despercebido.
func (m *Model) sectionEntries() []entry {
	section, ok := m.currentSection()
	if !ok {
		return nil
	}
	entries := make([]entry, 0, len(m.values)+len(m.actions))
	for _, action := range m.actions {
		if action.Section == section.ID {
			entries = append(entries, entry{action: action, isAction: true})
		}
	}
	for _, value := range m.values {
		if value.Section == section.ID {
			entries = append(entries, entry{value: value})
		}
	}
	return entries
}

func (m *Model) sectionInfo() []core.InfoRow {
	section, ok := m.currentSection()
	if !ok {
		return nil
	}
	rows := make([]core.InfoRow, 0, len(m.info))
	for _, row := range m.info {
		if row.Section == section.ID {
			rows = append(rows, row)
		}
	}
	return rows
}

func (m *Model) currentSection() (core.Section, bool) {
	if m.section < 0 || m.section >= len(m.sections) {
		return core.Section{}, false
	}
	return m.sections[m.section], true
}

func (m *Model) currentField() (core.Value, bool) {
	current, ok := m.currentEntry()
	if !ok || current.isAction {
		return core.Value{}, false
	}
	return current.value, true
}

func (m *Model) currentEntry() (entry, bool) {
	entries := m.sectionEntries()
	if m.field < 0 || m.field >= len(entries) {
		return entry{}, false
	}
	return entries[m.field], true
}

// sectionCount conta os ajustes de uma seção, para a lista da esquerda dizer
// o que há dentro antes de o usuário entrar.
func (m *Model) sectionCount(section core.Section) int {
	count := 0
	for _, value := range m.values {
		if value.Section == section.ID {
			count++
		}
	}
	for _, row := range m.info {
		if row.Section == section.ID {
			count++
		}
	}
	for _, action := range m.actions {
		if action.Section == section.ID {
			count++
		}
	}
	return count
}
