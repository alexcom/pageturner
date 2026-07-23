package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type item struct {
	name   string
	isDir  bool
	isM4B  bool
	hasM4B bool
}

func (i item) Title() string {
	if i.isDir {
		return "📁 " + i.name
	}
	return "📄 " + i.name
}
func (i item) Description() string {
	if i.name == ".." {
		return "Parent Directory"
	}
	if i.isDir {
		return "Directory"
	}
	return "File"
}
func (i item) FilterValue() string { return i.name }

var fmKeys = struct {
	Open  key.Binding
	Back  key.Binding
	Dir   key.Binding
	Theme key.Binding
	Quit  key.Binding
}{
	Open: key.NewBinding(
		key.WithKeys("enter", "o", " "),
		key.WithHelp("space/enter", "open"),
	),
	Back: key.NewBinding(
		key.WithKeys("left", "h"),
		key.WithHelp("←/h", "back"),
	),
	Dir: key.NewBinding(
		key.WithKeys("right", "l"),
		key.WithHelp("→/l", "enter dir"),
	),
	Theme: key.NewBinding(
		key.WithKeys("t", "T"),
		key.WithHelp("t", "theme"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q"),
		key.WithHelp("q", "quit"),
	),
}

type FileManagerModel struct {
	dir       string
	list      list.Model
	topOffset int
}

func (m *FileManagerModel) updateThemeStyles() {
	applyHelpStyles(&m.list.Help.Styles)
}

func newFileManagerModel(dir string) *FileManagerModel {
	m := &FileManagerModel{
		dir: dir,
	}

	delegate := list.NewDefaultDelegate()
	delegate.ShowDescription = false
	delegate.SetSpacing(0)

	m.list = list.New([]list.Item{}, delegate, 80, 25)
	m.list.SetShowStatusBar(false)
	m.list.SetShowPagination(false)
	m.list.SetFilteringEnabled(true)
	m.list.SetShowTitle(false)
	m.list.KeyMap.Quit = key.NewBinding()
	applyHelpStyles(&m.list.Help.Styles)

	m.list.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{fmKeys.Open, fmKeys.Back, fmKeys.Theme, fmKeys.Quit}
	}
	m.list.AdditionalFullHelpKeys = m.list.AdditionalShortHelpKeys

	m.loadDir(dir)
	return m
}

func dirHasM4B(dirPath string) bool {
	return len(listFilesByExt(dirPath, ".m4b")) > 0
}

func (m *FileManagerModel) loadDir(dir string) {
	m.dir = filepath.Clean(dir)
	entries, err := os.ReadDir(m.dir)
	var items []list.Item

	// List title is managed by the main UI

	// Add parent if not at root
	if parentDir := filepath.Dir(m.dir); parentDir != m.dir {
		items = append(items, item{name: "..", isDir: true})
	}

	if err == nil {
		for _, e := range entries {
			// skip hidden files
			if !strings.HasPrefix(e.Name(), ".") {
				isDir := e.IsDir()
				isM4B := !isDir && strings.HasSuffix(strings.ToLower(e.Name()), ".m4b")
				hasM4B := false
				if isDir {
					hasM4B = dirHasM4B(filepath.Join(m.dir, e.Name()))
				}
				items = append(items, item{
					name:   e.Name(),
					isDir:  isDir,
					isM4B:  isM4B,
					hasM4B: hasM4B,
				})
			}
		}
	}
	m.list.SetItems(items)
	m.list.ResetSelected()
	m.topOffset = 0
}

func (m *FileManagerModel) loadDirAndSelectChild(dir string, childName string) {
	m.loadDir(dir)
	for i, it := range m.list.Items() {
		if it.(item).name == childName {
			m.list.Select(i)
			break
		}
	}
}

func (m *FileManagerModel) Init() tea.Cmd {
	return nil
}

func (m *FileManagerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h, v := docStyle.GetFrameSize()
		// Account for the main global title and directory view above the list
		titleHeight := 6 // Bordered box is 3 lines tall, plus dirView (1) and 2 spaces
		m.list.SetSize(msg.Width-h, msg.Height-v-titleHeight)
		// We still pass WindowSizeMsg down so the list updates itself.
	case tea.KeyMsg:
		// We only intercept keys for directory traversal and selection.
		// Navigation (up/down/j/k) and filtering are handled by m.list.Update
		if !m.list.SettingFilter() {
			if key.Matches(msg, fmKeys.Back) {
				parentDir := filepath.Dir(m.dir)
				if parentDir != m.dir {
					m.loadDirAndSelectChild(parentDir, filepath.Base(m.dir))
				}
				return m, nil
			} else if key.Matches(msg, fmKeys.Dir) {
				if i, ok := m.list.SelectedItem().(item); ok {
					if i.name == ".." {
						parentDir := filepath.Dir(m.dir)
						if parentDir != m.dir {
							m.loadDirAndSelectChild(parentDir, filepath.Base(m.dir))
						}
					} else if i.isDir {
						m.loadDir(filepath.Join(m.dir, i.name))
					}
				}
				return m, nil
			} else if key.Matches(msg, fmKeys.Open) {
				if i, ok := m.list.SelectedItem().(item); ok {
					if i.name == ".." {
						return m, func() tea.Msg { return msgSwitchToDashboard{dir: m.dir} }
					} else if i.isDir {
						selectedDir := filepath.Join(m.dir, i.name)
						return m, func() tea.Msg { return msgSwitchToDashboard{dir: selectedDir} }
					} else {
						return m, func() tea.Msg { return msgSwitchToDashboard{dir: m.dir} }
					}
				}
			}
		}
	}

	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m *FileManagerModel) View() string {
	var lines []string

	if m.list.SettingFilter() || m.list.FilterValue() != "" {
		lines = append(lines, m.list.FilterInput.View(), "")
	}

	items := m.list.VisibleItems()
	n := len(items)

	availHeight := m.list.Height()
	if m.list.SettingFilter() || m.list.FilterValue() != "" {
		availHeight -= 2
	}
	availHeight -= 2 // reserved for help bar & spacing
	if availHeight < 1 {
		availHeight = 1
	}

	sel := m.list.Index()

	if sel < m.topOffset {
		m.topOffset = sel
	}
	if sel >= m.topOffset+availHeight {
		m.topOffset = sel - availHeight + 1
	}
	if m.topOffset > n-availHeight {
		m.topOffset = n - availHeight
	}
	if m.topOffset < 0 {
		m.topOffset = 0
	}

	if n == 0 {
		lines = append(lines, "  No files found.")
	} else {
		for i := m.topOffset; i < m.topOffset+availHeight && i < n; i++ {
			it, ok := items[i].(item)
			if !ok {
				continue
			}

			var icon string
			if it.isDir {
				icon = "📁 "
			} else {
				icon = "📄 "
			}

			text := icon + it.name

			var itemColor lipgloss.Color
			if it.isM4B || it.hasM4B {
				itemColor = successColor
			} else {
				itemColor = textColor
			}

			if i == sel {
				line := lipgloss.NewStyle().Foreground(primaryColor).Bold(true).Render("> ") +
					lipgloss.NewStyle().Foreground(itemColor).Bold(true).Render(text)
				lines = append(lines, line)
			} else {
				line := lipgloss.NewStyle().Foreground(itemColor).Render("  " + text)
				lines = append(lines, line)
			}
		}
	}

	for len(lines) < availHeight {
		lines = append(lines, "")
	}

	lines = append(lines, "", m.list.Help.View(m.list))
	return strings.Join(lines, "\n")
}
