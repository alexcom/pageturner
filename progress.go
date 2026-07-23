package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var progKeys = struct {
	ToggleLogs key.Binding
	Quit       key.Binding
	Back       key.Binding
	Theme      key.Binding
}{
	ToggleLogs: key.NewBinding(
		key.WithKeys("l", "L"),
		key.WithHelp("l", "toggle logs"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c"),
		key.WithHelp("q", "quit"),
	),
	Back: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "back"),
	),
	Theme: key.NewBinding(
		key.WithKeys("t", "T"),
		key.WithHelp("t", "theme"),
	),
}

type progressKeyMap struct{}

func (k progressKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{progKeys.ToggleLogs, progKeys.Theme, progKeys.Back, progKeys.Quit}
}
func (k progressKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.ShortHelp()}
}

var progHelpKeys = progressKeyMap{}

type msgLog struct {
	text string
}

type msgWorkerUpdate struct {
	workerID int
	status   string
}

type msgProgress struct {
	completed int
	total     int
}

type msgStepAdvance struct {
	step int
}

type msgInitWorkers struct {
	count int
}

type ProgressModel struct {
	ctx    context.Context
	config ConversionConfig

	totalFiles     int
	convertedFiles int

	workers []string

	currentStep int

	logs         []string
	showFullLogs bool
	cancelled    bool

	updates chan tea.Msg

	progress progress.Model
	viewport viewport.Model
	help     help.Model

	panelWidth int
}

func (m *ProgressModel) updateThemeStyles() {
	m.viewport.Style = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(secondaryColor).
		PaddingRight(2)
	applyHelpStyles(&m.help.Styles)
}

func newProgressModel(ctx context.Context, dir string, config ConversionConfig) *ProgressModel {
	files := listFilesByExt(dir, ".mp3")
	
	prog := progress.New(progress.WithDefaultGradient())
	prog.Width = 48
	vp := viewport.New(80, 20)
	vp.Style = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(secondaryColor).
		PaddingRight(2)
		
	m := &ProgressModel{
		ctx:        ctx,
		config:     config,
		totalFiles: len(files),
		workers:    []string{},
		logs:       []string{"Starting conversion..."},
		updates:    make(chan tea.Msg),
		progress:   prog,
		viewport:   vp,
		help:       newHelpModel(),
		panelWidth: 80,
	}

	m.viewport.SetContent(strings.Join(m.logs, "\n"))
	return m
}

func waitForUpdate(c chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return <-c
	}
}

func (m *ProgressModel) Init() tea.Cmd {
	go runConversionPipeline(m.ctx, m.config, m.updates)
	return waitForUpdate(m.updates)
}

func (m *ProgressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h, v := docStyle.GetFrameSize()
		m.panelWidth = msg.Width - h - 2
		if m.panelWidth < 40 {
			m.panelWidth = 40
		}
		m.progress.Width = m.panelWidth - 32
		if m.progress.Width < 10 {
			m.progress.Width = 10
		}
		
		titleHeight := 6 // 3 for header box + 1 for dirView + 2 spacing
		m.viewport.Width = msg.Width - h - 2
		m.viewport.Height = msg.Height - v - titleHeight - 4
		m.help.Width = msg.Width
		
		var vpCmd tea.Cmd
		m.viewport, vpCmd = m.viewport.Update(msg)
		cmds = append(cmds, vpCmd)
		
		return m, tea.Batch(cmds...)

	case tea.KeyMsg:
		if key.Matches(msg, progKeys.ToggleLogs) {
			m.showFullLogs = !m.showFullLogs
			if m.showFullLogs {
				m.viewport.GotoBottom()
			}
		}
		if m.showFullLogs {
			var vpCmd tea.Cmd
			m.viewport, vpCmd = m.viewport.Update(msg)
			cmds = append(cmds, vpCmd)
		}
		return m, tea.Batch(cmds...)
		
	case progress.FrameMsg:
		progressModel, cmd := m.progress.Update(msg)
		m.progress = progressModel.(progress.Model)
		return m, cmd

	case msgLog:
		m.logs = append(m.logs, msg.text)
		if len(m.logs) > 1000 {
			m.logs = m.logs[len(m.logs)-1000:]
		}
		m.viewport.SetContent(strings.Join(m.logs, "\n"))
		if m.viewport.AtBottom() {
			m.viewport.GotoBottom()
		}
		return m, waitForUpdate(m.updates)

	case msgWorkerUpdate:
		for len(m.workers) <= msg.workerID {
			m.workers = append(m.workers, "Idle")
		}
		m.workers[msg.workerID] = msg.status
		return m, waitForUpdate(m.updates)

	case msgProgress:
		m.convertedFiles = msg.completed
		m.totalFiles = msg.total
		pct := 0.0
		if m.totalFiles > 0 {
			pct = float64(m.convertedFiles) / float64(m.totalFiles)
		}
		return m, tea.Batch(waitForUpdate(m.updates), m.progress.SetPercent(pct))

	case msgStepAdvance:
		m.currentStep = msg.step
		return m, waitForUpdate(m.updates)

	case msgInitWorkers:
		m.workers = make([]string, msg.count)
		for i := 0; i < msg.count; i++ {
			m.workers[i] = "Starting..."
		}
		return m, waitForUpdate(m.updates)

	case msgError:
		return m, nil
	case msgConversionDone:
		m.currentStep = 6
		return m, nil
	case msgCancelled:
		m.cancelled = true
		return m, nil
	}
	
	if m.showFullLogs {
		var vpCmd tea.Cmd
		m.viewport, vpCmd = m.viewport.Update(msg)
		cmds = append(cmds, vpCmd)
	}
	return m, tea.Batch(cmds...)
}

