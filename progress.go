package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var progKeys = struct {
	ToggleLogs key.Binding
}{
	ToggleLogs: key.NewBinding(
		key.WithKeys("l", "L"),
		key.WithHelp("l", "toggle logs"),
	),
}

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
	config ConversionConfig

	totalFiles     int
	convertedFiles int

	workers []string

	currentStep int

	logs         []string
	showFullLogs bool

	updates chan tea.Msg

	progress progress.Model
	viewport viewport.Model
}

func newProgressModel(dir string, config ConversionConfig) *ProgressModel {
	files := listFilesByExt(getWd(), ".mp3")
	
	prog := progress.New(progress.WithDefaultGradient())
	vp := viewport.New(80, 20)
	vp.Style = lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		PaddingRight(2)
		
	m := &ProgressModel{
		config:     config,
		totalFiles: len(files),
		workers:    []string{},
		logs:       []string{"Starting conversion..."},
		updates:    make(chan tea.Msg),
		progress:   prog,
		viewport:   vp,
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
	go runConversionPipeline(m.config, m.updates)
	return waitForUpdate(m.updates)
}

func (m *ProgressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.progress.Width = msg.Width - 4
		if m.progress.Width > 80 {
			m.progress.Width = 80
		}
		
		m.viewport.Width = msg.Width - 2
		m.viewport.Height = msg.Height - 6
		
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
		return m, func() tea.Msg { return msg }
	case msgConversionDone:
		m.currentStep = 6
		return m, func() tea.Msg { return msg }
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
		title := titleStyle.Render("C O N V E R T I N G   A U D I O B O O K   -   L O G S")
		help := helpStyle.Render("Press 'l' to return to dashboard • Arrows/Scroll to navigate logs")
		return lipgloss.JoinVertical(lipgloss.Left, title, "", m.viewport.View(), "", help)
	}

	title := titleStyle.Render("C O N V E R T I N G   A U D I O B O O K")
	
	progressStr := lipgloss.JoinHorizontal(lipgloss.Left, 
		m.progress.View(),
		fmt.Sprintf("  (%d/%d Files)", m.convertedFiles, m.totalFiles),
	)
		
	// Left: Workers
	var workers []string
	workers = append(workers, lipgloss.NewStyle().Bold(true).Render("ACTIVE CONVERSION WORKERS"), "")
	for _, w := range m.workers {
		status := w
		if len([]rune(status)) > 45 {
			status = string([]rune(status)[:42]) + "..."
		}
		if status == "Idle" {
			workers = append(workers, "  "+lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("Idle"))
		} else {
			workers = append(workers, "  "+status)
		}
	}
	leftStr := lipgloss.NewStyle().Width(50).PaddingRight(5).Render(lipgloss.JoinVertical(lipgloss.Left, workers...))
	
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
		if i < m.currentStep {
			status = lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render("[✔]")
		} else if i == m.currentStep {
			status = lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render("[❯]")
		}
		checklist = append(checklist, fmt.Sprintf("%s %s", status, s))
	}
	rightStr := lipgloss.JoinVertical(lipgloss.Left, checklist...)
	
	// Render columns side by side
	split := lipgloss.JoinHorizontal(lipgloss.Top, leftStr, rightStr)
	
	// Bottom: Logs
	var logLines []string
	logLines = append(logLines, lipgloss.NewStyle().Bold(true).Render("CONVERSION LOGS (Press 'l' to expand)"))
	start := 0
	if len(m.logs) > 5 {
		start = len(m.logs) - 5
	}
	for i := start; i < len(m.logs); i++ {
		logLines = append(logLines, m.logs[i])
	}
	logsStr := lipgloss.JoinVertical(lipgloss.Left, logLines...)

	return lipgloss.JoinVertical(lipgloss.Left,
		title,
		"",
		progressStr,
		"",
		split,
		"",
		logsStr,
	)
}
