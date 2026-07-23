package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type simpleItem string

func (i simpleItem) Title() string       { return string(i) }
func (i simpleItem) Description() string { return "" }
func (i simpleItem) FilterValue() string { return string(i) }

type fileDelegate struct{ m *DashboardModel }

func (d fileDelegate) Height() int                             { return 1 }
func (d fileDelegate) Spacing() int                            { return 0 }
func (d fileDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d fileDelegate) Render(w io.Writer, l list.Model, index int, item list.Item) {
	i, ok := item.(simpleItem)
	if !ok {
		return
	}

	prefix := "  "
	if index == l.Index() {
		if d.m.focusIndex == dashFileList {
			prefix = lipgloss.NewStyle().Foreground(primaryColor).Render("┃ ")
		} else {
			prefix = "┃ "
		}
	}

	fname := string(i)
	maxLen := l.Width() - 4
	if maxLen < 10 {
		maxLen = 10
	}
	if len([]rune(fname)) > maxLen {
		fname = string([]rune(fname)[:maxLen-3]) + "..."
	}

	fmt.Fprint(w, prefix+fname)
}

type coverDelegate struct{ m *DashboardModel }

func (d coverDelegate) Height() int                             { return 1 }
func (d coverDelegate) Spacing() int                            { return 0 }
func (d coverDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d coverDelegate) Render(w io.Writer, l list.Model, index int, item list.Item) {
	i, ok := item.(simpleItem)
	if !ok {
		return
	}

	prefix := "  ( ) "
	if index == l.Index() {
		prefix = "  (•) "
	}

	str := fmt.Sprintf("%s%d. %s", prefix, index+1, i)

	if d.m.focusIndex == dashCoverSelection {
		if index == l.Index() {
			str = lipgloss.NewStyle().Foreground(primaryColor).Render(str)
		} else {
			str = lipgloss.NewStyle().Foreground(subtextColor).Render(str)
		}
	} else {
		if index == l.Index() {
			str = lipgloss.NewStyle().Render(str)
		} else {
			str = lipgloss.NewStyle().Foreground(subtextColor).Render(str)
		}
	}
	fmt.Fprint(w, str)
}

var dashKeys = struct {
	Back        key.Binding
	Quit        key.Binding
	NavUp       key.Binding
	NavDown     key.Binding
	Confirm     key.Binding
	Toggle      key.Binding
	SwitchCover key.Binding
}{
	Back: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "back"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q"),
		key.WithHelp("q", "quit"),
	),
	NavUp: key.NewBinding(
		key.WithKeys("up", "shift+tab"),
		key.WithHelp("↑/shift+tab", "up"),
	),
	NavDown: key.NewBinding(
		key.WithKeys("down", "tab"),
		key.WithHelp("↓/tab", "down"),
	),
	Confirm: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "confirm"),
	),
	Toggle: key.NewBinding(
		key.WithKeys(" "),
		key.WithHelp("space", "toggle"),
	),
	SwitchCover: key.NewBinding(
		key.WithKeys("c", "C"),
		key.WithHelp("c", "cover"),
	),
}

type dashboardKeyMap struct{}

func (k dashboardKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{dashKeys.NavUp, dashKeys.NavDown, dashKeys.Confirm, dashKeys.Toggle, dashKeys.SwitchCover, dashKeys.Back, dashKeys.Quit}
}
func (k dashboardKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.ShortHelp()}
}

var dashHelpKeys = dashboardKeyMap{}

type DashboardModel struct {
	dir string

	// Left column
	files        []string
	fileList     list.Model
	bitrate      int
	removeSource bool

	// Right column - text inputs
	inputs []textinput.Model

	// Right column - cover selection
	coverList list.Model

	// UI state
	focusIndex int
	help       help.Model

	// Window size
	width  int
	height int
}

const (
	dashInputArtist = iota
	dashInputAlbum
	dashInputTitle
	dashInputPerformer
	dashInputOutFilename
	dashCoverSelection
	dashRemoveSourceToggle
	dashFileList
	dashStartButton
)

