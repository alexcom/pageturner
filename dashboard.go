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
	Book        key.Binding
	Stories     key.Binding
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
	// Ctrl keys work while typing, so they are the mode shortcuts.
	Book:    key.NewBinding(key.WithKeys("ctrl+b"), key.WithHelp("^b/^s", "book/stories")),
	Stories: key.NewBinding(key.WithKeys("ctrl+s")),
}

type DashboardModel struct {
	ctx context.Context
	dir string

	// selection is the file manager's selection (absolute paths); nil means
	// all MP3s in dir. A selection is always converted as stories.
	selection []string
	mode      ConversionMode
	scan      dirScan

	// Directory scan runs in the background; the form is locked until it ends.
	scanning bool
	spinner  spinner.Model

	// Right pane: files to convert (absolute paths; read-only, scrollable)
	files      []string
	fileScroll scroller
	bitrate    int // detected; 0 if unknown
	bitrateIn  textinput.Model

	// Left pane
	inputs       []textinput.Model
	covers       []coverItem
	coverScroll  scroller // cursor is the selected cover
	removeSource bool

	// UI state
	focusIndex  int
	showConfirm bool
	notice      string // validation message, cleared on the next key
	help        help.Model

	width  int
	height int
	layout dashLayout
}

func (m *DashboardModel) updateThemeStyles() {
	for i := range m.inputs {
		applyInputStyles(&m.inputs[i])
	}
	applyInputStyles(&m.bitrateIn)
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
	dashBitrate
	dashBookButton
	dashStoriesButton
	dashStartButton
)

const (
	minBitrate = 8
	maxBitrate = 320
)

// newDashboardModel opens the converter for dir, or for selection (absolute
// mp3 paths under dir) when it is not nil.
func newDashboardModel(ctx context.Context, dir string, selection []string, mode ConversionMode, removeSource bool) *DashboardModel {
	if selection != nil {
		mode = ModeStories
	}
	m := &DashboardModel{
		ctx:          ctx,
		dir:          dir,
		selection:    selection,
		mode:         mode,
		removeSource: removeSource,
		inputs:       make([]textinput.Model, 5),
		help:         newHelpModel(),
		scanning:     true,
		spinner:      spinner.New(spinner.WithSpinner(spinner.Dot)),
	}
	m.spinner.Style = lipgloss.NewStyle().Foreground(primaryColor)

	for i := range m.inputs {
		t := textinput.New()
		t.Prompt = ""
		t.CharLimit = 128
		applyInputStyles(&t)
		m.inputs[i] = t
	}
	m.applyPlaceholders()
	m.bitrateIn = textinput.New()
	m.bitrateIn.Prompt = ""
	m.bitrateIn.CharLimit = 3
	m.bitrateIn.Width = 3
	m.bitrateIn.Placeholder = "64"
	applyInputStyles(&m.bitrateIn)

	m.focusIndex = dashInputArtist
	m.inputs[dashInputArtist].Focus()
	m.updateLayout(80, 24)
	return m
}

func (m *DashboardModel) IsEditingText() bool {
	if m.scanning {
		return false
	}
	return (m.focusIndex >= dashInputArtist && m.focusIndex <= dashInputOutFilename) || m.focusIndex == dashBitrate
}

// focusedInput returns the text input with focus, or nil.
func (m *DashboardModel) focusedInput() *textinput.Model {
	switch {
	case m.scanning:
		return nil
	case m.focusIndex >= dashInputArtist && m.focusIndex <= dashInputOutFilename:
		return &m.inputs[m.focusIndex]
	case m.focusIndex == dashBitrate:
		return &m.bitrateIn
	}
	return nil
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
	return scanSource(ctx, dir, nil)
}

