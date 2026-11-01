package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sahilm/fuzzy"
)

type item struct {
	name     string
	isDir    bool
	isMP3    bool
	isM4B    bool
	hasM4B   bool // folder that already contains an .m4b
	mp3Count int  // mp3 files directly inside a folder
}

var fmKeys = struct {
	Up           key.Binding
	Down         key.Binding
	PageUp       key.Binding
	PageDown     key.Binding
	Top          key.Binding
	Bottom       key.Binding
	Open         key.Binding
	Back         key.Binding
	Convert      key.Binding
	Select       key.Binding
	SelectAll    key.Binding
	Book         key.Binding
	Stories      key.Binding
	Filter       key.Binding
	ClearFilter  key.Binding
	AcceptFilter key.Binding
	CancelFilter key.Binding
}{
	Up:           key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/↓", "move")),
	Down:         key.NewBinding(key.WithKeys("down", "j")),
	PageUp:       key.NewBinding(key.WithKeys("pgup", "b", "u")),
	PageDown:     key.NewBinding(key.WithKeys("pgdown", "f", "d")),
	Top:          key.NewBinding(key.WithKeys("home", "g")),
	Bottom:       key.NewBinding(key.WithKeys("end", "G")),
	Open:         key.NewBinding(key.WithKeys("enter", "right", "l"), key.WithHelp("enter", "open")),
	Back:         key.NewBinding(key.WithKeys("left", "h", "backspace"), key.WithHelp("←", "up")),
	Convert:      key.NewBinding(key.WithKeys("c"), key.WithHelp("c", "convert")),
	Select:       key.NewBinding(key.WithKeys(" "), key.WithHelp("space", "select")),
	SelectAll:    key.NewBinding(key.WithKeys("a")),
	Book:         key.NewBinding(key.WithKeys("ctrl+b")),
	Stories:      key.NewBinding(key.WithKeys("ctrl+s")),
	Filter:       key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
	ClearFilter:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear")),
	AcceptFilter: key.NewBinding(key.WithKeys("enter", "tab", "up", "down"), key.WithHelp("enter", "apply filter")),
	CancelFilter: key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel")),
}

type filterState int

const (
	unfiltered filterState = iota
	filtering
	filterApplied
)

// FileManagerModel is a directory browser. It renders and scrolls its own rows
// rather than wrapping bubbles' list, whose paginator would disagree with the
// custom rendering about how many rows a page has.
type FileManagerModel struct {
	styles  *Styles
	dir     string
	dirMP3s int // mp3 files in dir itself
	items   []item
	visible []int // indexes into items, in filter rank order
	scroll  scroller

	filter      textinput.Model
	filterState filterState

	notice string // one-off message shown above the help bar until the next key

	// picked holds absolute paths of selected mp3s. It survives moving
	// between folders, so a selection can span a whole library.
	picked map[string]bool

	help   help.Model
	width  int
	height int
}

func newFileManagerModel(styles *Styles, dir string) *FileManagerModel {
	m := &FileManagerModel{styles: styles, help: styles.newHelp(), picked: map[string]bool{}}
	m.filter = styles.newInput()
	m.filter.Prompt = "Filter: "
	m.loadDir(dir)
	return m
}

func (m *FileManagerModel) IsFiltering() bool { return m.filterState == filtering }

func (m *FileManagerModel) SetSize(w, h int) {
	m.width, m.height = w, h
	m.help.Width = w
	m.scroll.SetRows(m.listRows())
}

// listRows is the number of entry rows that fit: everything except the
// filter line (+ blank) and the blank line + help bar.
func (m *FileManagerModel) listRows() int {
	rows := m.height - 2
	if m.filterState != unfiltered {
		rows -= 2
	}
	return max(1, rows)
}

// dirStats counts the mp3 files in a folder and reports whether it has an .m4b.
func dirStats(dirPath string) (mp3s int, hasM4B bool) {
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return 0, false
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".mp3":
			mp3s++
		case ".m4b":
			hasM4B = true
		}
	}
	return mp3s, hasM4B
}