func newDashboardModel(ctx context.Context, dir string) *DashboardModel {
	m := &DashboardModel{
		dir:    dir,
		inputs: make([]textinput.Model, 5),
		help:   newHelpModel(),
	}

	m.fileList = list.New([]list.Item{}, fileDelegate{m: m}, 41, 10)
	m.fileList.SetShowTitle(false)
	m.fileList.SetShowStatusBar(false)
	m.fileList.SetShowFilter(false)
	m.fileList.SetShowHelp(false)
	m.fileList.SetShowPagination(false)

	m.coverList = list.New([]list.Item{}, coverDelegate{m: m}, 41, 8)
	m.coverList.SetShowTitle(false)
	m.coverList.SetShowStatusBar(false)
	m.coverList.SetShowFilter(false)
	m.coverList.SetShowHelp(false)
	m.coverList.SetShowPagination(false)

	for i := range m.inputs {
		t := textinput.New()
		t.Prompt = ""
		t.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
		t.CharLimit = 128

		switch i {
		case dashInputArtist:
			t.Placeholder = "Artist"
		case dashInputAlbum:
			t.Placeholder = "Album"
		case dashInputTitle:
			t.Placeholder = "Title"
		case dashInputPerformer:
			t.Placeholder = "Performer"
		case dashInputOutFilename:
			t.Placeholder = "Output Filename"
		}
		m.inputs[i] = t
	}

	m.focusIndex = dashInputArtist
	m.inputs[dashInputArtist].Focus()
	m.updateLayout(80, 24)
	m.scanDirectory(ctx)
	return m
}

func (m *DashboardModel) updateViewport() {
	var items []list.Item
	for _, f := range m.files {
		items = append(items, simpleItem(f))
	}
	m.fileList.SetItems(items)
}

func (m *DashboardModel) IsEditingText() bool {
	return m.focusIndex >= dashInputArtist && m.focusIndex <= dashInputOutFilename
}

func (m *DashboardModel) scanDirectory(ctx context.Context) {
	m.files = listFilesByExt(m.dir, ".mp3")
	m.updateViewport()

	if len(m.files) > 0 {
		artist, album, title, performer := "", "", "", ""
		bb, err := getMetaJsonBytes(ctx, m.dir, m.files[0])
		if err == nil {
			var fileMeta container
			if err := json.Unmarshal(bb.Bytes(), &fileMeta); err == nil {
				artist = fileMeta.Format.Tags.Artist
				album = fileMeta.Format.Tags.Album
				title = fileMeta.Format.Tags.Title
				performer = fileMeta.Format.Tags.Performer
			}
		}

		if br, err := detectBitrate(ctx, m.dir, nil); err == nil {
			m.bitrate = br
		}

		if artist != "" {
			m.inputs[dashInputArtist].SetValue(artist)
		} else {
			m.inputs[dashInputArtist].SetValue("Unknown Artist")
		}

		if album != "" {
			m.inputs[dashInputAlbum].SetValue(album)
		} else {
			m.inputs[dashInputAlbum].SetValue("Unknown Album")
		}

		if title != "" {
			m.inputs[dashInputTitle].SetValue(title)
		} else {
			m.inputs[dashInputTitle].SetValue("Unknown Title")
		}

		if performer != "" {
			m.inputs[dashInputPerformer].SetValue(performer)
		} else {
			m.inputs[dashInputPerformer].SetValue("")
		}

		outName := ""
		if artist != "" && album != "" {
			outName = fmt.Sprintf("%s - %s", artist, album)
		} else if album != "" {
			outName = fmt.Sprintf("%s", album)
		} else {
			outName = filepath.Base(m.dir)
		}
		m.inputs[dashInputOutFilename].SetValue(strings.ReplaceAll(outName, string(filepath.Separator), "_"))
	}

	var coverItems []list.Item
	coverItems = append(coverItems, simpleItem("Default Cover"))
	// Extracted cover if possible
	if len(m.files) > 0 && hasCoverImage(ctx, m.dir, m.files[0]) {
		coverItems = append(coverItems, simpleItem("Extract from MP3"))
	}

	// Add other jpgs in the directory
	dirContent, _ := os.ReadDir(m.dir)
	for _, file := range dirContent {
		if !file.IsDir() && isSupportedImageFormatFile(strings.ToLower(file.Name())) {
			coverItems = append(coverItems, simpleItem(file.Name()))
		}
	}
	m.coverList.SetItems(coverItems)
	coverHeight := len(coverItems)
	if coverHeight < 1 {
		coverHeight = 1
	}
	m.coverList.SetHeight(coverHeight)
}

