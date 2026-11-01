package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type coverItem struct {
	kind  CoverKind
	label string
	file  string // file name inside the book directory for CoverFile
}

var dashKeys = struct {
	Next        key.Binding
	Prev        key.Binding
	ListUp      key.Binding
	ListDown    key.Binding
	Confirm     key.Binding
	Toggle      key.Binding
	SwitchCover key.Binding
	Back        key.Binding
}{
	// Up/down move focus, except in the cover and file lists where they move
	// inside the list (ListUp/ListDown are checked first).
	Next:        key.NewBinding(key.WithKeys("tab", "down"), key.WithHelp("tab/shift+tab", "move")),
	Prev:        key.NewBinding(key.WithKeys("shift+tab", "up")),
	ListUp:      key.NewBinding(key.WithKeys("up"), key.WithHelp("↑/↓", "choose")),
	ListDown:    key.NewBinding(key.WithKeys("down")),
	Confirm:     key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "next")),
	Toggle:      key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "toggle")),
	SwitchCover: key.NewBinding(key.WithKeys("c", "C"), key.WithHelp("c", "cover")),
	Back:        key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
}

type DashboardModel struct {
	ctx context.Context
	dir string

	// Directory scan runs in the background; the form is locked until it ends.
	scanning bool
	spinner  spinner.Model

	// Right pane: discovered files (read-only, scrollable)
	files      []string
	fileScroll scroller
	bitrate    int

	// Left pane
	inputs       []textinput.Model
	covers       []coverItem
	coverScroll  scroller // cursor is the selected cover
	removeSource bool

	// UI state
	focusIndex  int
	showConfirm bool
	help        help.Model

	width  int
	height int
	layout dashLayout
}