func (m *FileManagerModel) loadDir(dir string) {
	m.dir = filepath.Clean(dir)
	entries, err := os.ReadDir(m.dir)
	m.items = nil
	m.dirMP3s = 0

	// Add parent if not at root
	if parentDir := filepath.Dir(m.dir); parentDir != m.dir {
		m.items = append(m.items, item{name: "..", isDir: true})
	}

	if err == nil {
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") { // skip hidden files
				continue
			}
			it := item{name: e.Name(), isDir: e.IsDir()}
			if it.isDir {
				it.mp3Count, it.hasM4B = dirStats(filepath.Join(m.dir, e.Name()))
			} else {
				ext := strings.ToLower(filepath.Ext(e.Name()))
				it.isMP3, it.isM4B = ext == ".mp3", ext == ".m4b"
				if it.isMP3 {
					m.dirMP3s++
				}
			}
			m.items = append(m.items, it)
		}
	}
	m.clearFilter()
}

func (m *FileManagerModel) loadDirAndSelectChild(dir string, childName string) {
	m.loadDir(dir)
	for vi, idx := range m.visible {
		if m.items[idx].name == childName {
			m.scroll.Select(vi)
			break
		}
	}
}

// reload re-reads the current directory and keeps the highlighted entry.
// Selected files that no longer exist are dropped.
func (m *FileManagerModel) reload() {
	for path := range m.picked {
		if _, err := os.Stat(path); err != nil {
			delete(m.picked, path)
		}
	}
	var name string
	if it, ok := m.selected(); ok {
		name = it.name
	}
	m.loadDirAndSelectChild(m.dir, name)
}

func (m *FileManagerModel) selected() (item, bool) {
	c := m.scroll.Cursor()
	if c < 0 || c >= len(m.visible) {
		return item{}, false
	}
	return m.items[m.visible[c]], true
}

// applyFilter recomputes visible entries (fuzzy match, best first).
func (m *FileManagerModel) applyFilter() {
	m.visible = m.visible[:0]
	term := m.filter.Value()
	if term == "" {
		for i := range m.items {
			m.visible = append(m.visible, i)
		}
	} else {
		names := make([]string, len(m.items))
		for i, it := range m.items {
			names[i] = it.name
		}
		for _, match := range fuzzy.Find(term, names) {
			m.visible = append(m.visible, match.Index)
		}
	}
	m.scroll.SetCount(len(m.visible))
	m.scroll.Top()
}

func (m *FileManagerModel) setFilterState(s filterState) {
	m.filterState = s
	m.scroll.SetRows(m.listRows())
}

func (m *FileManagerModel) clearFilter() {
	m.filter.Reset()
	m.filter.Blur()
	m.setFilterState(unfiltered)
	m.applyFilter()
}

func (m *FileManagerModel) goToParent() {
	if parentDir := filepath.Dir(m.dir); parentDir != m.dir {
		m.loadDirAndSelectChild(parentDir, filepath.Base(m.dir))
	}
}

func (m *FileManagerModel) Init() tea.Cmd {
	return nil
}

func (m *FileManagerModel) Update(msg tea.Msg) tea.Cmd {
	keyMsg, isKey := msg.(tea.KeyMsg)

	if m.filterState == filtering {
		if isKey {
			switch {
			case key.Matches(keyMsg, fmKeys.CancelFilter):
				m.clearFilter()
				return nil
			case key.Matches(keyMsg, fmKeys.AcceptFilter):
				m.filter.Blur()
				if m.filter.Value() == "" {
					m.setFilterState(unfiltered)
				} else {
					m.setFilterState(filterApplied)
				}
				return nil
			}
		}
		// Typing and cursor blink go to the filter input.
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		if isKey {
			m.applyFilter()
		}
		return cmd
	}

	if !isKey {
		return nil
	}
	m.notice = ""
	switch {
	case key.Matches(keyMsg, fmKeys.Up):
		m.scroll.Move(-1)
	case key.Matches(keyMsg, fmKeys.Down):
		m.scroll.Move(1)
	case key.Matches(keyMsg, fmKeys.PageUp):
		m.scroll.Move(-m.scroll.Page())
	case key.Matches(keyMsg, fmKeys.PageDown):
		m.scroll.Move(m.scroll.Page())
	case key.Matches(keyMsg, fmKeys.Top):
		m.scroll.Top()
	case key.Matches(keyMsg, fmKeys.Bottom):
		m.scroll.Bottom()
	case key.Matches(keyMsg, fmKeys.Filter):
		m.setFilterState(filtering)
		return m.filter.Focus()
	case key.Matches(keyMsg, fmKeys.ClearFilter) && m.filterState == filterApplied:
		m.clearFilter()
	case key.Matches(keyMsg, fmKeys.ClearFilter) && len(m.picked) > 0:
		m.clearSelection()
	case key.Matches(keyMsg, fmKeys.Select):
		m.toggleSelected()
	case key.Matches(keyMsg, fmKeys.SelectAll):
		m.toggleFolder(m.dir)
	case key.Matches(keyMsg, fmKeys.Book):
		if len(m.picked) > 0 {
			m.notice = "Selected files are converted as stories: press ctrl+s, or esc to clear the selection"
			return nil
		}
		return m.convertTarget(ModeBook)
	case key.Matches(keyMsg, fmKeys.Stories):
		return m.convertTarget(ModeStories)
	case key.Matches(keyMsg, fmKeys.Back):
		m.goToParent()
	case key.Matches(keyMsg, fmKeys.Open):
		if it, ok := m.selected(); ok && it.isDir {
			if it.name == ".." {
				m.goToParent()
			} else {
				m.loadDir(filepath.Join(m.dir, it.name))
			}
		}
	case key.Matches(keyMsg, fmKeys.Convert):
		return m.convertTarget(ModeBook)
	}
	return nil
}

