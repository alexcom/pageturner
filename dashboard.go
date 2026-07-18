package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var dashKeys = struct {
	Quit    key.Binding
	NavUp   key.Binding
	NavDown key.Binding
	Confirm key.Binding
	Toggle  key.Binding
	Left    key.Binding
	Right   key.Binding
}{
	Quit: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "back"),
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
	Left: key.NewBinding(
		key.WithKeys("left"),
		key.WithHelp("←", "left"),
	),
	Right: key.NewBinding(
		key.WithKeys("right"),
		key.WithHelp("→", "right"),
	),
}

type DashboardModel struct {
	dir string

	// Left column
	files          []string
	fileOffset     int
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
	dashFileList = iota
	dashInputArtist
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
		inputs: make([]textinput.Model, 5),
	}

	for i := range m.inputs {
		if i == dashFileList {
			continue
		}
		
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
	if len(m.files) > 0 && hasCoverImage(m.dir, m.files[0]) {
		m.covers = append(m.covers, "Extract from MP3")
	}
	
	// Add other jpgs in the directory
	dirContent, _ := os.ReadDir(m.dir)
	for _, file := range dirContent {
		if !file.IsDir() && isSupportedImageFormatFile(strings.ToLower(file.Name())) {
			m.covers = append(m.covers, file.Name())
		}
	}
}

func hasCoverImage(dir, filename string) bool {
	cmd := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "stream=codec_type", "-of", "default=nw=1:nk=1", filepath.Join(dir, filename))
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(out)) == "video"
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
		if key.Matches(msg, dashKeys.Quit) {
			return m, func() tea.Msg { return msgSwitchToFileManager{} }
		} else if key.Matches(msg, dashKeys.NavUp, dashKeys.NavDown) {
			isUp := key.Matches(msg, dashKeys.NavUp)
			
			if m.focusIndex == dashFileList && (msg.String() == "up" || msg.String() == "down") {
				if isUp {
					if m.fileOffset > 0 {
						m.fileOffset--
						return m, nil
					}
				} else {
					if m.fileOffset < len(m.files)-10 {
						m.fileOffset++
						return m, nil
					}
				}
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
			return m, func() tea.Msg { return msgStartConversion{config: m.getConfig()} }
		} else if key.Matches(msg, dashKeys.Toggle) {
			if m.focusIndex == dashRemoveSourceToggle {
				m.removeSource = !m.removeSource
			}
		} else if key.Matches(msg, dashKeys.Left, dashKeys.Right) {
			if m.focusIndex == dashCoverSelection {
				if key.Matches(msg, dashKeys.Left) {
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
		if i == dashFileList { continue }
		if m.focusIndex == i {
			var cmd tea.Cmd
			m.inputs[i], cmd = m.inputs[i].Update(msg)
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

func (m *DashboardModel) View() string {
	title := titleStyle.Render("P A G E T U R N E R  -  A u d i o b o o k   C o n v e r t e r")
	
	// Left: Discovered files
	fileListTitle := "DISCOVERED MP3 FILES"
	if m.focusIndex == dashFileList {
		fileListTitle = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render("> " + fileListTitle)
	} else {
		fileListTitle = "  " + fileListTitle
	}
	
	var left []string
	left = append(left, lipgloss.NewStyle().Bold(true).Render(fileListTitle), "")
	
	displayFiles := m.files
	start := m.fileOffset
	end := start + 10
	if end > len(displayFiles) {
		end = len(displayFiles)
	}

	if len(displayFiles) > 10 {
		for i := start; i < end; i++ {
			prefix := "  "
			if m.focusIndex == dashFileList {
				prefix = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("┃ ")
			}
			fname := displayFiles[i]
			if len([]rune(fname)) > 38 {
				fname = string([]rune(fname)[:35]) + "..."
			}
			left = append(left, prefix+fname)
		}
		left = append(left, fmt.Sprintf("   ... %d of %d ", end, len(displayFiles)))
	} else if len(displayFiles) == 0 {
		left = append(left, "  No MP3 files found.")
		for i := 0; i < 10; i++ { left = append(left, "") }
	} else {
		for _, f := range displayFiles {
			fname := f
			if len([]rune(fname)) > 38 {
				fname = string([]rune(fname)[:35]) + "..."
			}
			left = append(left, "  "+fname)
		}
		for i := len(displayFiles); i < 10; i++ { left = append(left, "") }
		left = append(left, "")
	}
	
	left = append(left, "", "Detected Bitrate: Unknown")
	
	toggleStr := "[ ]"
	if m.removeSource {
		toggleStr = "[x]"
	}
	if m.focusIndex == dashRemoveSourceToggle {
		toggleStr = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render(">" + toggleStr)
	}
	left = append(left, fmt.Sprintf("%s Remove source files after conversion", toggleStr))
	
	// Right: Metadata
	var right []string
	right = append(right, lipgloss.NewStyle().Bold(true).Render("METADATA & CONFIGURATION"), "")
	for i := dashInputArtist; i <= dashInputOutFilename; i++ {
		prefix := "  "
		if m.focusIndex == i {
			prefix = "> "
		}
		label := []string{"", "Artist:  ", "Album:   ", "Title:   ", "Out M4B: "}[i]
		right = append(right, prefix+label+m.inputs[i].View())
	}
	
	right = append(right, "", lipgloss.NewStyle().Bold(true).Render("COVER ART SOURCE"), "")
	for i, c := range m.covers {
		prefix := "  [ ] "
		if i == m.coverIndex {
			prefix = "  [x] "
		}
		if m.focusIndex == dashCoverSelection && i == m.coverIndex {
			prefix = "> [x] "
			prefix = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render(prefix)
		}
		right = append(right, fmt.Sprintf("%s%d. %s", prefix, i+1, c))
	}
	
	// Render columns side by side
	leftStr := lipgloss.NewStyle().Width(50).PaddingRight(4).Render(lipgloss.JoinVertical(lipgloss.Left, left...))
	rightStr := lipgloss.JoinVertical(lipgloss.Left, right...)
	
	split := lipgloss.JoinHorizontal(lipgloss.Top, leftStr, rightStr)
	
	startBtn := "[ START CONVERSION (Enter) ]"
	if m.focusIndex == dashStartButton {
		startBtn = lipgloss.NewStyle().Foreground(lipgloss.Color("0")).Background(lipgloss.Color("6")).Render(startBtn)
	}
	
	help := helpStyle.Render("Tab/Shift+Tab: Navigate • Arrows: Select • Space: Toggle • Enter: Confirm • q/Ctrl+C: Quit")

	return lipgloss.JoinVertical(lipgloss.Left,
		title,
		"",
		split,
		"",
		startBtn,
		"",
		help,
	)
}
