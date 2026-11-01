package main

import (
	"context"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Global keys, handled by UI before the active screen sees the key.
var uiKeys = struct {
	Quit      key.Binding
	ForceQuit key.Binding
	Theme     key.Binding
}{
	Quit: key.NewBinding(
		key.WithKeys("q"),
		key.WithHelp("q", "quit"),
	),
	ForceQuit: key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "quit"),
	),
	Theme: key.NewBinding(
		key.WithKeys("t", "T"),
		key.WithHelp("t", "theme"),
	),
}

// Keys for (y/N) dialogs. Enter means No, as the capital N promises.
var confirmKeys = struct {
	Yes key.Binding
	No  key.Binding
}{
	Yes: key.NewBinding(key.WithKeys("y", "Y")),
	No:  key.NewBinding(key.WithKeys("n", "N", "esc", "enter")),
}

// globalHelp returns the global bindings for a screen's help bar. While a
// text field captures keys, q and t are typed instead, so only ctrl+c is shown.
func globalHelp(capturing bool) []key.Binding {
	quit, force, theme := uiKeys.Quit, uiKeys.ForceQuit, uiKeys.Theme
	quit.SetEnabled(!capturing)
	theme.SetEnabled(!capturing)
	force.SetEnabled(capturing)
	return []key.Binding{quit, force, theme}
}

type state int

const (
	stateFileManager state = iota
	stateDashboard
	stateProgress
)

// UI is the top-level application model. Screens are plain structs with
// Init/Update/View/SetSize; only UI implements tea.Model.
type UI struct {
	ctx         context.Context
	styles      *Styles
	state       state
	dir         string
	fileManager *FileManagerModel
	dashboard   *DashboardModel
	progress    *ProgressModel

	err             error // fatal error, shown as a dialog
	showQuitConfirm bool

	width  int
	height int

	args Arguments
}

func newUI(ctx context.Context, initialState state, initialDir string, args Arguments) *UI {
	m := &UI{
		ctx:    ctx,
		styles: newStyles(defaultPalette),
		state:  initialState,
		dir:    initialDir,
		args:   args,
	}

	switch initialState {
	case stateFileManager:
		m.fileManager = newFileManagerModel(m.styles, initialDir)
	case stateDashboard, stateProgress:
		m.dashboard = newDashboardModel(ctx, m.styles, initialDir, nil, args.mode(), args.RemoveSource)
		if initialState == stateProgress {
			// No form to show (gogogo): scan synchronously so the config has tags and bitrate.
			m.dashboard.applyScan(scanDirectory(ctx, initialDir))
			m.progress = newProgressModel(ctx, m.styles, initialDir, m.dashboard.getConfig())
		}
	}
	return m
}

func (m *UI) Init() tea.Cmd {
	switch m.state {
	case stateFileManager:
		return m.fileManager.Init()
	case stateDashboard:
		return m.dashboard.Init()
	case stateProgress:
		return m.progress.Init()
	}
	return nil
}

// Custom messages to trigger state transitions
type msgSwitchToDashboard struct {
	dir   string
	files []string // selected mp3s (absolute); nil converts all of dir
	mode  ConversionMode
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
	Mode         ConversionMode
	Files        []string // stories mode: absolute paths of the MP3s to convert
}

type msgStartConversion struct {
	config ConversionConfig
}

// msgConversionDone ends a successful run and lists what was written.
type msgConversionDone struct {
	outputs []outputInfo
}

type msgError struct {
	err error
}

type msgCancelled struct{}

// capturingInput reports whether the active screen is typing text, in which
// case single-letter global shortcuts (q, t) must reach the screen.
func (m *UI) capturingInput() bool {
	switch m.state {
	case stateDashboard:
		return m.dashboard != nil && m.dashboard.IsEditingText()
	case stateFileManager:
		return m.fileManager != nil && m.fileManager.IsFiltering()
	}
	return false
}

func (m *UI) runFinished() bool {
	return m.progress == nil || m.progress.Finished()
}

func (m *UI) quit() (tea.Model, tea.Cmd) {
	if m.state == stateProgress && !m.runFinished() {
		m.showQuitConfirm = true
		return m, nil
	}
	return m, tea.Quit
}