// targetDir is the folder c/ctrl+b/ctrl+s convert: the highlighted folder,
// or the current one when a file or ".." is highlighted.
func (m *FileManagerModel) targetDir() string {
	if it, ok := m.selected(); ok && it.isDir && it.name != ".." {
		return filepath.Join(m.dir, it.name)
	}
	return m.dir
}

// convertTarget opens the converter: for the selection (always stories) if
// there is one, otherwise for targetDir in the given mode.
func (m *FileManagerModel) convertTarget(mode ConversionMode) tea.Cmd {
	if files := m.selectedFiles(); len(files) > 0 {
		dir := commonDir(files)
		return func() tea.Msg { return msgSwitchToDashboard{dir: dir, files: files, mode: ModeStories} }
	}
	dir := m.targetDir()
	if len(listFilesByExt(dir, ".mp3")) == 0 {
		m.notice = "No MP3 files in " + filepath.Base(dir) + " to convert"
		return nil
	}
	return func() tea.Msg { return msgSwitchToDashboard{dir: dir, mode: mode} }
}

// selectedFiles returns the selection, sorted.
func (m *FileManagerModel) selectedFiles() []string {
	files := make([]string, 0, len(m.picked))
	for path := range m.picked {
		files = append(files, path)
	}
	sort.Strings(files)
	return files
}

func (m *FileManagerModel) clearSelection() {
	m.picked = map[string]bool{}
}

// toggleSelected selects or unselects the highlighted mp3 (and moves down,
// so several files can be picked in a row), or all mp3s of a highlighted folder.
func (m *FileManagerModel) toggleSelected() {
	it, ok := m.selected()
	switch {
	case !ok:
	case it.isDir && it.name != "..":
		m.toggleFolder(filepath.Join(m.dir, it.name))
	case it.isMP3:
		path := filepath.Join(m.dir, it.name)
		if m.picked[path] {
			delete(m.picked, path)
		} else {
			m.picked[path] = true
		}
		m.scroll.Move(1)
	default:
		m.notice = "Only MP3 files can be selected"
	}
}

// toggleFolder selects all mp3s directly in dir, or unselects them if all
// are already selected.
func (m *FileManagerModel) toggleFolder(dir string) {
	names := listFilesByExt(dir, ".mp3")
	if len(names) == 0 {
		m.notice = "No MP3 files in " + filepath.Base(dir)
		return
	}
	all := true
	for _, n := range names {
		all = all && m.picked[filepath.Join(dir, n)]
	}
	for _, n := range names {
		if all {
			delete(m.picked, filepath.Join(dir, n))
		} else {
			m.picked[filepath.Join(dir, n)] = true
		}
	}
}

// selectedIn counts selected files directly inside dir.
func (m *FileManagerModel) selectedIn(dir string) int {
	n := 0
	for path := range m.picked {
		if filepath.Dir(path) == dir {
			n++
		}
	}
	return n
}