// scanSource scans the mp3s in dir, or only selection (absolute paths) when
// set. Folder images are offered as covers only for a whole folder.
func scanSource(ctx context.Context, dir string, selection []string) dirScan {
	s := dirScan{dir: dir, files: selection}
	if selection == nil {
		for _, name := range listFilesByExt(dir, ".mp3") {
			s.files = append(s.files, filepath.Join(dir, name))
		}
	}
	if len(s.files) > 0 {
		if bb, err := getMetaJsonBytes(ctx, "", s.files[0]); err == nil {
			var fileMeta container
			if err := json.Unmarshal(bb.Bytes(), &fileMeta); err == nil {
				s.artist = fileMeta.Format.Tags.Artist
				s.album = fileMeta.Format.Tags.Album
				s.title = fileMeta.Format.Tags.Title
				s.performer = fileMeta.Format.Tags.Performer
			}
		}
		if br, err := detectBitrateOf(ctx, s.files); err == nil {
			s.bitrate = br
		}
		s.hasEmbeddedCover = hasCoverImage(ctx, "", s.files[0])
	}
	if selection != nil {
		return s
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
	ctx, dir, sel := m.ctx, m.dir, m.selection
	return func() tea.Msg { return msgDirScanned{scan: scanSource(ctx, dir, sel)} }
}

// tagPrefill is what a metadata field starts with in a mode. In stories mode
// an empty field means "keep each file's own tag".
func (m *DashboardModel) tagPrefill(mode ConversionMode, field int) string {
	if mode == ModeStories {
		return ""
	}
	switch field {
	case dashInputArtist:
		return m.scan.artist
	case dashInputAlbum:
		return m.scan.album
	case dashInputPerformer:
		return m.scan.performer
	}
	return ""
}

var modeTagFields = []int{dashInputArtist, dashInputAlbum, dashInputPerformer}

func (m *DashboardModel) applyPlaceholders() {
	ph := []string{"Unknown Artist", "Unknown Album", "Same as album", "Not set", "Output Filename"}
	if m.mode == ModeStories {
		ph = []string{"From each file", "From each file", "", "From each file", ""}
	}
	for i := range m.inputs {
		m.inputs[i].Placeholder = ph[i]
	}
}

// setMode switches between book and stories. Fields the user didn't change
// follow the new mode's prefill; edited fields are kept.
func (m *DashboardModel) setMode(mode ConversionMode) tea.Cmd {
	if mode == m.mode {
		return nil
	}
	if mode == ModeBook && m.selection != nil {
		m.notice = "Selected files are always converted as separate stories."
		return nil
	}
	if !m.scanning {
		for _, f := range modeTagFields {
			if m.inputs[f].Value() == m.tagPrefill(m.mode, f) {
				m.inputs[f].SetValue(m.tagPrefill(mode, f))
			}
		}
	}
	m.mode = mode
	m.applyPlaceholders()
	if !m.scanning {
		m.buildCovers()
		m.updateLayout(m.width, m.height)
	}
	if !m.focusable(m.focusIndex) {
		return m.moveFocus(1)
	}
	return nil
}

// focusable reports whether focus index i can take focus in the current mode.
func (m *DashboardModel) focusable(i int) bool {
	switch i {
	case dashInputTitle, dashInputOutFilename:
		return m.mode == ModeBook // stories take both from each file
	case dashBookButton:
		return m.selection == nil
	}
	return true
}

// buildCovers fills the cover choices for the mode, keeping the current choice if it still exists.
func (m *DashboardModel) buildCovers() {
	var prev coverItem
	if c := m.coverScroll.Cursor(); c < len(m.covers) {
		prev = m.covers[c]
	}
	m.covers = []coverItem{
		{kind: CoverAuto, label: "Auto-detect"},
		{kind: CoverDefault, label: "Default Cover"},
	}
	switch {
	case m.mode == ModeStories:
		m.covers = append(m.covers, coverItem{kind: CoverExtract, label: "Embedded in each MP3"})
	case m.scan.hasEmbeddedCover:
		m.covers = append(m.covers, coverItem{kind: CoverExtract, label: "Extract from MP3"})
	}
	for _, name := range m.scan.images {
		m.covers = append(m.covers, coverItem{kind: CoverFile, label: name, file: name})
	}
	m.coverScroll.SetCount(len(m.covers))
	m.coverScroll.Top()
	for i, c := range m.covers {
		if c.kind == prev.kind && c.file == prev.file {
			m.coverScroll.Select(i)
		}
	}
}

// displayName is how a file to convert is listed: relative to the dashboard folder.
func (m *DashboardModel) displayName(path string) string {
	if rel, err := filepath.Rel(m.dir, path); err == nil && !strings.HasPrefix(rel, "..") {
		return rel
	}
	return path
}

// existingOutputs lists the .m4b files the conversion would overwrite.
func (m *DashboardModel) existingOutputs() []string {
	var outs []string
	if m.mode == ModeBook {
		outs = []string{filepath.Join(m.dir, m.getConfig().OutFilename)}
	} else {
		for _, f := range m.files {
			outs = append(outs, storyOutputPath(f))
		}
	}
	var existing []string
	for _, o := range outs {
		if _, err := os.Stat(o); err == nil {
			existing = append(existing, o)
		}
	}
	return existing
}

func (m *DashboardModel) applyScan(s dirScan) {
	m.scanning = false
	m.scan = s
	m.files = s.files
	m.bitrate = s.bitrate
	m.fileScroll.SetCount(len(m.files))

	if s.bitrate > 0 {
		m.bitrateIn.SetValue(fmt.Sprint(s.bitrate))
	}

	if len(m.files) > 0 {
		// Missing tags stay empty and show a placeholder, so "Unknown ..."
		// isn't written into the book's tags.
		for _, f := range modeTagFields {
			m.inputs[f].SetValue(m.tagPrefill(m.mode, f))
		}
		m.inputs[dashInputTitle].SetValue(s.title)

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

	m.buildCovers()
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
		BitRate:      m.chosenBitrate(),
		Mode:         m.mode,
		Files:        m.files,
	}
}

// chosenBitrate is the bitrate field's value, or 0 if it isn't valid.
func (m *DashboardModel) chosenBitrate() int {
	var br int
	if _, err := fmt.Sscan(m.bitrateIn.Value(), &br); err != nil || br < minBitrate || br > maxBitrate {
		return 0
	}
	return br
}

// validate returns why the conversion can't start, or "".
func (m *DashboardModel) validate() string {
	if len(m.files) == 0 {
		return "Nothing to convert: no MP3 files here. Press esc to pick another folder."
	}
	if m.chosenBitrate() == 0 {
		return fmt.Sprintf("Enter a bitrate between %d and %d kbps.", minBitrate, maxBitrate)
	}
	return ""
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

// focusOn moves focus to index i, focusing or blurring the text inputs.
func (m *DashboardModel) focusOn(i int) tea.Cmd {
	m.focusIndex = i
	var cmds []tea.Cmd
	for j := dashInputArtist; j <= dashInputOutFilename; j++ {
		if j == i {
			cmds = append(cmds, m.inputs[j].Focus())
		} else {
			m.inputs[j].Blur()
		}
	}
	if i == dashBitrate {
		cmds = append(cmds, m.bitrateIn.Focus())
	} else {
		m.bitrateIn.Blur()
	}
	return tea.Batch(cmds...)
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
		m.notice = ""
		switch {
		case key.Matches(msg, dashKeys.Book) && !m.showConfirm:
			return m.setMode(ModeBook)
		case key.Matches(msg, dashKeys.Stories) && !m.showConfirm:
			return m.setMode(ModeStories)
		}
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
			// Enter advances through the form, picks a mode on the mode
			// buttons, and only the Start button starts the job.
			switch m.focusIndex {
			case dashBookButton, dashStoriesButton:
				// Pick the mode, then go to Start.
				mode := ModeBook
				if m.focusIndex == dashStoriesButton {
					mode = ModeStories
				}
				if mode == ModeBook && m.selection != nil {
					return m.setMode(mode) // explains why not
				}
				return tea.Batch(m.setMode(mode), m.focusOn(dashStartButton))
			case dashStartButton:
			default:
				return m.moveFocus(1)
			}
			if problem := m.validate(); problem != "" {
				m.notice = problem
				if len(m.files) > 0 { // the bitrate is wrong
					return m.focusOn(dashBitrate)
				}
				return nil
			}
			if len(m.existingOutputs()) > 0 {
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

// moveFocus moves focus by delta, wrapping around and skipping what the
// current mode disables.
func (m *DashboardModel) moveFocus(delta int) tea.Cmd {
	n := dashStartButton + 1
	i := m.focusIndex
	for k := 0; k < n; k++ {
		i = (i + delta + n) % n
		if m.focusable(i) {
			break
		}
	}
	return m.focusOn(i)
}

func (m *DashboardModel) updateInputs(msg tea.Msg) tea.Cmd {
	in := m.focusedInput()
	if in == nil {
		return nil
	}
	if k, ok := msg.(tea.KeyMsg); ok && in == &m.bitrateIn && k.Type == tea.KeyRunes {
		for _, r := range k.Runes {
			if r < '0' || r > '9' {
				return nil // digits only
			}
		}
	}
	var cmd tea.Cmd
	*in, cmd = in.Update(msg)
	return cmd
}

// ShortHelp implements help.KeyMap with the keys that work at the current
// focus. Essentials come first; help truncates from the end on narrow terminals.
func (m *DashboardModel) ShortHelp() []key.Binding {
	if m.scanning {
		return append([]key.Binding{dashKeys.Back, dashKeys.Book}, globalHelp(false)...)
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
	switch m.focusIndex {
	case dashStartButton:
		confirm.SetHelp("enter", "start")
	case dashBookButton, dashStoriesButton:
		confirm.SetHelp("enter", "select")
	}
	b = append(b, confirm, dashKeys.Next, dashKeys.Back)
	global := globalHelp(editing)
	b = append(b, global[0], global[1], dashKeys.Book) // quit / ctrl+c quit, then mode
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
		existing := m.existingOutputs()
		var msg []string
		if len(existing) == 1 {
			msg = []string{fmt.Sprintf("%q already exists.", m.displayName(existing[0])), "Overwrite it? (y/N)"}
		} else {
			msg = []string{fmt.Sprintf("%d of the .m4b files already exist.", len(existing)), "Overwrite them? (y/N)"}
		}
		dialogBox := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(errorColor).
			Padding(1, 2).
			MaxWidth(m.width).
			Render(lipgloss.JoinVertical(lipgloss.Center, append([]string{
				lipgloss.NewStyle().Bold(true).Foreground(errorColor).Render("Output File Exists"), ""},
				msg...)...))

		return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, dialogBox)
	}

	lay := m.layout
	stories := m.mode == ModeStories
	dim := lipgloss.NewStyle().Foreground(subtextColor)
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
		field := m.inputs[i].View()
		if !m.focusable(i) {
			field = dim.Render("From each file")
		}
		meta = append(meta, focusPrefix(i)+labels[i]+field)
	}
	section("METADATA", meta...)
	if stories {
		section("OUTPUT M4B FILES", "  "+dim.Render("<name>.m4b next to each MP3"))
	} else {
		section("OUTPUT M4B FILE", focusPrefix(dashInputOutFilename)+m.inputs[dashInputOutFilename].View())
	}
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

	// Right: files to convert
	filesTitle := "DISCOVERED MP3 FILES"
	if m.selection != nil {
		filesTitle = "SELECTED MP3 FILES"
	}
	if len(m.files) > 0 {
		filesTitle += fmt.Sprintf(" (%d)", len(m.files))
	}
	right := []string{title(filesTitle)}
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
	detected := "not detected"
	switch {
	case m.bitrate > 0 && m.chosenBitrate() == m.bitrate:
		detected = "detected"
	case m.bitrate > 0:
		detected = fmt.Sprintf("detected %d", m.bitrate)
	}
	right = append(right, focusPrefix(dashBitrate)+"Bitrate: "+m.bitrateIn.View()+"kbps  "+dim.Render(detected))
	for i, l := range right {
		right[i] = row(l)
	}
	rightStr := paneStyle(m.focusIndex == dashFileList || m.focusIndex == dashBitrate).Render(lipgloss.JoinVertical(lipgloss.Left, right...))

	split := lipgloss.JoinHorizontal(lipgloss.Top, leftStr, strings.Repeat(" ", layoutPaneGap), rightStr)

	// Buttons: mode (book / stories) and start.
	startText := "START CONVERSION (Enter)"
	switch {
	case len(m.files) == 0:
		startText = "NO MP3 FILES TO CONVERT"
	case stories:
		startText = fmt.Sprintf("CONVERT %d STORIES (Enter)", len(m.files))
		if len(m.files) == 1 {
			startText = "CONVERT 1 STORY (Enter)"
		}
	}
	// Shorter labels when the full ones don't fit the width.
	var buttons string
	for _, l := range [][3]string{{"BOOK ^B", "STORIES ^S", startText}, {"BOOK", "STORIES", "START"}} {
		buttons = lipgloss.JoinHorizontal(lipgloss.Center,
			m.button(dashBookButton, l[0], !stories, m.selection == nil),
			" ",
			m.button(dashStoriesButton, l[1], stories, true),
			"   ",
			m.button(dashStartButton, l[2], true, len(m.files) > 0),
		)
		if lipgloss.Width(buttons) <= lipgloss.Width(split) {
			break
		}
	}
	btnBlock := lipgloss.NewStyle().Width(lipgloss.Width(split)).Align(lipgloss.Center)
	if !lay.compactButton {
		btnBlock = btnBlock.Margin(1, 0)
	}

	notice := ""
	if m.notice != "" {
		notice = lipgloss.NewStyle().Foreground(warningColor).Render(truncateText(m.notice, m.width))
	}
	parts := []string{split, btnBlock.Render(buttons)}
	switch {
	case !lay.compactButton:
		parts = append(parts, notice, m.help.View(m))
	case notice != "":
		parts = append(parts, notice) // no spare line: the notice replaces help until the next key
	default:
		parts = append(parts, m.help.View(m))
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// button renders one of the bottom buttons. on means filled (the chosen mode,
// or an enabled Start); focus adds "> <" and a highlighted frame.
func (m *DashboardModel) button(idx int, text string, on, enabled bool) string {
	focused := m.focusIndex == idx
	if focused {
		text = "> " + text + " <"
	}
	style := lipgloss.NewStyle().Padding(0, 2)
	switch {
	case !enabled:
		style = style.Background(secondaryColor).Foreground(subtextColor)
	case on:
		style = style.Background(primaryColor).Foreground(onPrimaryColor).Bold(true)
	default:
		style = style.Background(secondaryColor).Foreground(textColor)
	}
	out := style.Render(text)
	if !m.layout.compactButton {
		frame := secondaryColor
		if focused && enabled {
			frame = primaryColor
		}
		out = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(frame).Render(out)
	}
	return out
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
		rows = append(rows, prefix+truncateText(m.displayName(m.files[i]), m.layout.contentWidth-2))
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