func (m *ProgressModel) View() string {
	if m.showFullLogs {
		return lipgloss.JoinVertical(lipgloss.Left, m.viewport.View(), "", m.help.View(progHelpKeys))
	}

	statusText := lipgloss.NewStyle().Foreground(warningColor).Render("Working...")
	if m.cancelled {
		statusText = lipgloss.NewStyle().Foreground(errorColor).Render("Cancelled")
	} else if m.currentStep >= 6 {
		statusText = lipgloss.NewStyle().Foreground(successColor).Render("Complete")
	}

	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(primaryColor).
		Padding(0, 1)

	progressStr := panelStyle.Width(m.panelWidth).Render(lipgloss.JoinHorizontal(lipgloss.Left, 
		m.progress.View(),
		fmt.Sprintf("  (%d/%d Files) - %s", m.convertedFiles, m.totalFiles, statusText),
	))

	// Left: Workers
	var workers []string
	workers = append(workers, lipgloss.NewStyle().Bold(true).Render("ACTIVE CONVERSION WORKERS"), "")
	for _, w := range m.workers {
		status := w
		if len([]rune(status)) > 40 {
			status = string([]rune(status)[:37]) + "..."
		}
		if status == "Idle" {
			workers = append(workers, "  "+lipgloss.NewStyle().Foreground(subtextColor).Render("Idle"))
		} else {
			workers = append(workers, "  "+status)
		}
	}
	leftWidth := (m.panelWidth - 4) / 2
	if leftWidth < 10 {
		leftWidth = 10
	}
	leftStr := panelStyle.Width(leftWidth).Render(lipgloss.JoinVertical(lipgloss.Left, workers...))
	
	// Right: Checklist
	var checklist []string
	checklist = append(checklist, lipgloss.NewStyle().Bold(true).Render("PIPELINE CHECKLIST"), "")
	steps := []string{
		"Check Prerequisites & Scan Directory",
		"Convert MP3s to M4A (Parallel)",
		"Resolve Cover Art",
		"Generate Metadata (FFMETA)",
		"Merge into M4B",
		"Clean up temporary files",
	}
	
	for i, s := range steps {
		status := "[ ]"
		if m.cancelled && i == m.currentStep {
			status = lipgloss.NewStyle().Foreground(errorColor).Render("[✗]")
		} else if i < m.currentStep {
			status = lipgloss.NewStyle().Foreground(successColor).Render("[✔]")
		} else if i == m.currentStep {
			status = lipgloss.NewStyle().Foreground(warningColor).Render("[❯]")
		}
		checklist = append(checklist, fmt.Sprintf("%s %s", status, s))
	}
	rightWidth := m.panelWidth - 4 - leftWidth
	if rightWidth < 10 {
		rightWidth = 10
	}
	rightStr := panelStyle.Width(rightWidth).Render(lipgloss.JoinVertical(lipgloss.Left, checklist...))
	
	// Render columns side by side
	split := lipgloss.JoinHorizontal(lipgloss.Top, leftStr, lipgloss.NewStyle().Width(2).Render(""), rightStr)
	
	// Bottom: Logs
	var logLines []string
	logLines = append(logLines, lipgloss.NewStyle().Bold(true).Render("CONVERSION LOGS (Press 'l' to expand)"), "")
	start := 0
	if len(m.logs) > 5 {
		start = len(m.logs) - 5
	}
	for i := start; i < len(m.logs); i++ {
		logLines = append(logLines, m.logs[i])
	}
	logsStr := panelStyle.Width(m.panelWidth).Render(lipgloss.JoinVertical(lipgloss.Left, logLines...))

	return lipgloss.JoinVertical(lipgloss.Left,
		progressStr,
		"",
		split,
		"",
		logsStr,
		"",
		m.help.View(progHelpKeys),
	)
}
