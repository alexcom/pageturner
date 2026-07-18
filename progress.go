package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

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
	
	logs []string
	showFullLogs bool

	updates chan tea.Msg
}

func newProgressModel(dir string, config ConversionConfig) *ProgressModel {
	m := &ProgressModel{
		config: config,
		workers: []string{},
		logs:   []string{"Starting conversion..."},
		updates: make(chan tea.Msg),
	}
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
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "l" || msg.String() == "L" {
			m.showFullLogs = !m.showFullLogs
		}
		return m, nil
	case msgLog:
		m.logs = append(m.logs, msg.text)
		if len(m.logs) > 1000 {
			m.logs = m.logs[len(m.logs)-1000:]
		}
		return m, waitForUpdate(m.updates)
	case msgWorkerUpdate:
		if msg.workerID >= 0 && msg.workerID < len(m.workers) {
			m.workers[msg.workerID] = msg.status
		}
		return m, waitForUpdate(m.updates)
	case msgInitWorkers:
		m.workers = make([]string, msg.count)
		for i := 0; i < msg.count; i++ {
			m.workers[i] = "Idle"
		}
		return m, waitForUpdate(m.updates)
	case msgProgress:
		m.convertedFiles = msg.completed
		m.totalFiles = msg.total
		return m, waitForUpdate(m.updates)
	case msgStepAdvance:
		m.currentStep = msg.step
		return m, waitForUpdate(m.updates)
	case msgError:
		return m, func() tea.Msg { return msg }
	case msgConversionDone:
		m.currentStep = 6
		return m, func() tea.Msg { return msg }
	}
	return m, nil
}

func (m *ProgressModel) View() string {
	b := &strings.Builder{}

	if m.showFullLogs {
		fmt.Fprintf(b, "%s\n\n", titleStyle.Render("C O N V E R T I N G   A U D I O B O O K   -   L O G S"))
		for _, l := range m.logs {
			fmt.Fprintf(b, "%s\n", l)
		}
		fmt.Fprintf(b, "\n%s\n", helpStyle.Render("Press 'l' to return to dashboard"))
		return b.String()
	}

	fmt.Fprintf(b, "%s\n\n", titleStyle.Render("C O N V E R T I N G   A U D I O B O O K"))
	
	// Progress Bar
	pct := 0.0
	if m.totalFiles > 0 {
		pct = float64(m.convertedFiles) / float64(m.totalFiles)
	}
	
	barWidth := 40
	filled := int(float64(barWidth) * pct)
	bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	
	fmt.Fprintf(b, "Progress: [%s] %d%%  (%d/%d Files)\n\n", 
		lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Render(bar),
		int(pct*100), m.convertedFiles, m.totalFiles)
		
	// Split Layout
	var left, right strings.Builder
	
	// Left: Workers
	fmt.Fprintf(&left, lipgloss.NewStyle().Bold(true).Render("ACTIVE CONVERSION WORKERS") + "\n\n")
	for _, w := range m.workers {
		status := w
		if len([]rune(status)) > 45 {
			status = string([]rune(status)[:42]) + "..."
		}
		if status == "Idle" {
			fmt.Fprintf(&left, "  %s\n", lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("Idle"))
		} else {
			fmt.Fprintf(&left, "  %s\n", status)
		}
	}
	
	// Right: Checklist
	fmt.Fprintf(&right, lipgloss.NewStyle().Bold(true).Render("PIPELINE CHECKLIST") + "\n\n")
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
		fmt.Fprintf(&right, "%s %s\n", status, s)
	}
	
	leftStr := lipgloss.NewStyle().Width(50).PaddingRight(4).Render(left.String())
	fmt.Fprintf(b, "%s\n\n", lipgloss.JoinHorizontal(lipgloss.Top, leftStr, right.String()))
	
	// Bottom: Logs
	fmt.Fprintf(b, "%s\n", lipgloss.NewStyle().Bold(true).Render("CONVERSION LOGS (Press 'l' to expand)"))
	start := 0
	if len(m.logs) > 5 {
		start = len(m.logs) - 5
	}
	for i := start; i < len(m.logs); i++ {
		fmt.Fprintf(b, "%s\n", m.logs[i])
	}

	return b.String()
}
