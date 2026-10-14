package tui

import (
	"fmt"
	"io"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type screen int

const (
	selectScreen screen = iota
	previewScreen
)

var (
	titleStyle    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D7AFF"))
	accentStyle   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#66D9A5"))
	categoryStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#A7A8C7"))
	valueStyle    = lipgloss.NewStyle().Bold(true)
	mutedStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#777A90"))
	kindStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#8AADF4"))
	errorStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#ED8796"))
	successStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#A6DA95"))
)

type workflowModel struct {
	config    Config
	screen    screen
	selector  selectorModel
	preview   previewModel
	result    Result
	err       error
	cancelled bool
	width     int
	height    int
}

func newWorkflow(config Config) workflowModel {
	return workflowModel{
		config:   config,
		selector: newSelector(config.Components),
		width:    80,
		height:   24,
	}
}

func (m workflowModel) Init() tea.Cmd { return nil }

func (m workflowModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.selector.width, m.selector.height = msg.Width, msg.Height
		m.preview.width, m.preview.height = msg.Width, msg.Height
		return m, nil
	case tea.KeyPressMsg:
		return m.updateKey(msg.String())
	}
	return m, nil
}

func (m workflowModel) updateKey(key string) (workflowModel, tea.Cmd) {
	if key == "ctrl+c" {
		m.cancelled = true
		return m, tea.Quit
	}
	if m.screen == selectScreen {
		switch m.selector.handleKey(key) {
		case selectorCancel:
			m.cancelled = true
			return m, tea.Quit
		case selectorSubmit:
			m.result.SelectedIDs = m.selector.selectedIDs()
			if !m.config.Preview {
				m.result.Confirmed = true
				return m, tea.Quit
			}
			if m.config.BuildPlan == nil {
				m.err = fmt.Errorf("TUI preview requires a plan builder")
				return m, tea.Quit
			}
			plan, err := m.config.BuildPlan(m.result.SelectedIDs)
			if err != nil {
				m.err = err
				return m, tea.Quit
			}
			m.result.Plan = plan
			m.preview = newPreview(plan, len(m.result.SelectedIDs), m.config.ConfirmLabel)
			m.preview.width, m.preview.height = m.width, m.height
			m.screen = previewScreen
		}
		return m, nil
	}

	switch m.preview.handleKey(key) {
	case previewBack:
		m.screen = selectScreen
	case previewCancel:
		m.cancelled = true
		return m, tea.Quit
	case previewConfirm:
		m.result.Confirmed = true
		return m, tea.Quit
	}
	return m, nil
}

func (m workflowModel) View() tea.View {
	content := m.selector.view(m.config.Profile)
	if m.screen == previewScreen {
		content = m.preview.view()
	}
	view := tea.NewView(lipgloss.NewStyle().Padding(1, 2).Render(content))
	view.AltScreen = true
	return view
}

func Run(input io.Reader, output io.Writer, config Config) (Result, error) {
	if len(config.Components) == 0 {
		return Result{}, fmt.Errorf("the selected profile has no components supported on this platform")
	}
	program := tea.NewProgram(newWorkflow(config), tea.WithInput(input), tea.WithOutput(output))
	finalModel, err := program.Run()
	if err != nil {
		return Result{}, err
	}
	model, ok := finalModel.(workflowModel)
	if !ok {
		return Result{}, fmt.Errorf("unexpected TUI model %T", finalModel)
	}
	if model.err != nil {
		return Result{}, model.err
	}
	if model.cancelled {
		return Result{}, ErrCancelled
	}
	return model.result, nil
}