func (m *UI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.resizeChildren()
		return m, nil

	case tea.KeyMsg:
		if m.showQuitConfirm {
			switch {
			case key.Matches(msg, confirmKeys.Yes):
				return m, tea.Quit
			case key.Matches(msg, confirmKeys.No):
				m.showQuitConfirm = false
			}
			return m, nil
		}

		if m.err != nil {
			return m, tea.Quit
		}

		capturing := m.capturingInput()
		switch {
		case key.Matches(msg, uiKeys.ForceQuit):
			return m.quit()
		case key.Matches(msg, uiKeys.Quit) && !capturing:
			return m.quit()
		case key.Matches(msg, uiKeys.Theme) && !capturing:
			toggleTheme()
			return m, nil
		case key.Matches(msg, progKeys.Back) && m.state == stateProgress && m.runFinished():
			return m, func() tea.Msg { return msgSwitchToFileManager{} }
		}

	case msgError:
		if m.state != stateProgress || m.progress == nil {
			m.err = msg.err
			return m, nil
		}
		// A failed conversion ends the run; the progress screen shows the
		// failure and the user can still read the logs, go back or quit.

	case error:
		m.err = msg
		return m, nil

	case msgSwitchToDashboard:
		m.state = stateDashboard
		m.dir = msg.dir
		m.dashboard = newDashboardModel(m.ctx, m.styles, msg.dir, msg.files, msg.mode, m.args.RemoveSource)
		m.resizeChildren()
		return m, m.dashboard.Init()

	case msgSwitchToFileManager:
		if m.state == stateProgress && m.progress != nil {
			m.progress.detach()
		}
		m.state = stateFileManager
		if m.fileManager == nil {
			m.fileManager = newFileManagerModel(m.styles, m.dir)
		} else {
			// A conversion may have added an .m4b since the listing was loaded.
			m.fileManager.reload()
		}
		m.dir = m.fileManager.dir
		m.resizeChildren()
		return m, m.fileManager.Init()

	case msgStartConversion:
		if m.fileManager != nil {
			m.fileManager.clearSelection() // the selected files are being converted (maybe removed)
		}
		m.state = stateProgress
		m.dir = msg.config.TargetDir
		m.progress = newProgressModel(m.ctx, m.styles, msg.config.TargetDir, msg.config)
		m.resizeChildren()
		return m, m.progress.Init()
	}

	// Dispatch to the active screen
	var cmd tea.Cmd
	switch m.state {
	case stateFileManager:
		if m.fileManager != nil {
			cmd = m.fileManager.Update(msg)
			m.dir = m.fileManager.dir
		}
	case stateDashboard:
		if m.dashboard != nil {
			cmd = m.dashboard.Update(msg)
		}
	case stateProgress:
		if m.progress != nil {
			cmd = m.progress.Update(msg)
		}
	}
	return m, cmd
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

	style := m.styles.Header.Copy()
	// Width includes padding but not the border.
	inner := max(0, m.width-docStyle.GetHorizontalFrameSize()-style.GetHorizontalBorderSize())
	// Keep the header on one line so the chrome height stays fixed.
	return style.Width(inner).Render(truncateText(titleText, inner-style.GetHorizontalPadding()))
}

func (m *UI) dirBadgeView() string {
	style := m.styles.DirBadge
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

// resizeChildren gives every screen the size of its content area.
func (m *UI) resizeChildren() {
	if m.width == 0 || m.height == 0 {
		return
	}
	w, h := m.contentSize()
	if m.fileManager != nil {
		m.fileManager.SetSize(w, h)
	}
	if m.dashboard != nil {
		m.dashboard.SetSize(w, h)
	}
	if m.progress != nil {
		m.progress.SetSize(w, h)
	}
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
		errorBox := m.styles.Dialog.Render(lipgloss.JoinVertical(lipgloss.Center,
			m.styles.DialogTitle.Render("FATAL ERROR"),
			"",
			lipgloss.NewStyle().MaxWidth(maxW).Render(m.err.Error()),
			"",
			m.styles.Dim.Render("Press any key to exit"),
		))
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, errorBox)
	}

	if baseView == "" {
		return docStyle.Render("Unknown state")
	}

	if m.showQuitConfirm {
		dialogBox := m.styles.Dialog.Render(lipgloss.JoinVertical(lipgloss.Center,
			m.styles.DialogTitle.Render("Quit Conversion?"),
			"",
			"Conversion is currently running.",
			"Are you sure you want to quit? (y/N)",
		))
		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, dialogBox)
	}

	return docStyle.Render(lipgloss.JoinVertical(lipgloss.Left, m.chromeView(), baseView))
}

var docStyle = lipgloss.NewStyle().Margin(1, 2, 0, 2)
