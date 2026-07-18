package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type DashboardModel struct {
	dir string

	// Left column
	files          []string
	bitrate        int
	removeSource   bool

	// Right column - text inputs
	inputs []textinput.Model

	// Right column - cover selection
	covers     []string
	coverIndex int

	// UI state
	focusIndex int
}

const (
	dashInputArtist = iota
	dashInputAlbum
	dashInputTitle
	dashInputOutFilename
	dashCoverSelection
	dashRemoveSourceToggle
	dashStartButton
)

func newDashboardModel(dir string) *DashboardModel {
	m := &DashboardModel{
		dir:    dir,
		inputs: make([]textinput.Model, 4),
	}

	for i := range m.inputs {
		t := textinput.New()
		t.Cursor.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
		t.CharLimit = 128

		switch i {
		case dashInputArtist:
			t.Placeholder = "Artist"
		case dashInputAlbum:
			t.Placeholder = "Album"
		case dashInputTitle:
			t.Placeholder = "Title"
		case dashInputOutFilename:
			t.Placeholder = "Output Filename"
		}
		m.inputs[i] = t
	}

	m.inputs[dashInputArtist].Focus()
	m.scanDirectory()
	return m
}

func (m *DashboardModel) scanDirectory() {
	m.files = listFilesByExt(m.dir, ".mp3")
	
	if len(m.files) > 0 {
		artist, album, title := "", "", ""
		bb, err := getMetaJsonBytes(m.dir, m.files[0])
		if err == nil {
			var fileMeta container
			if err := json.Unmarshal(bb.Bytes(), &fileMeta); err == nil {
				artist = fileMeta.Format.Tags.Artist
				album = fileMeta.Format.Tags.Album
				title = fileMeta.Format.Tags.Title
			}
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
		
		outName := ""
		if artist != "" && album != "" {
			outName = fmt.Sprintf("%s - %s.m4b", artist, album)
		} else if album != "" {
			outName = fmt.Sprintf("%s.m4b", album)
		} else {
			outName = filepath.Base(m.dir) + ".m4b"
		}
		m.inputs[dashInputOutFilename].SetValue(strings.ReplaceAll(outName, string(filepath.Separator), "_"))
	}

	m.covers = []string{"Default Cover"}
	// Extracted cover if possible
	m.covers = append(m.covers, "Extract from MP3")
	
	// Add other jpgs in the directory
	dirContent, _ := os.ReadDir(m.dir)
	for _, file := range dirContent {
		if !file.IsDir() && isSupportedImageFormatFile(strings.ToLower(file.Name())) {
			m.covers = append(m.covers, file.Name())
		}
	}
}

func (m *DashboardModel) getConfig() ConversionConfig {
	outFn := m.inputs[dashInputOutFilename].Value()
	if outFn == "" {
		outFn = "book.m4b"
	}
	if !strings.HasSuffix(outFn, ".m4b") {
		outFn += ".m4b"
	}
	
	coverSrc := m.covers[m.coverIndex]
	var coverPath string
	if coverSrc != "Default Cover" && coverSrc != "Extract from MP3" {
		coverPath = filepath.Join(m.dir, coverSrc)
	}

	return ConversionConfig{
		TargetDir:    m.dir,
		Artist:       m.inputs[dashInputArtist].Value(),
		Album:        m.inputs[dashInputAlbum].Value(),
		Title:        m.inputs[dashInputTitle].Value(),
		OutFilename:  outFn,
		CoverSource:  m.coverIndex,
		CoverPath:    coverPath,
		RemoveSource: m.removeSource,
	}
}

func (m *DashboardModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m *DashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "tab", "shift+tab", "up", "down":
			s := msg.String()
			
			// Adjust focus
			if s == "up" || s == "shift+tab" {
				m.focusIndex--
			} else {
				m.focusIndex++
			}
			
			if m.focusIndex > dashStartButton {
				m.focusIndex = 0
			} else if m.focusIndex < 0 {
				m.focusIndex = dashStartButton
			}
			
			cmds := make([]tea.Cmd, len(m.inputs))
			for i := 0; i <= dashInputOutFilename; i++ {
				if i == m.focusIndex {
					cmds[i] = m.inputs[i].Focus()
				} else {
					m.inputs[i].Blur()
				}
			}
			return m, tea.Batch(cmds...)
			
		case "enter":
			if m.focusIndex == dashStartButton {
				return m, func() tea.Msg { return msgStartConversion{config: m.getConfig()} }
			}
		
		case " ":
			if m.focusIndex == dashRemoveSourceToggle {
				m.removeSource = !m.removeSource
			}
			
		case "left", "right":
			if m.focusIndex == dashCoverSelection {
				if msg.String() == "left" {
					m.coverIndex--
					if m.coverIndex < 0 {
						m.coverIndex = len(m.covers) - 1
					}
				} else {
					m.coverIndex++
					if m.coverIndex >= len(m.covers) {
						m.coverIndex = 0
					}
				}
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
	b := &strings.Builder{}

	title := titleStyle.Render("P A G E T U R N E R  -  A u d i o b o o k   C o n v e r t e r")
	fmt.Fprintf(b, "%s\n\n", title)
	
	// Split layout conceptually
	var left, right strings.Builder
	
	// Left: Discovered files
	fmt.Fprintf(&left, lipgloss.NewStyle().Bold(true).Render("DISCOVERED MP3 FILES") + "\n\n")
	displayFiles := m.files
	if len(displayFiles) > 10 {
		for i := 0; i < 9; i++ {
			fmt.Fprintf(&left, "%d. %s\n", i+1, displayFiles[i])
		}
		fmt.Fprintf(&left, "... and %d more\n", len(displayFiles)-9)
	} else if len(displayFiles) == 0 {
		fmt.Fprintf(&left, "No MP3 files found.\n")
	} else {
		for i, f := range displayFiles {
			fmt.Fprintf(&left, "%d. %s\n", i+1, f)
		}
	}
	
	fmt.Fprintf(&left, "\nDetected Bitrate: Unknown\n")
	
	toggleStr := "[ ]"
	if m.removeSource {
		toggleStr = "[x]"
	}
	if m.focusIndex == dashRemoveSourceToggle {
		toggleStr = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render(">" + toggleStr)
	}
	fmt.Fprintf(&left, "%s Remove source files after conversion\n", toggleStr)
	
	// Right: Metadata
	fmt.Fprintf(&right, lipgloss.NewStyle().Bold(true).Render("METADATA & CONFIGURATION") + "\n\n")
	for i := 0; i <= dashInputOutFilename; i++ {
		prefix := "  "
		if m.focusIndex == i {
			prefix = "> "
		}
		label := []string{"Artist:  ", "Album:   ", "Title:   ", "Out M4B: "}[i]
		fmt.Fprintf(&right, "%s%s%s\n", prefix, label, m.inputs[i].View())
	}
	
	fmt.Fprintf(&right, "\n%s\n\n", lipgloss.NewStyle().Bold(true).Render("COVER ART SOURCE"))
	for i, c := range m.covers {
		prefix := "  [ ] "
		if i == m.coverIndex {
			prefix = "  [x] "
		}
		if m.focusIndex == dashCoverSelection && i == m.coverIndex {
			prefix = "> [x] "
			prefix = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render(prefix)
		}
		fmt.Fprintf(&right, "%s%d. %s\n", prefix, i+1, c)
	}
	
	// Render columns side by side
	leftStr := lipgloss.NewStyle().Width(50).PaddingRight(4).Render(left.String())
	rightStr := right.String()
	
	fmt.Fprintf(b, "%s\n\n", lipgloss.JoinHorizontal(lipgloss.Top, leftStr, rightStr))
	
	startBtn := "[ START CONVERSION (Enter) ]"
	if m.focusIndex == dashStartButton {
		startBtn = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("6")).Render(startBtn)
	}
	fmt.Fprintf(b, "%s\n", startBtn)
	
	fmt.Fprintf(b, "\n%s\n", helpStyle.Render("Tab/Shift+Tab: Navigate • Arrows: Select • Space: Toggle • Enter: Confirm • q/Ctrl+C: Quit"))

	return b.String()
}
