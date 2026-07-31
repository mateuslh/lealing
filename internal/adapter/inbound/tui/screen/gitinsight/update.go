package gitinsight

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mateuslh/lealing/internal/adapter/inbound/tui"
	core "github.com/mateuslh/lealing/internal/core/gitinsight"
)

// Update implementa tui.Screen.
func (m *Model) Update(msg tea.Msg) (tui.Screen, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case reportMsg:
		m.loading = false
		m.report, m.err = msg.report, msg.err
		m.clampCursor()
		return m, nil

	case actionMsg:
		m.mode, m.action = modeBrowse, actionNone
		m.operation = operation{}
		if msg.err != nil {
			m.feedback = fmt.Sprintf("%s: %s", actionLabel(msg.operation.action), firstLine(msg.err.Error()))
			m.feedbackErr = true
			return m, nil
		}
		if msg.operation.action == actionUpdateAll {
			m.mode = modeResults
			m.updateReport = msg.updateReport
			m.resultCursor = 0
			m.feedback = ""
			m.feedbackErr = false
			return m, nil
		}
		m.feedback = actionSuccess(msg.operation)
		m.feedbackErr = false
		m.loading, m.err = true, nil
		m.detailOffset = 0
		return m, m.load()

	case tea.KeyMsg:
		if m.mode != modeBrowse {
			return m.updateAction(msg)
		}
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
				m.detailOffset = 0
			}
		case "down", "j":
			if m.cursor < len(m.repositories())-1 {
				m.cursor++
				m.detailOffset = 0
			}
		case "tab":
			m.filter = (m.filter + 1) % filterCount
			m.cursor, m.detailOffset = 0, 0
		case "shift+tab":
			m.filter = (m.filter + filterCount - 1) % filterCount
			m.cursor, m.detailOffset = 0, 0
		case "left", "h":
			m.filter = (m.filter + filterCount - 1) % filterCount
			m.cursor, m.detailOffset = 0, 0
		case "right", "l":
			m.filter = (m.filter + 1) % filterCount
			m.cursor, m.detailOffset = 0, 0
		case "pgup", "ctrl+u":
			m.detailOffset = max(m.detailOffset-1, 0)
		case "pgdown", "ctrl+d":
			if repo, ok := m.selected(); ok {
				// Títulos e respiros acrescentam até cinco linhas além das
				// duas linhas de cada branch.
				m.detailOffset = min(m.detailOffset+1, max(len(repo.Branches)*2+5, 0))
			}
		case "1", "2", "3", "4", "5":
			m.filter = filter(int(msg.String()[0] - '1'))
			m.cursor, m.detailOffset = 0, 0
		case "r", "ctrl+r":
			m.loading, m.err = true, nil
			m.feedback = ""
			m.detailOffset = 0
			return m, m.load()
		case "p":
			m.openPicker(actionPush)
		case "d":
			m.openPicker(actionDelete)
		case "f":
			repo, ok := m.selected()
			if !ok {
				break
			}
			m.mode = modeRunning
			m.operation = operation{action: actionFetch, repo: repo}
			m.feedback = ""
			return m, m.run(m.operation)
		case "u":
			m.mode = modeConfirm
			m.operation = operation{
				action: actionUpdateAll,
				total:  len(m.report.Repositories),
			}
			m.feedback = ""
		}
	}
	return m, nil
}

func (m *Model) openPicker(next action) {
	repo, ok := m.selected()
	if !ok {
		return
	}
	m.action = next
	m.actionCursor = 0
	m.feedbackErr = false
	switch next {
	case actionPush:
		if len(repo.PushBranches()) == 0 {
			m.feedback = "nenhuma branch pendente de push neste clone"
			return
		}
	case actionDelete:
		if len(repo.CleanupBranches()) == 0 {
			m.feedback = "nenhuma branch local segura para remover neste clone"
			return
		}
	}
	m.feedback = ""
	m.mode = modePick
}

func (m *Model) updateAction(msg tea.KeyMsg) (tui.Screen, tea.Cmd) {
	switch m.mode {
	case modePick:
		branches := m.actionBranches()
		switch msg.String() {
		case "up", "k":
			m.actionCursor = max(m.actionCursor-1, 0)
		case "down", "j":
			m.actionCursor = min(m.actionCursor+1, max(len(branches)-1, 0))
		case "enter":
			repo, ok := m.selected()
			if ok && m.actionCursor < len(branches) {
				m.operation = operation{
					action: m.action,
					repo:   repo,
					branch: branches[m.actionCursor],
				}
				m.mode = modeConfirm
			}
		case "esc":
			m.cancelAction()
		}

	case modeConfirm:
		switch msg.String() {
		case "s", "y", "enter":
			m.mode = modeRunning
			return m, m.run(m.operation)
		case "n", "esc":
			m.cancelAction()
		}

	case modeResults:
		switch msg.String() {
		case "up", "k":
			m.resultCursor = max(m.resultCursor-1, 0)
		case "down", "j":
			m.resultCursor = min(m.resultCursor+1, max(len(m.updateReport.Results)-1, 0))
		case "enter", "esc", "r":
			m.mode = modeBrowse
			m.updateReport = core.UpdateReport{}
			m.loading, m.err = true, nil
			return m, m.load()
		}
	}
	return m, nil
}

func (m *Model) cancelAction() {
	m.mode, m.action = modeBrowse, actionNone
	m.operation = operation{}
	m.feedback = "ação cancelada"
	m.feedbackErr = false
}

func actionLabel(selected action) string {
	switch selected {
	case actionPush:
		return "push"
	case actionDelete:
		return "remoção local"
	case actionFetch:
		return "fetch"
	case actionUpdateAll:
		return "atualização geral"
	default:
		return "ação"
	}
}

func actionSuccess(done operation) string {
	switch done.action {
	case actionPush:
		return "push concluído: " + done.operationBranch()
	case actionDelete:
		return "branch local removida: " + done.operationBranch()
	case actionFetch:
		return "remotos atualizados: " + done.repo.Relative
	default:
		return "ação concluída"
	}
}

func (o operation) operationBranch() string {
	if o.repo.Relative == "" {
		return o.branch.Name
	}
	return o.repo.Relative + " · " + o.branch.Name
}