func hasCoverImage(ctx context.Context, dir, filename string) bool {
	cmd := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=codec_type", "-of", "default=nw=1:nk=1", filepath.Join(dir, filename))
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "video"
}

func (m *DashboardModel) getConfig() ConversionConfig {
	outFn := m.inputs[dashInputOutFilename].Value()
	if outFn == "" {
		outFn = "book"
	}
	outFn = strings.TrimSuffix(outFn, ".m4b")
	outFn += ".m4b"

	var coverSrc string
	if sel := m.coverList.SelectedItem(); sel != nil {
		coverSrc = string(sel.(simpleItem))
	} else {
		coverSrc = "Default Cover"
	}

	var coverPath string
	if coverSrc != "Default Cover" && coverSrc != "Extract from MP3" {
		coverPath = filepath.Join(m.dir, coverSrc)
	}

	return ConversionConfig{
		TargetDir:    m.dir,
		Artist:       m.inputs[dashInputArtist].Value(),
		Album:        m.inputs[dashInputAlbum].Value(),
		Title:        m.inputs[dashInputTitle].Value(),
		Performer:    m.inputs[dashInputPerformer].Value(),
		OutFilename:  outFn,
		CoverSource:  m.coverList.Index(),
		CoverPath:    coverPath,
		RemoveSource: m.removeSource,
		BitRate:      m.bitrate,
	}
}

func (m *DashboardModel) Init() tea.Cmd {
	return textinput.Blink
}

// Layout calculation constants
const (
	layoutDocHorizontalMargin       = 4 // 2 left + 2 right from docStyle
	layoutPaneGap                   = 2 // gap between left and right panes
	layoutMinPaneWidth              = 30
	layoutVerticalNonPaneSpace      = 14 // header stack (7), margins (2), start btn (3), help (1-2)
	layoutMinPaneHeight             = 15
	layoutPaneBorderWidth           = 6 // borders (2) + horizontal padding (4)
	layoutPaneBorderHeight          = 4 // borders (2) + vertical padding (2)
	layoutMinContentWidth           = 10
	layoutLeftPaneNonViewportHeight = 11 // borders (2) + padding (2) + titles/bitrate (4) + offset (3)
	layoutMinComponentHeight        = 3
	layoutMetaPrefixLabelWidth      = 14 // prefix (2) + label (11) + textinput cursor margin (1)
	layoutOutPrefixWidth            = 3  // prefix (2) + textinput cursor margin (1)
)

func (m *DashboardModel) updateLayout(w, h int) {
	m.width = w
	m.height = h
	m.help.Width = w

	paneWidth := (w - layoutDocHorizontalMargin - layoutPaneGap) / 2
	if paneWidth < layoutMinPaneWidth {
		paneWidth = layoutMinPaneWidth
	}

	paneHeight := h - layoutVerticalNonPaneSpace
	if paneHeight < layoutMinPaneHeight {
		paneHeight = layoutMinPaneHeight
	}

	contentWidth := paneWidth - layoutPaneBorderWidth
	if contentWidth < layoutMinContentWidth {
		contentWidth = layoutMinContentWidth
	}

	m.fileList.SetWidth(contentWidth)
	vpHeight := paneHeight - layoutLeftPaneNonViewportHeight
	if vpHeight < layoutMinComponentHeight {
		vpHeight = layoutMinComponentHeight
	}
	m.fileList.SetHeight(vpHeight)

	m.coverList.SetWidth(contentWidth)

	coverHeight := len(m.coverList.Items())
	if coverHeight < 1 {
		coverHeight = 1
	}
	m.coverList.SetHeight(coverHeight)

	// Update text input widths so text scrolls horizontally without wrapping lines inside the border pane.
	wArtistAlbumTitle := contentWidth - layoutMetaPrefixLabelWidth
	if wArtistAlbumTitle < 1 {
		wArtistAlbumTitle = 1
	}
	for _, idx := range []int{dashInputArtist, dashInputAlbum, dashInputTitle, dashInputPerformer} {
		m.inputs[idx].Width = wArtistAlbumTitle
		m.inputs[idx].SetCursor(m.inputs[idx].Position())
	}

	wOutFilename := contentWidth - layoutOutPrefixWidth
	if wOutFilename < 1 {
		wOutFilename = 1
	}
	m.inputs[dashInputOutFilename].Width = wOutFilename
	m.inputs[dashInputOutFilename].SetCursor(m.inputs[dashInputOutFilename].Position())
}

