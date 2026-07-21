package main

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
)

type item struct {
	name  string
	isDir bool
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
	Open key.Binding
	Back key.Binding
	Dir  key.Binding
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
}

type FileManagerModel struct {
	dir  string
	list list.Model
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
	m.list.SetFilteringEnabled(true)
	m.list.SetShowTitle(false)
	applyHelpStyles(&m.list.Help.Styles)

	m.list.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{fmKeys.Open, fmKeys.Back}
	}
	m.list.AdditionalFullHelpKeys = m.list.AdditionalShortHelpKeys

	m.loadDir(dir)
	return m
}

func (m *FileManagerModel) loadDir(dir string) {
	m.dir = filepath.Clean(dir)
	entries, err := os.ReadDir(m.dir)
	var items []list.Item

	// List title is managed by the main UI

	// Always add parent
	items = append(items, item{name: "..", isDir: true})

	if err == nil {
		for _, e := range entries {
			// skip hidden files
			if !strings.HasPrefix(e.Name(), ".") {
				items = append(items, item{name: e.Name(), isDir: e.IsDir()})
			}
		}
	}
	m.list.SetItems(items)
	m.list.ResetSelected()
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
	return m.list.View()
}
