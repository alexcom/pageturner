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

// Pipeline step weights for overall progress calculation.
// Step 0: Prerequisites (2%), Step 1: Conversion (82%), Step 2: Cover (2%),
// Step 3: Metadata (2%), Step 4: Merge (10%), Step 5: Cleanup (2%).
const numPipelineSteps = 6

var stepWeights = [numPipelineSteps]float64{0.02, 0.82, 0.02, 0.02, 0.10, 0.02}

// overallProgress returns [0.0, 1.0] combining step advancement with
// fine-grained file-level progress during conversion (step 1).
func overallProgress(currentStep, convertedFiles, totalFiles int) float64 {
	pct := 0.0

	for step := 0; step < numPipelineSteps; step++ {
		if step > currentStep {
			break
		}

		w := stepWeights[step]

		if step < currentStep {
			// Step already completed.
			pct += w
		} else if step == currentStep && step == 1 && totalFiles > 0 {
			// Subdivide conversion weight among individual files.
			pct += w * (float64(convertedFiles) / float64(totalFiles))
		}
	}

	if pct > 1.0 {
		pct = 1.0
	}
	return pct
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
	err          error // set when the pipeline failed

	updates chan tea.Msg

	progress progress.Model
	viewport viewport.Model
	help     help.Model

	panelWidth int
	width      int
	height     int
}

func (m *ProgressModel) updateThemeStyles() {
	applyHelpStyles(&m.help.Styles)
}

// logBoxStyle frames the full-log viewport. The frame is drawn outside the
// viewport: bubbles' viewport shows Height lines but clips its own frame out of
// that height, which would hide the newest lines.
func logBoxStyle() lipgloss.Style {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(secondaryColor).
		PaddingRight(2)
}

func newProgressModel(ctx context.Context, dir string, config ConversionConfig) *ProgressModel {
	files := listFilesByExt(dir, ".mp3")

	// The percentage is part of the status label, so the bar doesn't print its own.
	prog := progress.New(progress.WithDefaultGradient(), progress.WithoutPercentage())
	prog.Width = 48
	vp := viewport.New(80, 20)

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

// msgUpdatesClosed means the pipeline has exited and closed its channel.
type msgUpdatesClosed struct{}

// waitForUpdate reads the next pipeline message. The model keeps re-arming it,
// even after done/error, until the channel is closed: the pipeline may still
// send (e.g. deferred cleanup warnings) and would block on an unread channel.
func waitForUpdate(c chan tea.Msg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-c
		if !ok {
			return msgUpdatesClosed{}
		}
		return msg
	}
}

// detach drains the pipeline channel in the background once the user leaves
// the progress screen, so late messages can't block the pipeline goroutine.
func (m *ProgressModel) detach() {
	ch := m.updates
	go func() {
		for range ch {
		}
	}()
}

func (m *ProgressModel) Init() tea.Cmd {
	go runConversionPipeline(m.ctx, m.config, m.updates)
	return waitForUpdate(m.updates)
}