func (m *DashboardModel) updateThemeStyles() {
	for i := range m.inputs {
		applyInputStyles(&m.inputs[i])
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

	placeholders := []string{"Artist", "Album", "Title", "Performer", "Output Filename"}
	for i := range m.inputs {
		t := textinput.New()
		t.Prompt = ""
		t.CharLimit = 128
		t.Placeholder = placeholders[i]
		applyInputStyles(&t)
		m.inputs[i] = t
	}

	m.focusIndex = dashInputArtist
	m.inputs[dashInputArtist].Focus()
	m.updateLayout(80, 24)
	return m
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
	m.fileScroll.SetCount(len(m.files))

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

	m.covers = []coverItem{
		{kind: CoverAuto, label: "Auto-detect"},
		{kind: CoverDefault, label: "Default Cover"},
	}
	if s.hasEmbeddedCover {
		m.covers = append(m.covers, coverItem{kind: CoverExtract, label: "Extract from MP3"})
	}
	for _, name := range s.images {
		m.covers = append(m.covers, coverItem{kind: CoverFile, label: name, file: name})
	}
	m.coverScroll.SetCount(len(m.covers))
	m.coverScroll.Top()
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
	if c := m.coverScroll.Cursor(); c < len(m.covers) {
		sel := m.covers[c]
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

func (m *DashboardModel) SetSize(w, h int) { m.updateLayout(w, h) }

func (m *DashboardModel) updateLayout(w, h int) {
	m.width = w
	m.height = h
	m.help.Width = w

	paneWidth := (w - layoutPaneGap) / 2
	contentWidth := max(layoutMinContentWidth, paneWidth-2-2*layoutPaneHPadding)
	paneWidth = contentWidth + 2 + 2*layoutPaneHPadding

	coverItems := max(1, len(m.covers))
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
	m.fileScroll.SetRows(max(layoutMinComponentRows, inner-rightFixedRows(chosen.density)))
	m.coverScroll.SetRows(coverRows)

	// Text input widths, so text scrolls horizontally instead of wrapping inside the pane.
	for _, idx := range []int{dashInputArtist, dashInputAlbum, dashInputTitle, dashInputPerformer} {
		m.inputs[idx].Width = max(1, contentWidth-layoutMetaPrefixWidth)
		m.inputs[idx].SetCursor(m.inputs[idx].Position())
	}
	m.inputs[dashInputOutFilename].Width = max(1, contentWidth-layoutOutPrefixWidth)
	m.inputs[dashInputOutFilename].SetCursor(m.inputs[dashInputOutFilename].Position())
}

func (m *DashboardModel) start() tea.Cmd {
	config := m.getConfig()
	return func() tea.Msg { return msgStartConversion{config: config} }
}

func (m *DashboardModel) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case msgDirScanned:
		if msg.scan.dir == m.dir {
			m.applyScan(msg.scan)
		}
		return nil
	case spinner.TickMsg:
		if !m.scanning {
			return nil
		}
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return cmd
	case tea.KeyMsg:
		if m.scanning {
			if key.Matches(msg, dashKeys.Back) {
				return func() tea.Msg { return msgSwitchToFileManager{} }
			}
			return nil
		}
		if m.showConfirm {
			switch {
			case key.Matches(msg, confirmKeys.Yes):
				m.showConfirm = false
				return m.start()
			case key.Matches(msg, confirmKeys.No):
				m.showConfirm = false
			}
			return nil
		}

		inCovers := m.focusIndex == dashCoverSelection
		inFiles := m.focusIndex == dashFileList
		switch {
		case key.Matches(msg, dashKeys.Back):
			return func() tea.Msg { return msgSwitchToFileManager{} }
		case key.Matches(msg, dashKeys.ListUp) && (inCovers || inFiles):
			m.listScroll().Move(-1)
			return nil
		case key.Matches(msg, dashKeys.ListDown) && (inCovers || inFiles):
			m.listScroll().Move(1)
			return nil
		case key.Matches(msg, dashKeys.Prev):
			return m.moveFocus(-1)
		case key.Matches(msg, dashKeys.Next):
			return m.moveFocus(1)
		case key.Matches(msg, dashKeys.Confirm):
			// Enter advances through the form; only the Start button starts the job.
			if m.focusIndex != dashStartButton {
				return m.moveFocus(1)
			}
			if len(m.files) == 0 {
				return nil
			}
			if _, err := os.Stat(filepath.Join(m.dir, m.getConfig().OutFilename)); err == nil {
				m.showConfirm = true
				return nil
			}
			return m.start()
		case key.Matches(msg, dashKeys.Toggle) && m.focusIndex == dashRemoveSourceToggle:
			m.removeSource = !m.removeSource
			return nil
		case key.Matches(msg, dashKeys.SwitchCover) && !m.IsEditingText():
			if len(m.covers) > 0 {
				m.coverScroll.Select((m.coverScroll.Cursor() + 1) % len(m.covers))
			}
			return nil
		}
	}

	return m.updateInputs(msg)
}

// listScroll returns the scroller of the focused list.
func (m *DashboardModel) listScroll() *scroller {
	if m.focusIndex == dashCoverSelection {
		return &m.coverScroll
	}
	return &m.fileScroll
}

// moveFocus moves focus by delta (wrapping) and focuses/blurs text inputs.
func (m *DashboardModel) moveFocus(delta int) tea.Cmd {
	m.focusIndex = (m.focusIndex + delta + dashStartButton + 1) % (dashStartButton + 1)

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
	if !m.IsEditingText() {
		return nil
	}
	var cmd tea.Cmd
	m.inputs[m.focusIndex], cmd = m.inputs[m.focusIndex].Update(msg)
	return cmd
}

// ShortHelp implements help.KeyMap with the keys that work at the current
// focus. Essentials come first; help truncates from the end on narrow terminals.
func (m *DashboardModel) ShortHelp() []key.Binding {
	if m.scanning {
		return append([]key.Binding{dashKeys.Back}, globalHelp(false)...)
	}
	editing := m.IsEditingText()
	var b []key.Binding
	switch m.focusIndex {
	case dashCoverSelection:
		b = append(b, dashKeys.ListUp)
	case dashFileList:
		scroll := dashKeys.ListUp
		scroll.SetHelp("↑/↓", "scroll")
		b = append(b, scroll)
	case dashRemoveSourceToggle:
		b = append(b, dashKeys.Toggle)
	}
	confirm := dashKeys.Confirm
	if m.focusIndex == dashStartButton {
		confirm.SetHelp("enter", "start")
		confirm.SetEnabled(len(m.files) > 0)
	}
	b = append(b, confirm, dashKeys.Next, dashKeys.Back)
	global := globalHelp(editing)
	b = append(b, global[0], global[1]) // quit / ctrl+c quit
	cover := dashKeys.SwitchCover
	cover.SetEnabled(!editing && m.focusIndex != dashCoverSelection)
	return append(b, cover, global[2]) // theme last
}

func (m *DashboardModel) FullHelp() [][]key.Binding { return [][]key.Binding{m.ShortHelp()} }

func (m *DashboardModel) View() string {
	if m.scanning {
		status := m.spinner.View() + " Reading tags and bitrate…"
		return lipgloss.JoinVertical(lipgloss.Left,
			lipgloss.Place(m.width, max(1, m.height-1), lipgloss.Center, lipgloss.Center, status),
			m.help.View(m))
	}

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

	lay := m.layout
	// Every pane row is truncated (not wrapped) and padded to the content width.
	padStyle := lipgloss.NewStyle().Width(lay.contentWidth)
	row := func(s string) string { return padStyle.Render(truncateText(s, lay.contentWidth)) }
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
	section("COVER ART SOURCE", m.coverRows()...)
	box := "[ ]"
	if m.removeSource {
		box = "[x]"
	}
	section("SOURCE MP3 FILES", focusPrefix(dashRemoveSourceToggle)+box+" Remove")

	for i, l := range left {
		left[i] = row(l)
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
		right = append(right, m.fileRows()...)
	}
	for len(right) < lay.paneHeight-2-2*paneVPadding(lay.density)-1 { // keep bitrate at the bottom
		right = append(right, "")
	}
	bitrateStr := "Detected Bitrate: Unknown"
	if m.bitrate > 0 {
		bitrateStr = fmt.Sprintf("Detected Bitrate: %d kbps", m.bitrate)
	}
	right = append(right, bitrateStr)
	for i, l := range right {
		right[i] = row(l)
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
	parts = append(parts, m.help.View(m))
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m *DashboardModel) fileRows() []string {
	var rows []string
	start, end := m.fileScroll.Visible()
	for i := start; i < end; i++ {
		prefix := "  "
		if i == m.fileScroll.Cursor() {
			prefix = "┃ "
			if m.focusIndex == dashFileList {
				prefix = lipgloss.NewStyle().Foreground(primaryColor).Render(prefix)
			}
		}
		rows = append(rows, prefix+truncateText(m.files[i], m.layout.contentWidth-2))
	}
	return rows
}

func (m *DashboardModel) coverRows() []string {
	var rows []string
	start, end := m.coverScroll.Visible()
	for i := start; i < end; i++ {
		selected := i == m.coverScroll.Cursor()
		mark := "  ( ) "
		if selected {
			mark = "  (•) "
		}
		str := truncateText(fmt.Sprintf("%s%d. %s", mark, i+1, m.covers[i].label), m.layout.contentWidth)
		switch {
		case selected && m.focusIndex == dashCoverSelection:
			str = lipgloss.NewStyle().Foreground(primaryColor).Render(str)
		case !selected:
			str = lipgloss.NewStyle().Foreground(subtextColor).Render(str)
		}
		rows = append(rows, str)
	}
	if len(rows) == 0 {
		rows = []string{""}
	}
	return rows
}
