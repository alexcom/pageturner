package main

import (
	"context"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/textinput"
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
		ctx:   ctx,
		state: initialState,
		dir:   initialDir,
		args:  args,
	}

	switch initialState {
	case stateFileManager:
		m.fileManager = newFileManagerModel(initialDir)
	case stateDashboard, stateProgress:
		m.dashboard = newDashboardModel(ctx, initialDir, nil, args.mode(), args.RemoveSource)
		if initialState == stateProgress {
			// No form to show (gogogo): scan synchronously so the config has tags and bitrate.
			m.dashboard.applyScan(scanDirectory(ctx, initialDir))
			m.progress = newProgressModel(ctx, initialDir, m.dashboard.getConfig())
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
			return m, m.toggleTheme()
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
		m.dashboard = newDashboardModel(m.ctx, msg.dir, msg.files, msg.mode, m.args.RemoveSource)
		m.resizeChildren()
		return m, m.dashboard.Init()

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
		m.resizeChildren()
		return m, m.fileManager.Init()

	case msgStartConversion:
		if m.fileManager != nil {
			m.fileManager.clearSelection() // the selected files are being converted (maybe removed)
		}
		m.state = stateProgress
		m.dir = msg.config.TargetDir
		m.progress = newProgressModel(m.ctx, msg.config.TargetDir, msg.config)
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

	style := lipgloss.NewStyle().
		Bold(true).
		Foreground(primaryColor).
		Padding(0, 1).
		Border(lipgloss.RoundedBorder()).
		BorderForeground(primaryColor)
	// Width includes padding but not the border.
	inner := max(0, m.width-docStyle.GetHorizontalFrameSize()-style.GetHorizontalBorderSize())
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

	return docStyle.Render(lipgloss.JoinVertical(lipgloss.Left, m.chromeView(), baseView))
}

var docStyle = lipgloss.NewStyle().Margin(1, 2, 0, 2)

type ThemeKind int

const (
	ThemeDark ThemeKind = iota
	ThemeLight
)

type Theme struct {
	Primary    lipgloss.Color
	Secondary  lipgloss.Color
	Text       lipgloss.Color
	Subtext    lipgloss.Color
	Error      lipgloss.Color
	Success    lipgloss.Color
	Warning    lipgloss.Color
	DirBadgeBg lipgloss.Color
	DirBadgeFg lipgloss.Color
	OnPrimary  lipgloss.Color // text on a Primary background (buttons)
	GradientA  string         // progress bar gradient
	GradientB  string
}

var (
	currentThemeKind = ThemeDark

	DarkTheme = Theme{
		Primary:    lipgloss.Color("#00ADB5"),
		Secondary:  lipgloss.Color("#393E46"),
		Text:       lipgloss.Color("#EEEEEE"),
		Subtext:    lipgloss.Color("#AAAAAA"),
		Error:      lipgloss.Color("#FF2E63"),
		Success:    lipgloss.Color("#00D846"),
		Warning:    lipgloss.Color("#FFB800"),
		DirBadgeBg: lipgloss.Color("#00ADB5"),
		DirBadgeFg: lipgloss.Color("#1A1A1A"),
		OnPrimary:  lipgloss.Color("#1A1A1A"),
		GradientA:  "#5A56E0", // bubbles' default gradient
		GradientB:  "#EE6FF8",
	}

	LightTheme = Theme{
		Primary:    lipgloss.Color("#005F73"),
		Secondary:  lipgloss.Color("#CBD5E1"),
		Text:       lipgloss.Color("#0F172A"),
		Subtext:    lipgloss.Color("#64748B"),
		Error:      lipgloss.Color("#DC2626"),
		Success:    lipgloss.Color("#059669"),
		Warning:    lipgloss.Color("#D97706"),
		DirBadgeBg: lipgloss.Color("#005F73"),
		DirBadgeFg: lipgloss.Color("#FFFFFF"),
		OnPrimary:  lipgloss.Color("#FFFFFF"),
		GradientA:  "#4338CA",
		GradientB:  "#DB2777",
	}

	currentTheme   Theme
	primaryColor   lipgloss.Color
	secondaryColor lipgloss.Color
	textColor      lipgloss.Color
	subtextColor   lipgloss.Color
	errorColor     lipgloss.Color
	successColor   lipgloss.Color
	warningColor   lipgloss.Color
	dirBadgeBg     lipgloss.Color
	dirBadgeFg     lipgloss.Color
	onPrimaryColor lipgloss.Color
)

func applyTheme(kind ThemeKind) {
	currentThemeKind = kind
	t := DarkTheme
	if kind == ThemeLight {
		t = LightTheme
	}
	currentTheme = t
	primaryColor = t.Primary
	secondaryColor = t.Secondary
	textColor = t.Text
	subtextColor = t.Subtext
	errorColor = t.Error
	successColor = t.Success
	warningColor = t.Warning
	dirBadgeBg = t.DirBadgeBg
	dirBadgeFg = t.DirBadgeFg
	onPrimaryColor = t.OnPrimary
}

func init() {
	applyTheme(ThemeDark)
}

// detectTheme picks the initial theme from the terminal background. It
// queries the terminal, so it must run before the Bubble Tea program starts.
func detectTheme() {
	if !lipgloss.HasDarkBackground() {
		applyTheme(ThemeLight)
	}
}

func (m *UI) toggleTheme() tea.Cmd {
	if currentThemeKind == ThemeDark {
		applyTheme(ThemeLight)
	} else {
		applyTheme(ThemeDark)
	}
	if m.fileManager != nil {
		m.fileManager.updateThemeStyles()
	}
	if m.dashboard != nil {
		m.dashboard.updateThemeStyles()
	}
	if m.progress != nil {
		return m.progress.updateThemeStyles()
	}
	return nil
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
	s.Ellipsis = lipgloss.NewStyle().Foreground(secondaryColor)
}

// applyInputStyles themes a textinput; its default placeholder colour is a
// fixed grey that ignores the theme.
func applyInputStyles(t *textinput.Model) {
	t.Cursor.Style = lipgloss.NewStyle().Foreground(primaryColor)
	t.PromptStyle = lipgloss.NewStyle().Foreground(primaryColor)
	t.PlaceholderStyle = lipgloss.NewStyle().Foreground(subtextColor)
}

func newProgressBar() progress.Model {
	// The percentage is part of the status label, so the bar doesn't print its own.
	return progress.New(
		progress.WithGradient(currentTheme.GradientA, currentTheme.GradientB),
		progress.WithoutPercentage(),
	)
}