func (m *ProgressModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// The parent UI sends the size of the content area below its chrome.
		m.width, m.height = msg.Width, msg.Height
		m.panelWidth = max(40, msg.Width-2) // panel border is 2 columns

		frameW, frameH := logBoxStyle().GetFrameSize()
		m.viewport.Width = max(1, msg.Width-frameW)
		m.viewport.Height = max(1, msg.Height-frameH-2) // blank line + help
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
		// Check before SetContent: once content grows, AtBottom is false.
		follow := m.viewport.AtBottom()
		m.viewport.SetContent(strings.Join(m.logs, "\n"))
		if follow {
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
		pct := overallProgress(m.currentStep, m.convertedFiles, m.totalFiles)
		return m, tea.Batch(waitForUpdate(m.updates), m.progress.SetPercent(pct))

	case msgStepAdvance:
		m.currentStep = msg.step
		pct := overallProgress(m.currentStep, m.convertedFiles, m.totalFiles)
		return m, tea.Batch(waitForUpdate(m.updates), m.progress.SetPercent(pct))

	case msgInitWorkers:
		m.workers = make([]string, msg.count)
		for i := 0; i < msg.count; i++ {
			m.workers[i] = "Starting..."
		}
		return m, waitForUpdate(m.updates)

	case msgError:
		m.err = msg.err
		return m, waitForUpdate(m.updates)
	case msgConversionDone:
		m.currentStep = 6
		return m, waitForUpdate(m.updates)
	case msgCancelled:
		m.cancelled = true
		return m, waitForUpdate(m.updates)
	case msgUpdatesClosed:
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
		return lipgloss.JoinVertical(lipgloss.Left, logBoxStyle().Render(m.viewport.View()), "", m.help.View(progHelpKeys))
	}

	statusText := lipgloss.NewStyle().Foreground(warningColor).Render("Working...")
	if m.err != nil {
		statusText = lipgloss.NewStyle().Foreground(errorColor).Render("Failed")
	} else if m.cancelled {
		statusText = lipgloss.NewStyle().Foreground(errorColor).Render("Cancelled")
	} else if m.currentStep >= 6 {
		statusText = lipgloss.NewStyle().Foreground(successColor).Render("Complete")
	}

	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(primaryColor).
		Padding(0, 1)

	pct := overallProgress(m.currentStep, m.convertedFiles, m.totalFiles)
	step := min(m.currentStep+1, numPipelineSteps)
	label := fmt.Sprintf("Step %d/%d", step, numPipelineSteps)
	if m.currentStep == 1 && m.totalFiles > 0 {
		label += fmt.Sprintf(" · %d/%d files", m.convertedFiles, m.totalFiles)
	}
	label += fmt.Sprintf(" · %3d%% · %s", int(pct*100), statusText)

	// Size the bar to whatever the label leaves on the line (panel padding is 2).
	inner := m.panelWidth - panelStyle.GetHorizontalPadding()
	bar := m.progress
	bar.Width = max(10, inner-lipgloss.Width(label)-2)
	progressStr := panelStyle.Width(m.panelWidth).Render(
		truncateText(bar.View()+"  "+label, inner))

	bold := lipgloss.NewStyle().Bold(true)
	leftWidth := max(10, (m.panelWidth-4)/2) // panel Width, incl. padding
	rightWidth := max(10, m.panelWidth-4-leftWidth)

	// Vertical budget. Roomy layout has blank lines between blocks and under
	// titles; compact drops them so the screen still fits small terminals.
	const helpRows, progressRows, checklistRows = 1, 3, 6
	roomy := true
	gap, titleGap := 1, 1
	splitInner := func() int { return 1 + titleGap + max(len(m.workers), checklistRows) }
	logRows := m.height - (progressRows + gap + splitInner() + 2 + gap + 2 + 1 + titleGap + gap + helpRows)
	if logRows < 3 {
		roomy, gap, titleGap = false, 0, 0
		logRows = m.height - (progressRows + splitInner() + 2 + 2 + 1 + helpRows)
	}
	workerRows := len(m.workers)
	if logRows < 1 {
		// Still too tall: show fewer workers (but keep room for the checklist).
		logRows = 1
		avail := m.height - (progressRows + 2 + 2 + 1 + logRows + helpRows) - 1 // -1 title
		workerRows = max(min(len(m.workers), max(avail, checklistRows)), 1)
	}

	// Left: workers
	leftInner := leftWidth - panelStyle.GetHorizontalPadding()
	workers := []string{bold.Render("ACTIVE CONVERSION WORKERS")}
	if roomy {
		workers = append(workers, "")
	}
	shown := m.workers
	if len(shown) > workerRows {
		shown = shown[:workerRows-1]
	}
	for _, w := range shown {
		if w == "Idle" {
			workers = append(workers, "  "+lipgloss.NewStyle().Foreground(subtextColor).Render("Idle"))
		} else {
			workers = append(workers, "  "+truncateText(w, leftInner-2))
		}
	}
	if hidden := len(m.workers) - len(shown); hidden > 0 {
		workers = append(workers, helpStyle().Render(fmt.Sprintf("  +%d more", hidden)))
	}
	leftStr := panelStyle.Width(leftWidth).Render(lipgloss.JoinVertical(lipgloss.Left, workers...))

	// Right: checklist
	rightInner := rightWidth - panelStyle.GetHorizontalPadding()
	checklist := []string{bold.Render("PIPELINE CHECKLIST")}
	if roomy {
		checklist = append(checklist, "")
	}
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
		if (m.cancelled || m.err != nil) && i == m.currentStep {
			status = lipgloss.NewStyle().Foreground(errorColor).Render("[✗]")
		} else if i < m.currentStep {
			status = lipgloss.NewStyle().Foreground(successColor).Render("[✔]")
		} else if i == m.currentStep {
			status = lipgloss.NewStyle().Foreground(warningColor).Render("[❯]")
		}
		checklist = append(checklist, truncateText(status+" "+s, rightInner))
	}
	rightStr := panelStyle.Width(rightWidth).Render(lipgloss.JoinVertical(lipgloss.Left, checklist...))

	split := lipgloss.JoinHorizontal(lipgloss.Top, leftStr, "  ", rightStr)

	// Bottom: latest logs, or the failure. Lines are truncated so the panel
	// height doesn't change with long ffmpeg output.
	logInner := m.panelWidth - panelStyle.GetHorizontalPadding()
	var body []string
	logTitle := bold.Render("CONVERSION LOGS (Press 'l' to expand)")
	if m.err != nil {
		logTitle = bold.Foreground(errorColor).Render("CONVERSION FAILED")
		for _, l := range strings.Split(m.err.Error(), "\n") {
			body = append(body, lipgloss.NewStyle().Foreground(errorColor).Render(l))
		}
		body = append(body, helpStyle().Render("If ffmpeg failed, its output is in fail-<time>.log in the book folder. Press 'l' for logs."))
	} else {
		body = m.logs[max(0, len(m.logs)-logRows):]
	}
	if len(body) > logRows {
		body = body[len(body)-logRows:]
	}
	logLines := []string{logTitle}
	if roomy {
		logLines = append(logLines, "")
	}
	for _, l := range body {
		logLines = append(logLines, truncateText(l, logInner))
	}
	for len(logLines) < logRows+1+titleGap { // pad so the help bar stays put
		logLines = append(logLines, "")
	}
	logsStr := panelStyle.Width(m.panelWidth).Render(lipgloss.JoinVertical(lipgloss.Left, logLines...))

	parts := []string{progressStr}
	for _, block := range []string{split, logsStr, m.help.View(progHelpKeys)} {
		if roomy {
			parts = append(parts, "")
		}
		parts = append(parts, block)
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}
