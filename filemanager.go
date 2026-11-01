package main

import (
	"fmt"
	"os"
	"path/filepath"
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
	Filter:       key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
	ClearFilter:  key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "clear filter")),
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
	dir     string
	items   []item
	visible []int // indexes into items, in filter rank order
	scroll  scroller

	filter      textinput.Model
	filterState filterState

	notice string // one-off message shown above the help bar until the next key

	help   help.Model
	width  int
	height int
}

func newFileManagerModel(dir string) *FileManagerModel {
	m := &FileManagerModel{help: newHelpModel()}
	m.filter = textinput.New()
	m.filter.Prompt = "Filter: "
	applyInputStyles(&m.filter)
	m.loadDir(dir)
	return m
}

func (m *FileManagerModel) updateThemeStyles() {
	applyHelpStyles(&m.help.Styles)
	applyInputStyles(&m.filter)
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

// reload re-reads the current directory and keeps the selected entry.
func (m *FileManagerModel) reload() {
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
		return m.convertTarget()
	}
	return nil
}

// convertTarget opens the converter for the highlighted folder, or for the
// current folder when a file or ".." is highlighted.
func (m *FileManagerModel) convertTarget() tea.Cmd {
	dir := m.dir
	if it, ok := m.selected(); ok && it.isDir && it.name != ".." {
		dir = filepath.Join(m.dir, it.name)
	}
	if len(listFilesByExt(dir, ".mp3")) == 0 {
		m.notice = "No MP3 files in " + filepath.Base(dir) + " to convert"
		return nil
	}
	return func() tea.Msg { return msgSwitchToDashboard{dir: dir} }
}

// ShortHelp implements help.KeyMap with the keys that work right now.
func (m *FileManagerModel) ShortHelp() []key.Binding {
	if m.filterState == filtering {
		return append([]key.Binding{fmKeys.AcceptFilter, fmKeys.CancelFilter}, globalHelp(true)...)
	}
	clear := fmKeys.ClearFilter
	clear.SetEnabled(m.filterState == filterApplied)
	return append([]key.Binding{fmKeys.Up, fmKeys.Open, fmKeys.Back, fmKeys.Convert, fmKeys.Filter, clear},
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

	status := ""
	if m.notice != "" {
		status = lipgloss.NewStyle().Foreground(warningColor).Render(truncateText(m.notice, m.width))
	}
	lines = append(lines, status, m.help.View(m))
	return strings.Join(lines, "\n")
}

// rowView renders one entry: icon and name, then a dim note for folders
// ("12 mp3 · ✓ m4b") so the colours don't need a legend.
func (m *FileManagerModel) rowView(it item, current bool) string {
	icon := "📄 "
	if it.isDir {
		icon = "📁 "
	}
	var notes []string
	if it.isDir && it.mp3Count > 0 {
		notes = append(notes, fmt.Sprintf("%d mp3", it.mp3Count))
	}
	if it.hasM4B {
		notes = append(notes, "✓ m4b")
	}
	note := ""
	if len(notes) > 0 {
		note = "  " + strings.Join(notes, " · ")
	}

	nameColor := textColor
	switch {
	case it.isM4B || it.hasM4B:
		nameColor = successColor
	case !it.isDir && !it.isMP3:
		nameColor = subtextColor
	}
	nameStyle := lipgloss.NewStyle().Foreground(nameColor).Bold(current)
	prefix := "  "
	if current {
		prefix = lipgloss.NewStyle().Foreground(primaryColor).Bold(true).Render("> ")
	}

	avail := m.width - 2 // selection prefix
	if lipgloss.Width(note) > avail/2 {
		note = ""
	}
	name := truncateText(icon+it.name, avail-lipgloss.Width(note))
	return prefix + nameStyle.Render(name) + lipgloss.NewStyle().Foreground(subtextColor).Render(note)
}