func (m *DashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.updateLayout(msg.Width, msg.Height)
		m.updateViewport() // Make sure viewport text gets re-truncated and wrapped
	case tea.MouseMsg:
		if m.focusIndex == dashFileList {
			var cmd tea.Cmd
			m.fileList, cmd = m.fileList.Update(msg)
			return m, cmd
		} else if m.focusIndex == dashCoverSelection {
			var cmd tea.Cmd
			m.coverList, cmd = m.coverList.Update(msg)
			return m, cmd
		}
	case tea.KeyMsg:
		if key.Matches(msg, dashKeys.Back) {
			return m, func() tea.Msg { return msgSwitchToFileManager{} }
		} else if key.Matches(msg, dashKeys.NavUp, dashKeys.NavDown) {
			isUp := key.Matches(msg, dashKeys.NavUp)

			if m.focusIndex == dashFileList && (msg.String() == "up" || msg.String() == "down") {
				var cmd tea.Cmd
				m.fileList, cmd = m.fileList.Update(msg)
				return m, cmd
			}
			if m.focusIndex == dashCoverSelection && (msg.String() == "up" || msg.String() == "down") {
				var cmd tea.Cmd
				m.coverList, cmd = m.coverList.Update(msg)
				return m, cmd
			}

			// Adjust focus
			if isUp {
				m.focusIndex--
			} else {
				m.focusIndex++
			}

			if m.focusIndex > dashStartButton {
				m.focusIndex = 0
			} else if m.focusIndex < 0 {
				m.focusIndex = dashStartButton
			}

			m.updateViewport()

			cmds := make([]tea.Cmd, len(m.inputs))
			for i := dashInputArtist; i <= dashInputOutFilename; i++ {
				if i == m.focusIndex {
					cmds[i] = m.inputs[i].Focus()
				} else {
					m.inputs[i].Blur()
				}
			}
			return m, tea.Batch(cmds...)
		} else if key.Matches(msg, dashKeys.Confirm) {
			if len(m.files) > 0 {
				return m, func() tea.Msg { return msgStartConversion{config: m.getConfig()} }
			}
			return m, nil
		} else if key.Matches(msg, dashKeys.Toggle) {
			if m.focusIndex == dashRemoveSourceToggle {
				m.removeSource = !m.removeSource
			}
		} else if key.Matches(msg, dashKeys.SwitchCover) {
			if !(m.focusIndex >= dashInputArtist && m.focusIndex <= dashInputOutFilename) {
				idx := m.coverList.Index() + 1
				if idx >= len(m.coverList.Items()) {
					idx = 0
				}
				m.coverList.Select(idx)
				return m, nil
			}
		}
	}

	cmd := m.updateInputs(msg)
	return m, cmd
}