// commonDir is the deepest folder that contains all paths.
func commonDir(paths []string) string {
	dir := filepath.Dir(paths[0])
	for _, p := range paths[1:] {
		for {
			rel, err := filepath.Rel(dir, p)
			if err == nil && !strings.HasPrefix(rel, "..") {
				break
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return dir
}

// ShortHelp implements help.KeyMap with the keys that work right now.
func (m *FileManagerModel) ShortHelp() []key.Binding {
	if m.filterState == filtering {
		return append([]key.Binding{fmKeys.AcceptFilter, fmKeys.CancelFilter}, globalHelp(true)...)
	}
	clear := fmKeys.ClearFilter
	clear.SetEnabled(m.filterState == filterApplied || len(m.picked) > 0)
	return append([]key.Binding{fmKeys.Open, fmKeys.Back, fmKeys.Select, fmKeys.Convert, fmKeys.Filter, clear},
		globalHelp(false)...)
}

func (m *FileManagerModel) FullHelp() [][]key.Binding { return [][]key.Binding{m.ShortHelp()} }

func (m *FileManagerModel) View() string {
	var lines []string
	if m.filterState != unfiltered {
		lines = append(lines, m.filter.View(), "")
	}

	rows := m.listRows()
	entriesStart := len(lines)
	if len(m.visible) == 0 {
		lines = append(lines, "  No files found.")
	} else {
		start, end := m.scroll.Visible()
		for i := start; i < end; i++ {
			lines = append(lines, m.rowView(m.items[m.visible[i]], i == m.scroll.Cursor()))
		}
	}

	for len(lines)-entriesStart < rows { // pad so the help bar stays at the bottom
		lines = append(lines, "")
	}

	status := m.styles.Dim.Render(truncateText(m.hint(), m.width))
	if m.notice != "" {
		status = m.styles.Warning.Render(truncateText(m.notice, m.width))
	}
	lines = append(lines, status, m.help.View(m))
	return strings.Join(lines, "\n")
}

// hint is the status line: what c / ctrl+b / ctrl+s would do right now.
func (m *FileManagerModel) hint() string {
	if n := len(m.picked); n > 0 {
		noun := "files"
		if n == 1 {
			noun = "file"
		}
		return fmt.Sprintf("%d %s selected · c or ctrl+s: convert as stories · esc: clear", n, noun)
	}
	dir, mp3s := m.dir, m.dirMP3s
	if it, ok := m.selected(); ok && it.isDir && it.name != ".." {
		dir, mp3s = filepath.Join(m.dir, it.name), it.mp3Count
	}
	if mp3s == 0 {
		return ""
	}
	return fmt.Sprintf("%s: c or ctrl+b convert as a book · ctrl+s as stories", filepath.Base(dir))
}

// rowView renders one entry: icon and name, then a dim note for folders
// ("12 mp3 · ✓ m4b") so the colours don't need a legend.
func (m *FileManagerModel) rowView(it item, current bool) string {
	icon := "📄 "
	path := filepath.Join(m.dir, it.name)
	picked := it.isMP3 && m.picked[path]
	switch {
	case it.isDir:
		icon = "📁 "
	case picked:
		icon = "✓  " // same width as the emoji icons
	}
	var notes []string
	if it.isDir && it.mp3Count > 0 {
		notes = append(notes, fmt.Sprintf("%d mp3", it.mp3Count))
		if n := m.selectedIn(path); n > 0 && it.name != ".." {
			notes = append(notes, fmt.Sprintf("%d selected", n))
		}
	}
	if it.hasM4B {
		notes = append(notes, "✓ m4b")
	}
	note := ""
	if len(notes) > 0 {
		note = "  " + strings.Join(notes, " · ")
	}

	c := m.styles.Colors
	nameColor := c.Text
	switch {
	case it.isM4B || it.hasM4B:
		nameColor = c.Success
	case picked:
		nameColor = c.Primary
	case !it.isDir && !it.isMP3:
		nameColor = c.Subtext
	}
	nameStyle := lipgloss.NewStyle().Foreground(nameColor).Bold(current)
	prefix := "  "
	if current {
		prefix = lipgloss.NewStyle().Foreground(c.Primary).Bold(true).Render("> ")
	}

	avail := m.width - 2 // selection prefix
	if lipgloss.Width(note) > avail/2 {
		note = ""
	}
	name := truncateText(icon+it.name, avail-lipgloss.Width(note))
	return prefix + nameStyle.Render(name) + m.styles.Dim.Render(note)
}
