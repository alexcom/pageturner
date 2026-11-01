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
	"github.com/charmbracelet/bubbles/spinner"
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

	fmt.Fprint(w, prefix+truncateText(string(i), l.Width()-lipgloss.Width(prefix)))
}

type coverItem struct {
	kind  CoverKind
	label string
	file  string // file name inside the book directory for CoverFile
}

func (i coverItem) FilterValue() string { return i.label }

type coverDelegate struct{ m *DashboardModel }

func (d coverDelegate) Height() int                             { return 1 }
func (d coverDelegate) Spacing() int                            { return 0 }
func (d coverDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd { return nil }
func (d coverDelegate) Render(w io.Writer, l list.Model, index int, item list.Item) {
	i, ok := item.(coverItem)
	if !ok {
		return
	}

	prefix := "  ( ) "
	if index == l.Index() {
		prefix = "  (•) "
	}

	str := truncateText(fmt.Sprintf("%s%d. %s", prefix, index+1, i.label), l.Width())

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
	Theme       key.Binding
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
		key.WithHelp("enter", "next/start"),
	),
	Toggle: key.NewBinding(
		key.WithKeys(" "),
		key.WithHelp("space", "toggle"),
	),
	SwitchCover: key.NewBinding(
		key.WithKeys("c", "C"),
		key.WithHelp("c", "cover"),
	),
	Theme: key.NewBinding(
		key.WithKeys("t", "T"),
		key.WithHelp("t", "theme"),
	),
}

type dashboardKeyMap struct{}

func (k dashboardKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{dashKeys.NavUp, dashKeys.NavDown, dashKeys.Confirm, dashKeys.Toggle, dashKeys.SwitchCover, dashKeys.Theme, dashKeys.Back, dashKeys.Quit}
}
func (k dashboardKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.ShortHelp()}
}

var dashHelpKeys = dashboardKeyMap{}

type DashboardModel struct {
	ctx context.Context
	dir string

	// Directory scan runs in the background; the form is locked until it ends.
	scanning bool
	spinner  spinner.Model

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
	focusIndex  int
	showConfirm bool
	help        help.Model

	// Window size
	width  int
	height int
	layout dashLayout
}