func (m *DashboardModel) updateInputs(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	for i := range m.inputs {
		if m.focusIndex == i {
			var cmd tea.Cmd
			m.inputs[i], cmd = m.inputs[i].Update(msg)
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

func (m *DashboardModel) View() string {
	paneWidth := (m.width - layoutDocHorizontalMargin - layoutPaneGap) / 2
	if paneWidth < layoutMinPaneWidth {
		paneWidth = layoutMinPaneWidth
	}
	paneHeight := m.height - layoutVerticalNonPaneSpace
	if paneHeight < layoutMinPaneHeight {
		paneHeight = layoutMinPaneHeight
	}

	contentWidth := paneWidth - layoutPaneBorderWidth
	if contentWidth < layoutMinContentWidth {
		contentWidth = layoutMinContentWidth
	}
	lineStyle := lipgloss.NewStyle().Width(contentWidth).MaxHeight(1)

	activeBorder := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(primaryColor).
		Padding(1, 2).
		Height(paneHeight - layoutPaneBorderHeight)

	inactiveBorder := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(secondaryColor).
		Padding(1, 2).
		Height(paneHeight - layoutPaneBorderHeight)

	// Left: Metadata & Output M4B File
	var left []string
	left = append(left, lipgloss.NewStyle().Bold(true).Render("METADATA"), "")
	for i := dashInputArtist; i <= dashInputPerformer; i++ {
		prefix := "  "
		if m.focusIndex == i {
			prefix = lipgloss.NewStyle().Foreground(primaryColor).Render("> ")
		}
		label := []string{"Artist:    ", "Album:     ", "Title:     ", "Performer: "}[i]

		inputView := m.inputs[i].View()
		left = append(left, prefix+label+inputView)
	}

	left = append(left, "", lipgloss.NewStyle().Bold(true).Render("OUTPUT M4B FILE"), "")
	prefixOut := "  "
	if m.focusIndex == dashInputOutFilename {
		prefixOut = lipgloss.NewStyle().Foreground(primaryColor).Render("> ")
	}
	left = append(left, prefixOut+m.inputs[dashInputOutFilename].View())

	left = append(left, "", lipgloss.NewStyle().Bold(true).Render("COVER ART SOURCE"), "")

	left = append(left, m.coverList.View())

	left = append(left, "", lipgloss.NewStyle().Bold(true).Render("SOURCE MP3 FILES"), "")

	prefix := "  "
	if m.focusIndex == dashRemoveSourceToggle {
		prefix = lipgloss.NewStyle().Foreground(primaryColor).Render("> ")
	}
	box := "[ ]"
	if m.removeSource {
		box = "[x]"
	}
	left = append(left, prefix+box+" Remove")

	var formattedLeft []string
	for _, l := range left {
		for _, line := range strings.Split(l, "\n") {
			formattedLeft = append(formattedLeft, lineStyle.Render(line))
		}
	}
	leftStr := lipgloss.JoinVertical(lipgloss.Left, formattedLeft...)
	if m.focusIndex >= dashInputArtist && m.focusIndex <= dashRemoveSourceToggle {
		leftStr = activeBorder.Render(leftStr)
	} else {
		leftStr = inactiveBorder.Render(leftStr)
	}

	// Right: Discovered files
	var right []string
	right = append(right, lipgloss.NewStyle().Bold(true).Render("DISCOVERED MP3 FILES"), "")

	if len(m.files) == 0 {
		right = append(right, "  No MP3 files found.")
	} else {
		for _, line := range strings.Split(m.fileList.View(), "\n") {
			right = append(right, line)
		}
	}
	right = append(right, "")

	bitrateStr := "Detected Bitrate: Unknown"
	if m.bitrate > 0 {
		bitrateStr = fmt.Sprintf("Detected Bitrate: %d kbps", m.bitrate)
	}
	right = append(right, bitrateStr)

	var formattedRight []string
	for _, l := range right {
		formattedRight = append(formattedRight, lineStyle.Render(l))
	}
	rightStr := lipgloss.JoinVertical(lipgloss.Left, formattedRight...)
	if m.focusIndex == dashFileList {
		rightStr = activeBorder.Render(rightStr)
	} else {
		rightStr = inactiveBorder.Render(rightStr)
	}

	split := lipgloss.JoinHorizontal(lipgloss.Top, leftStr, lipgloss.NewStyle().Width(2).Render(""), rightStr)

	startBtnStyle := lipgloss.NewStyle().
		Padding(0, 4).
		Margin(1, 0)

	if len(m.files) > 0 {
		startBtnStyle = startBtnStyle.
			Background(primaryColor).
			Foreground(lipgloss.Color("0")).
			Bold(true)
	} else {
		startBtnStyle = startBtnStyle.
			Background(secondaryColor).
			Foreground(textColor)
	}

	btnText := "  START CONVERSION (Enter)  "
	if m.focusIndex == dashStartButton {
		btnText = "> START CONVERSION (Enter) <"
	}
	startBtn := startBtnStyle.Render(btnText)

	return lipgloss.JoinVertical(lipgloss.Left,
		split,
		startBtn,
		"",
		m.help.View(dashHelpKeys),
	)
}
