package main

import (
	"context"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var uiKeys = struct {
	Quit  key.Binding
	Theme key.Binding
}{
	Quit: key.NewBinding(
		key.WithKeys("ctrl+c", "q"),
		key.WithHelp("q", "quit"),
	),
	Theme: key.NewBinding(
		key.WithKeys("t", "T"),
		key.WithHelp("t", "theme"),
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
	ctx         context.Context
	state       state
	dir         string
	fileManager *FileManagerModel
	dashboard   *DashboardModel
	progress    *ProgressModel

	// Error handling
	err error

	// Completion state
	done bool

	showQuitConfirm bool

	width  int
	height int

	args Arguments
}

func newUI(ctx context.Context, initialState state, initialDir string, args Arguments) *UI {
	m := &UI{
		ctx:   ctx,
		state: initialState,
		dir:   initialDir,
		args:  args,
	}

	// Initialize submodels
	if initialState == stateFileManager {
		m.fileManager = newFileManagerModel(initialDir)
	} else if initialState == stateDashboard || initialState == stateProgress {
		m.dashboard = newDashboardModel(ctx, initialDir, args.RemoveSource)
		if initialState == stateProgress {
			// No form to show: scan synchronously so the config has tags and bitrate.
			m.dashboard.applyScan(scanDirectory(ctx, initialDir))
			// Fast forward to progress directly (gogogo)
			m.progress = newProgressModel(ctx, initialDir, m.dashboard.getConfig())
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
	Performer    string
	OutFilename  string
	CoverKind    CoverKind
	CoverPath    string // absolute path, used when CoverKind == CoverFile
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

type msgCancelled struct{}

func (m *UI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

		return m, m.resizeChildren()

	case tea.KeyMsg:
		if m.showQuitConfirm {
			if msg.String() == "y" || msg.String() == "Y" {
				return m, tea.Quit
			} else if msg.String() == "n" || msg.String() == "N" || msg.String() == "esc" || msg.String() == "enter" {
				m.showQuitConfirm = false
				return m, nil
			}
			return m, nil
		}

		if m.err != nil {
			return m, tea.Quit
		}

		if msg.String() == "t" || msg.String() == "T" {
			editingText := (m.state == stateDashboard && m.dashboard != nil && m.dashboard.IsEditingText())
			settingFilter := (m.state == stateFileManager && m.fileManager != nil && m.fileManager.list.SettingFilter())

			if !editingText && !settingFilter {
				m.toggleTheme()
				return m, nil
			}
		}

		if msg.String() == "esc" && m.state == stateProgress && m.done {
			return m, func() tea.Msg { return msgSwitchToFileManager{} }
		}

		if key.Matches(msg, uiKeys.Quit) {
			if msg.String() == "q" && m.state == stateDashboard && m.dashboard != nil && m.dashboard.IsEditingText() {
				// Let the dashboard text inputs handle the "q" keystroke
			} else if m.state == stateProgress && !m.done {
				m.showQuitConfirm = true
				return m, nil
			} else {
				return m, tea.Quit
			}
		}
	case msgError:
		if m.state != stateProgress || m.progress == nil {
			m.err = msg.err
			return m, nil
		}
		// A failed conversion ends the run; the progress screen shows the
		// failure and the user can still read the logs, go back or quit.
		m.done = true
	case error:
		m.err = msg
		return m, nil

	case msgSwitchToDashboard:
		m.state = stateDashboard
		m.dir = msg.dir
		m.dashboard = newDashboardModel(m.ctx, msg.dir, m.args.RemoveSource)
		return m, tea.Batch(m.resizeChildren(), m.dashboard.Init())

	case msgSwitchToFileManager:
		if m.state == stateProgress && m.progress != nil {
			m.progress.detach()
		}
		m.state = stateFileManager
		if m.fileManager == nil {
			m.fileManager = newFileManagerModel(m.dir)
		} else {
			// A conversion may have added an .m4b since the listing was loaded.
			m.fileManager.reload()
		}
		m.dir = m.fileManager.dir
		return m, tea.Batch(m.resizeChildren(), m.fileManager.Init())

	case msgStartConversion:
		m.state = stateProgress
		m.dir = msg.config.TargetDir
		m.done = false
		m.progress = newProgressModel(m.ctx, msg.config.TargetDir, msg.config)
		return m, tea.Batch(m.resizeChildren(), m.progress.Init())

	case msgConversionDone:
		m.done = true

	case msgCancelled:
		m.done = true
	}

	// Dispatch to active sub-model
	switch m.state {
	case stateFileManager:
		if m.fileManager != nil {
			newModel, newCmd := m.fileManager.Update(msg)
			m.fileManager = newModel.(*FileManagerModel)
			m.dir = m.fileManager.dir
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

func (m *UI) headerView() string {
	titleText := "P A G E T U R N E R"
	switch m.state {
	case stateFileManager:
		titleText += "  -  F i l e   M a n a g e r"
	case stateDashboard:
		titleText += "  -  A u d i o b o o k   C o n v e r t e r"
	case stateProgress:
		if m.progress != nil && m.progress.showFullLogs {
			titleText += "  -  C o n v e r t i n g   L o g s"
		} else {
			titleText += "  -  C o n v e r t i n g"
		}
	}

	style := lipgloss.NewStyle().
		Bold(true).
		Foreground(primaryColor).
		Padding(0, 1).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(primaryColor)
	// Width includes padding but not the border.
	inner := m.width - docStyle.GetHorizontalFrameSize() - style.GetHorizontalBorderSize()
	if inner < 0 {
		inner = 0
	}
	// Keep the header on one line so the chrome height stays fixed.
	return style.Width(inner).Render(truncateText(titleText, inner-style.GetHorizontalPadding()))
}

func (m *UI) dirBadgeView() string {
	style := lipgloss.NewStyle().
		Background(dirBadgeBg).
		Foreground(dirBadgeFg).
		Padding(0, 1)
	maxText := m.width - docStyle.GetHorizontalFrameSize() - style.GetHorizontalPadding()
	return style.Render(truncateLeft(m.dir, maxText))
}

// chromeView is the header and directory badge shown above every screen.
func (m *UI) chromeView() string {
	return lipgloss.JoinVertical(lipgloss.Left, m.headerView(), "", m.dirBadgeView(), "")
}

// contentSize is the area left for the active screen below the chrome.
func (m *UI) contentSize() (int, int) {
	hf, vf := docStyle.GetFrameSize()
	return max(0, m.width-hf), max(0, m.height-vf-lipgloss.Height(m.chromeView()))
}

// resizeChildren tells every screen the size of its content area.
func (m *UI) resizeChildren() tea.Cmd {
	if m.width == 0 || m.height == 0 {
		return nil
	}
	w, h := m.contentSize()
	msg := tea.WindowSizeMsg{Width: w, Height: h}
	var cmds []tea.Cmd
	if m.fileManager != nil {
		_, cmd := m.fileManager.Update(msg)
		cmds = append(cmds, cmd)
	}
	if m.dashboard != nil {
		_, cmd := m.dashboard.Update(msg)
		cmds = append(cmds, cmd)
	}
	if m.progress != nil {
		_, cmd := m.progress.Update(msg)
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
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
		// Shown as a centered dialog: appending it below the screen would push
		// the view past the terminal height and scroll the header away.
		maxW := max(20, m.width-8)
		errorBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(errorColor).
			Padding(1, 2).
			Render(lipgloss.JoinVertical(lipgloss.Center,
				lipgloss.NewStyle().Bold(true).Foreground(errorColor).Render("FATAL ERROR"),
				"",
				lipgloss.NewStyle().MaxWidth(maxW).Render(m.err.Error()),
				"",
				helpStyle().Render("Press any key to exit"),
			))
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, errorBox)
	}

	if baseView == "" {
		return docStyle.Render("Unknown state")
	}

	fullView := docStyle.Render(lipgloss.JoinVertical(lipgloss.Left, m.chromeView(), baseView))

	if m.showQuitConfirm {
		dialogBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(errorColor).
			Padding(1, 2).
			Render(lipgloss.JoinVertical(lipgloss.Center,
				lipgloss.NewStyle().Bold(true).Foreground(errorColor).Render("Quit Conversion?"),
				"",
				"Conversion is currently running.",
				"Are you sure you want to quit? (y/N)",
			))

		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, dialogBox)
	}

	return fullView
}

var docStyle = lipgloss.NewStyle().Margin(1, 2, 0, 2)

type ThemeKind int

const (
	ThemeDark ThemeKind = iota
	ThemeLight
)

type Theme struct {
	IsDark     bool
	Primary    lipgloss.Color
	Secondary  lipgloss.Color
	Text       lipgloss.Color
	Subtext    lipgloss.Color
	Error      lipgloss.Color
	Success    lipgloss.Color
	Warning    lipgloss.Color
	DirBadgeBg lipgloss.Color
	DirBadgeFg lipgloss.Color
}

var (
	currentThemeKind = ThemeDark

	DarkTheme = Theme{
		IsDark:     true,
		Primary:    lipgloss.Color("#00ADB5"),
		Secondary:  lipgloss.Color("#393E46"),
		Text:       lipgloss.Color("#EEEEEE"),
		Subtext:    lipgloss.Color("#AAAAAA"),
		Error:      lipgloss.Color("#FF2E63"),
		Success:    lipgloss.Color("#00D846"),
		Warning:    lipgloss.Color("#FFB800"),
		DirBadgeBg: lipgloss.Color("#00ADB5"),
		DirBadgeFg: lipgloss.Color("#1A1A1A"),
	}

	LightTheme = Theme{
		IsDark:     false,
		Primary:    lipgloss.Color("#005F73"),
		Secondary:  lipgloss.Color("#CBD5E1"),
		Text:       lipgloss.Color("#0F172A"),
		Subtext:    lipgloss.Color("#64748B"),
		Error:      lipgloss.Color("#DC2626"),
		Success:    lipgloss.Color("#059669"),
		Warning:    lipgloss.Color("#D97706"),
		DirBadgeBg: lipgloss.Color("#005F73"),
		DirBadgeFg: lipgloss.Color("#FFFFFF"),
	}

	primaryColor   lipgloss.Color
	secondaryColor lipgloss.Color
	textColor      lipgloss.Color
	subtextColor   lipgloss.Color
	errorColor     lipgloss.Color
	successColor   lipgloss.Color
	warningColor   lipgloss.Color
	dirBadgeBg     lipgloss.Color
	dirBadgeFg     lipgloss.Color
)

func applyTheme(kind ThemeKind) {
	currentThemeKind = kind
	t := DarkTheme
	if kind == ThemeLight {
		t = LightTheme
	}
	primaryColor = t.Primary
	secondaryColor = t.Secondary
	textColor = t.Text
	subtextColor = t.Subtext
	errorColor = t.Error
	successColor = t.Success
	warningColor = t.Warning
	dirBadgeBg = t.DirBadgeBg
	dirBadgeFg = t.DirBadgeFg
}

func init() {
	applyTheme(ThemeDark)
}

func (m *UI) toggleTheme() {
	if currentThemeKind == ThemeDark {
		applyTheme(ThemeLight)
	} else {
		applyTheme(ThemeDark)
	}
	m.updateAllThemeStyles()
}

func (m *UI) updateAllThemeStyles() {
	if m.fileManager != nil {
		m.fileManager.updateThemeStyles()
	}
	if m.dashboard != nil {
		m.dashboard.updateThemeStyles()
	}
	if m.progress != nil {
		m.progress.updateThemeStyles()
	}
}

func helpStyle() lipgloss.Style {
	return lipgloss.NewStyle().Foreground(subtextColor)
}

func newHelpModel() help.Model {
	h := help.New()
	applyHelpStyles(&h.Styles)
	return h
}

func applyHelpStyles(s *help.Styles) {
	s.ShortKey = lipgloss.NewStyle().Foreground(primaryColor)
	s.ShortDesc = lipgloss.NewStyle().Foreground(subtextColor)
	s.ShortSeparator = lipgloss.NewStyle().Foreground(secondaryColor)
	s.FullKey = lipgloss.NewStyle().Foreground(primaryColor)
	s.FullDesc = lipgloss.NewStyle().Foreground(subtextColor)
	s.FullSeparator = lipgloss.NewStyle().Foreground(secondaryColor)
}