func (m *DashboardModel) updateThemeStyles() {
	for i := range m.inputs {
		m.inputs[i].Cursor.Style = lipgloss.NewStyle().Foreground(primaryColor)
	}
	m.spinner.Style = lipgloss.NewStyle().Foreground(primaryColor)
	applyHelpStyles(&m.help.Styles)
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

func newDashboardModel(ctx context.Context, dir string, removeSource bool) *DashboardModel {
	m := &DashboardModel{
		ctx:          ctx,
		dir:          dir,
		removeSource: removeSource,
		inputs:       make([]textinput.Model, 5),
		help:         newHelpModel(),
		scanning:     true,
		spinner:      spinner.New(spinner.WithSpinner(spinner.Dot)),
	}
	m.spinner.Style = lipgloss.NewStyle().Foreground(primaryColor)

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
		t.Cursor.Style = lipgloss.NewStyle().Foreground(primaryColor)
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
	return !m.scanning && m.focusIndex >= dashInputArtist && m.focusIndex <= dashInputOutFilename
}

// dirScan is what the dashboard needs to know about a book directory. It is
// produced off the UI thread because it runs ffprobe on every mp3.
type dirScan struct {
	dir                             string
	files                           []string
	artist, album, title, performer string
	bitrate                         int
	hasEmbeddedCover                bool
	images                          []string
}

type msgDirScanned struct{ scan dirScan }

func scanDirectory(ctx context.Context, dir string) dirScan {
	s := dirScan{dir: dir, files: listFilesByExt(dir, ".mp3")}
	if len(s.files) > 0 {
		if bb, err := getMetaJsonBytes(ctx, dir, s.files[0]); err == nil {
			var fileMeta container
			if err := json.Unmarshal(bb.Bytes(), &fileMeta); err == nil {
				s.artist = fileMeta.Format.Tags.Artist
				s.album = fileMeta.Format.Tags.Album
				s.title = fileMeta.Format.Tags.Title
				s.performer = fileMeta.Format.Tags.Performer
			}
		}
		if br, err := detectBitrate(ctx, dir, nil); err == nil {
			s.bitrate = br
		}
		s.hasEmbeddedCover = hasCoverImage(ctx, dir, s.files[0])
	}
	dirContent, _ := os.ReadDir(dir)
	for _, file := range dirContent {
		if !file.IsDir() && isSupportedImageFormatFile(strings.ToLower(file.Name())) {
			s.images = append(s.images, file.Name())
		}
	}
	return s
}

func (m *DashboardModel) scanCmd() tea.Cmd {
	ctx, dir := m.ctx, m.dir
	return func() tea.Msg { return msgDirScanned{scan: scanDirectory(ctx, dir)} }
}

func (m *DashboardModel) applyScan(s dirScan) {
	m.scanning = false
	m.files = s.files
	m.bitrate = s.bitrate
	m.updateViewport()

	if len(m.files) > 0 {
		orDefault := func(v, def string) string {
			if v != "" {
				return v
			}
			return def
		}
		m.inputs[dashInputArtist].SetValue(orDefault(s.artist, "Unknown Artist"))
		m.inputs[dashInputAlbum].SetValue(orDefault(s.album, "Unknown Album"))
		m.inputs[dashInputTitle].SetValue(orDefault(s.title, "Unknown Title"))
		m.inputs[dashInputPerformer].SetValue(s.performer)

		var outName string
		if s.artist != "" && s.album != "" {
			outName = fmt.Sprintf("%s - %s", s.artist, s.album)
		} else if s.album != "" {
			outName = s.album
		} else {
			outName = filepath.Base(m.dir)
		}
		m.inputs[dashInputOutFilename].SetValue(strings.ReplaceAll(outName, string(filepath.Separator), "_"))
	}

	coverItems := []list.Item{
		coverItem{kind: CoverAuto, label: "Auto-detect"},
		coverItem{kind: CoverDefault, label: "Default Cover"},
	}
	if s.hasEmbeddedCover {
		coverItems = append(coverItems, coverItem{kind: CoverExtract, label: "Extract from MP3"})
	}
	for _, name := range s.images {
		coverItems = append(coverItems, coverItem{kind: CoverFile, label: name, file: name})
	}
	m.coverList.SetItems(coverItems)
	m.updateLayout(m.width, m.height)
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

	coverKind := CoverAuto
	var coverPath string
	if sel, ok := m.coverList.SelectedItem().(coverItem); ok {
		coverKind = sel.kind
		if sel.kind == CoverFile {
			coverPath = filepath.Join(m.dir, sel.file)
		}
	}

	return ConversionConfig{
		TargetDir:    m.dir,
		Artist:       m.inputs[dashInputArtist].Value(),
		Album:        m.inputs[dashInputAlbum].Value(),
		Title:        m.inputs[dashInputTitle].Value(),
		Performer:    m.inputs[dashInputPerformer].Value(),
		OutFilename:  outFn,
		CoverKind:    coverKind,
		CoverPath:    coverPath,
		RemoveSource: m.removeSource,
		BitRate:      m.bitrate,
	}
}

func (m *DashboardModel) Init() tea.Cmd {
	if m.scanning {
		return tea.Batch(textinput.Blink, m.spinner.Tick, m.scanCmd())
	}
	return textinput.Blink
}

// Layout constants. The dashboard receives the content area below the UI chrome.
const (
	layoutPaneGap          = 2 // columns between left and right panes
	layoutPaneHPadding     = 2 // horizontal padding inside each pane, per side
	layoutMinContentWidth  = 10
	layoutMetaPrefixWidth  = 14 // focus prefix (2) + label (11) + textinput cursor (1)
	layoutOutPrefixWidth   = 3  // focus prefix (2) + textinput cursor (1)
	layoutButtonHeight     = 5  // margin (1) + framed button (3) + margin (1)
	layoutCompactButtonH   = 1  // unframed one-line button
	layoutHelpHeight       = 2  // blank line + help bar
	layoutCompactHelpH     = 1  // help bar only
	layoutRightFixedRoomy  = 4  // title, blank, (list), blank, bitrate
	layoutRightFixedDense  = 2  // title, (list), bitrate
	layoutMinComponentRows = 1
)

// dashLayout is computed once per resize (or rescan) and used by View.
type dashLayout struct {
	paneWidth     int // full pane width including border
	contentWidth  int // width of text inside a pane
	paneHeight    int // full pane height including border
	density       int // 0 roomy, 1 no blank under titles/no vertical padding, 2 no blank lines
	compactButton bool
}

// leftFixedRows is the number of left-pane rows besides the cover list.
func leftFixedRows(density int) int {
	switch density {
	case 0:
		return 17 // 4 sections x (title + blank above/below) + 5 single rows
	case 1:
		return 13
	default:
		return 10
	}
}

func rightFixedRows(density int) int {
	if density == 2 {
		return layoutRightFixedDense
	}
	return layoutRightFixedRoomy
}

func paneVPadding(density int) int {
	if density == 0 {
		return 1
	}
	return 0
}

func (m *DashboardModel) updateLayout(w, h int) {
	m.width = w
	m.height = h
	m.help.Width = w

	paneWidth := (w - layoutPaneGap) / 2
	contentWidth := max(layoutMinContentWidth, paneWidth-2-2*layoutPaneHPadding)
	paneWidth = contentWidth + 2 + 2*layoutPaneHPadding

	coverItems := max(1, len(m.coverList.Items()))
	candidates := []struct {
		density int
		compact bool
	}{{0, false}, {1, false}, {1, true}, {2, true}}

	var chosen dashLayout
	coverRows := coverItems
	for i, c := range candidates {
		below := layoutButtonHeight + layoutHelpHeight
		if c.compact {
			below = layoutCompactButtonH + layoutCompactHelpH
		}
		paneHeight := h - below
		inner := paneHeight - 2 - 2*paneVPadding(c.density)
		fitsLeft := inner >= leftFixedRows(c.density)+coverItems
		fitsRight := inner >= rightFixedRows(c.density)+layoutMinComponentRows
		last := i == len(candidates)-1
		if (fitsLeft && fitsRight) || last {
			if !fitsLeft {
				// Terminal is very small: let the cover list scroll.
				coverRows = max(layoutMinComponentRows, inner-leftFixedRows(c.density))
				inner = leftFixedRows(c.density) + coverRows
				paneHeight = inner + 2 + 2*paneVPadding(c.density)
			}
			chosen = dashLayout{
				paneWidth:     paneWidth,
				contentWidth:  contentWidth,
				paneHeight:    paneHeight,
				density:       c.density,
				compactButton: c.compact,
			}
			break
		}
	}
	m.layout = chosen

	inner := chosen.paneHeight - 2 - 2*paneVPadding(chosen.density)
	m.fileList.SetWidth(contentWidth)
	m.fileList.SetHeight(max(layoutMinComponentRows, inner-rightFixedRows(chosen.density)))
	m.coverList.SetWidth(contentWidth)
	m.coverList.SetHeight(coverRows)

	// Text input widths, so text scrolls horizontally instead of wrapping inside the pane.
	for _, idx := range []int{dashInputArtist, dashInputAlbum, dashInputTitle, dashInputPerformer} {
		m.inputs[idx].Width = max(1, contentWidth-layoutMetaPrefixWidth)
		m.inputs[idx].SetCursor(m.inputs[idx].Position())
	}
	m.inputs[dashInputOutFilename].Width = max(1, contentWidth-layoutOutPrefixWidth)
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
	case msgDirScanned:
		if msg.scan.dir == m.dir {
			m.applyScan(msg.scan)
		}
		return m, nil
	case spinner.TickMsg:
		if !m.scanning {
			return m, nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case tea.KeyMsg:
		if m.scanning {
			if key.Matches(msg, dashKeys.Back) {
				return m, func() tea.Msg { return msgSwitchToFileManager{} }
			}
			return m, nil
		}
		if m.showConfirm {
			// Default is No, as the (y/N) prompt says: Enter must not overwrite.
			if msg.String() == "y" || msg.String() == "Y" {
				m.showConfirm = false
				return m, func() tea.Msg { return msgStartConversion{config: m.getConfig()} }
			} else if msg.String() == "n" || msg.String() == "N" || msg.String() == "esc" || msg.String() == "enter" {
				m.showConfirm = false
				return m, nil
			}
			return m, nil
		}

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

			if isUp {
				return m, m.moveFocus(-1)
			}
			return m, m.moveFocus(1)
		} else if key.Matches(msg, dashKeys.Confirm) {
			// Enter advances through the form; only the Start button starts the job.
			if m.focusIndex != dashStartButton {
				return m, m.moveFocus(1)
			}
			if len(m.files) > 0 {
				config := m.getConfig()
				if _, err := os.Stat(filepath.Join(m.dir, config.OutFilename)); err == nil {
					m.showConfirm = true
					return m, nil
				}
				return m, func() tea.Msg { return msgStartConversion{config: config} }
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

// moveFocus moves focus by delta (wrapping) and focuses/blurs text inputs.
func (m *DashboardModel) moveFocus(delta int) tea.Cmd {
	m.focusIndex = (m.focusIndex + delta + dashStartButton + 1) % (dashStartButton + 1)
	m.updateViewport()

	cmds := make([]tea.Cmd, len(m.inputs))
	for i := dashInputArtist; i <= dashInputOutFilename; i++ {
		if i == m.focusIndex {
			cmds[i] = m.inputs[i].Focus()
		} else {
			m.inputs[i].Blur()
		}
	}
	return tea.Batch(cmds...)
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
	if m.showConfirm {
		dialogBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(errorColor).
			Padding(1, 2).
			Render(lipgloss.JoinVertical(lipgloss.Center,
				lipgloss.NewStyle().Bold(true).Foreground(errorColor).Render("Output File Exists"),
				"",
				fmt.Sprintf("%q already exists in this directory.", m.getConfig().OutFilename),
				"Overwrite it? (y/N)",
			))

		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, dialogBox)
	}

	if m.scanning {
		status := m.spinner.View() + " Reading tags and bitrate…"
		return lipgloss.JoinVertical(lipgloss.Left,
			lipgloss.Place(m.width, max(1, m.height-1), lipgloss.Center, lipgloss.Center, status),
			helpStyle().Render("esc back • q quit"))
	}

	lay := m.layout
	lineStyle := lipgloss.NewStyle().Width(lay.contentWidth).MaxHeight(1)
	title := func(s string) string { return lipgloss.NewStyle().Bold(true).Render(s) }

	paneStyle := func(active bool) lipgloss.Style {
		border := secondaryColor
		if active {
			border = primaryColor
		}
		return lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(border).
			Padding(paneVPadding(lay.density), layoutPaneHPadding).
			Height(lay.paneHeight - 2) // Height includes padding, excludes border
	}

	// section adds a titled block, with spacing depending on layout density.
	var left []string
	section := func(name string, rows ...string) {
		if len(left) > 0 && lay.density < 2 {
			left = append(left, "")
		}
		left = append(left, title(name))
		if lay.density == 0 {
			left = append(left, "")
		}
		left = append(left, rows...)
	}
	focusPrefix := func(i int) string {
		if m.focusIndex == i {
			return lipgloss.NewStyle().Foreground(primaryColor).Render("> ")
		}
		return "  "
	}

	// Left: metadata, output, cover, source options
	labels := []string{"Artist:    ", "Album:     ", "Title:     ", "Performer: "}
	var meta []string
	for i := dashInputArtist; i <= dashInputPerformer; i++ {
		meta = append(meta, focusPrefix(i)+labels[i]+m.inputs[i].View())
	}
	section("METADATA", meta...)
	section("OUTPUT M4B FILE", focusPrefix(dashInputOutFilename)+m.inputs[dashInputOutFilename].View())
	section("COVER ART SOURCE", strings.Split(m.coverList.View(), "\n")...)
	box := "[ ]"
	if m.removeSource {
		box = "[x]"
	}
	section("SOURCE MP3 FILES", focusPrefix(dashRemoveSourceToggle)+box+" Remove")

	for i, l := range left {
		left[i] = lineStyle.Render(l)
	}
	leftActive := m.focusIndex >= dashInputArtist && m.focusIndex <= dashRemoveSourceToggle
	leftStr := paneStyle(leftActive).Render(lipgloss.JoinVertical(lipgloss.Left, left...))

	// Right: discovered files
	right := []string{title("DISCOVERED MP3 FILES")}
	if lay.density < 2 {
		right = append(right, "")
	}
	if len(m.files) == 0 {
		right = append(right, "  No MP3 files found.")
	} else {
		right = append(right, strings.Split(m.fileList.View(), "\n")...)
	}
	if lay.density < 2 {
		right = append(right, "")
	}
	bitrateStr := "Detected Bitrate: Unknown"
	if m.bitrate > 0 {
		bitrateStr = fmt.Sprintf("Detected Bitrate: %d kbps", m.bitrate)
	}
	right = append(right, bitrateStr)
	for i, l := range right {
		right[i] = lineStyle.Render(l)
	}
	rightStr := paneStyle(m.focusIndex == dashFileList).Render(lipgloss.JoinVertical(lipgloss.Left, right...))

	split := lipgloss.JoinHorizontal(lipgloss.Top, leftStr, strings.Repeat(" ", layoutPaneGap), rightStr)

	// Start button
	btnText := "START CONVERSION (Enter)"
	if m.focusIndex == dashStartButton {
		btnText = "> START CONVERSION (Enter) <"
	}
	innerBtnStyle := lipgloss.NewStyle().Padding(0, 2)
	if len(m.files) > 0 {
		innerBtnStyle = innerBtnStyle.Background(primaryColor).Foreground(dirBadgeFg).Bold(true)
	} else {
		innerBtnStyle = innerBtnStyle.Background(secondaryColor).Foreground(textColor)
	}
	startBtn := innerBtnStyle.Render(btnText)
	btnBlock := lipgloss.NewStyle().Width(lipgloss.Width(split)).Align(lipgloss.Center)
	if !lay.compactButton {
		frameStyle := lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(secondaryColor)
		if m.focusIndex == dashStartButton && len(m.files) > 0 {
			frameStyle = frameStyle.BorderForeground(primaryColor)
		}
		startBtn = frameStyle.Render(startBtn)
		btnBlock = btnBlock.Margin(1, 0)
	}

	parts := []string{split, btnBlock.Render(startBtn)}
	if !lay.compactButton {
		parts = append(parts, "")
	}
	parts = append(parts, m.help.View(dashHelpKeys))
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}
