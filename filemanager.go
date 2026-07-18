package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type FileManagerModel struct {
	dir     string
	entries []os.DirEntry
	cursor  int
	offset  int
}

func newFileManagerModel(dir string) *FileManagerModel {
	m := &FileManagerModel{
		dir: dir,
	}
	m.loadDir(dir)
	return m
}

func (m *FileManagerModel) loadDir(dir string) {
	m.dir = filepath.Clean(dir)
	entries, err := os.ReadDir(m.dir)
	if err == nil {
		m.entries = []os.DirEntry{}
		for _, e := range entries {
			// skip hidden files
			if !strings.HasPrefix(e.Name(), ".") {
				m.entries = append(m.entries, e)
			}
		}
	}
	m.cursor = 0
	m.offset = 0
}

func (m *FileManagerModel) loadDirAndSelectChild(dir string, childName string) {
	m.loadDir(dir)
	for i, e := range m.entries {
		if e.Name() == childName {
			m.cursor = i + 1
			if m.cursor >= 20 {
				m.offset = m.cursor - 19
			}
			break
		}
	}
}

func (m *FileManagerModel) Init() tea.Cmd {
	return nil
}

func (m *FileManagerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
			if m.cursor < m.offset {
				m.offset = m.cursor
			}
		case "down", "j":
			if m.cursor < len(m.entries) { // +1 for ".."
				m.cursor++
			}
			if m.cursor >= m.offset+20 {
				m.offset = m.cursor - 19
			}
		case "backspace", "left", "h":
			parentDir := filepath.Dir(m.dir)
			if parentDir != m.dir {
				m.loadDirAndSelectChild(parentDir, filepath.Base(m.dir))
			}
		case "enter", "right", "l":
			if m.cursor == 0 {
				parentDir := filepath.Dir(m.dir)
				if parentDir != m.dir {
					m.loadDirAndSelectChild(parentDir, filepath.Base(m.dir))
				}
			} else {
				entry := m.entries[m.cursor-1]
				if entry.IsDir() {
					m.loadDir(filepath.Join(m.dir, entry.Name()))
				}
			}
		case "o", "space": // Open selected dir
			if m.cursor == 0 {
				return m, func() tea.Msg { return msgSwitchToDashboard{dir: m.dir} }
			} else {
				entry := m.entries[m.cursor-1]
				if entry.IsDir() {
					selectedDir := filepath.Join(m.dir, entry.Name())
					return m, func() tea.Msg { return msgSwitchToDashboard{dir: selectedDir} }
				} else {
					return m, func() tea.Msg { return msgSwitchToDashboard{dir: m.dir} }
				}
			}
		}
	}
	return m, nil
}

func (m *FileManagerModel) View() string {
	b := &strings.Builder{}

	fmt.Fprintf(b, "%s\n\n", titleStyle.Render("P A G E T U R N E R  -  F i l e   M a n a g e r"))
	fmt.Fprintf(b, "Current Directory: %s\n\n", m.dir)

	end := m.offset + 20
	if end > len(m.entries)+1 {
		end = len(m.entries) + 1
	}

	for i := m.offset; i < end; i++ {
		if i == 0 {
			cursorStr := "  "
			if m.cursor == 0 {
				cursorStr = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render("> ")
			}
			fmt.Fprintf(b, "%s .. (Up)\n", cursorStr)
			continue
		}

		e := m.entries[i-1]
		cursorStr := "  "
		if m.cursor == i {
			cursorStr = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render("> ")
		}

		icon := "📄"
		if e.IsDir() {
			icon = "📁"
		}

		fmt.Fprintf(b, "%s %s %s\n", cursorStr, icon, e.Name())
	}
	
	// Pad the rest of the 20 slots if we have fewer items
	renderedCount := end - m.offset
	for i := renderedCount; i < 20; i++ {
		fmt.Fprintf(b, "\n")
	}
	
	if len(m.entries)+1 > m.offset+20 {
		fmt.Fprintf(b, "   ... and %d more items\n", len(m.entries)+1-(m.offset+20))
	} else {
		fmt.Fprintf(b, "\n")
	}

	fmt.Fprintf(b, "\n%s\n", helpStyle.Render("Arrows/hjkl: Navigate • Enter/Right: Enter Dir • Backspace/Left: Parent Dir • Space: Select this directory • q: Quit"))

	return b.String()
}
