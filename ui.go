package main


import (
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var uiKeys = struct {
	Quit key.Binding
}{
	Quit: key.NewBinding(
		key.WithKeys("ctrl+c", "q"),
		key.WithHelp("q", "quit"),
	),
}

type state int

const (
	stateFileManager state = iota
	stateDashboard
	stateProgress
)

// UI is the top-level application model
type UI struct {
	state       state
	fileManager *FileManagerModel
	dashboard   *DashboardModel
	progress    *ProgressModel

	// Error handling
	err error
	
	// Completion state
	done bool
}

func newUI(initialState state, initialDir string) *UI {
	m := &UI{
		state: initialState,
	}

	// Initialize submodels
	if initialState == stateFileManager {
		m.fileManager = newFileManagerModel(initialDir)
	} else if initialState == stateDashboard || initialState == stateProgress {
		m.dashboard = newDashboardModel(initialDir)
		if initialState == stateProgress {
			// Fast forward to progress directly (gogogo)
			m.progress = newProgressModel(initialDir, m.dashboard.getConfig())
		}
	}

	return m
}

func (m *UI) Init() tea.Cmd {
	var cmds []tea.Cmd
	if m.fileManager != nil && m.state == stateFileManager {
		cmds = append(cmds, m.fileManager.Init())
	}
	if m.dashboard != nil && m.state == stateDashboard {
		cmds = append(cmds, m.dashboard.Init())
	}
	if m.progress != nil && m.state == stateProgress {
		cmds = append(cmds, m.progress.Init())
	}
	return tea.Batch(cmds...)
}

// Custom messages to trigger state transitions
type msgSwitchToDashboard struct {
	dir string
}

type msgSwitchToFileManager struct{}

type ConversionConfig struct {
	TargetDir    string
	Album        string
	Artist       string
	Title        string
	OutFilename  string
	CoverSource  int    // index or type of cover
	CoverPath    string // actual path or empty if default
	RemoveSource bool
	BitRate      int
}

type msgStartConversion struct {
	config ConversionConfig
}

type msgConversionDone struct{}

type msgError struct {
	err error
}

func (m *UI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.err != nil || m.done {
			return m, tea.Quit
		}
		if key.Matches(msg, uiKeys.Quit) {
			return m, tea.Quit
		}
	case msgError:
		m.err = msg.err
		return m, nil
	case error:
		m.err = msg
		return m, nil

	case msgSwitchToDashboard:
		m.state = stateDashboard
		m.dashboard = newDashboardModel(msg.dir)
		return m, m.dashboard.Init()

	case msgSwitchToFileManager:
		m.state = stateFileManager
		if m.fileManager == nil {
			m.fileManager = newFileManagerModel(m.dashboard.dir)
		}
		return m, m.fileManager.Init()

	case msgStartConversion:
		m.state = stateProgress
		m.progress = newProgressModel(msg.config.TargetDir, msg.config)
		return m, m.progress.Init()

	case msgConversionDone:
		m.done = true
		return m, nil
	}

	// Dispatch to active sub-model
	switch m.state {
	case stateFileManager:
		if m.fileManager != nil {
			newModel, newCmd := m.fileManager.Update(msg)
			m.fileManager = newModel.(*FileManagerModel)
			cmds = append(cmds, newCmd)
		}
	case stateDashboard:
		if m.dashboard != nil {
			newModel, newCmd := m.dashboard.Update(msg)
			m.dashboard = newModel.(*DashboardModel)
			cmds = append(cmds, newCmd)
		}
	case stateProgress:
		if m.progress != nil {
			newModel, newCmd := m.progress.Update(msg)
			m.progress = newModel.(*ProgressModel)
			cmds = append(cmds, newCmd)
		}
	}

	return m, tea.Batch(cmds...)
}

func (m *UI) View() string {
	var baseView string
	switch m.state {
	case stateFileManager:
		if m.fileManager != nil {
			baseView = m.fileManager.View()
		}
	case stateDashboard:
		if m.dashboard != nil {
			baseView = m.dashboard.View()
		}
	case stateProgress:
		if m.progress != nil {
			baseView = m.progress.View()
		}
	}

	if m.err != nil {
		errorBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("9")). // Red
			Padding(1, 2).
			Render(lipgloss.JoinVertical(lipgloss.Center,
				lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("9")).Render("FATAL ERROR"),
				"",
				m.err.Error(),
				"",
				helpStyle.Render("Press any key to exit"),
			))
		
		return baseView + "\n\n" + errorBox
	}
	
	if m.done {
		successBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("10")). // Green
			Padding(1, 2).
			Render(lipgloss.JoinVertical(lipgloss.Center,
				lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10")).Render("CONVERSION COMPLETE"),
				"",
				"Your audiobook has been created successfully!",
				"",
				helpStyle.Render("Press any key to exit"),
			))
		
		return baseView + "\n\n" + successBox
	}

	if baseView == "" {
		return "Unknown state"
	}
	return baseView
}

// Global UI Styles
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("6")). // Cyan
			Padding(0, 1).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("6"))

	helpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)
