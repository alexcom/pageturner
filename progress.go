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
	Back       key.Binding
}{
	ToggleLogs: key.NewBinding(
		key.WithKeys("l", "L"),
		key.WithHelp("l", "full logs"),
	),
	// Only after the run ends; Enter is the natural "continue" on the finish screen.
	Back: key.NewBinding(
		key.WithKeys("esc", "enter"),
		key.WithHelp("esc", "back"),
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

// updatesBuffer lets ffmpeg workers report without waiting for each render.
const updatesBuffer = 256

type ProgressModel struct {
	ctx    context.Context
	config ConversionConfig

	totalFiles     int
	convertedFiles int

	workers []string

	currentStep int

	logs         []string
	wrapped      []string // logs wrapped to wrapWidth, one entry per log line
	wrapWidth    int
	showFullLogs bool
	cancelled    bool
	err          error // set when the pipeline failed
	outputs      []outputInfo

	updates chan tea.Msg

	progress progress.Model
	viewport viewport.Model
	help     help.Model

	panelWidth int
	width      int
	height     int
}

// updateThemeStyles re-themes the screen. The progress bar's gradient can only
// be set at construction, so the bar is rebuilt and re-animated to its value.
func (m *ProgressModel) updateThemeStyles() tea.Cmd {
	applyHelpStyles(&m.help.Styles)
	bar := newProgressBar()
	bar.Width = m.progress.Width
	pct := m.progress.Percent()
	m.progress = bar
	return m.progress.SetPercent(pct)
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

	prog := newProgressBar()
	prog.Width = 48

	m := &ProgressModel{
		ctx:        ctx,
		config:     config,
		totalFiles: len(files),
		workers:    []string{},
		logs:       []string{"Starting conversion..."},
		updates:    make(chan tea.Msg, updatesBuffer),
		progress:   prog,
		viewport:   viewport.New(80, 20),
		help:       newHelpModel(),
		panelWidth: 80,
	}
	m.refreshLogView()
	return m
}

func (m *ProgressModel) succeeded() bool {
	return m.err == nil && !m.cancelled && m.currentStep >= numPipelineSteps
}

// Finished reports whether the run has ended (done, failed or cancelled).
func (m *ProgressModel) Finished() bool {
	return m.err != nil || m.cancelled || m.currentStep >= numPipelineSteps
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

// Init starts the pipeline as a command (Bubble Tea runs each command in its
// own goroutine); the pipeline closes the updates channel when it returns.
func (m *ProgressModel) Init() tea.Cmd {
	ctx, config, updates := m.ctx, m.config, m.updates
	run := func() tea.Msg {
		runConversionPipeline(ctx, config, updates)
		return nil
	}
	return tea.Batch(run, waitForUpdate(m.updates))
}

func (m *ProgressModel) SetSize(w, h int) {
	m.width, m.height = w, h
	m.panelWidth = max(40, w-2) // panel border is 2 columns
	frameW, frameH := logBoxStyle().GetFrameSize()
	m.viewport.Width = max(1, w-frameW)
	m.viewport.Height = max(1, h-frameH-2) // blank line + help
	m.help.Width = w
	m.refreshLogView()
}

// refreshLogView wraps the logs to the viewport width (the viewport cuts long
// lines and can't scroll sideways) and keeps following the end if it was there.
// Wrapped lines are cached; everything is rewrapped only when the width changes.
func (m *ProgressModel) refreshLogView() {
	if m.wrapWidth != m.viewport.Width {
		m.wrapWidth = m.viewport.Width
		m.wrapped = m.wrapped[:0]
	}
	for i := len(m.wrapped); i < len(m.logs); i++ {
		m.wrapped = append(m.wrapped, wrapText(m.logs[i], m.wrapWidth))
	}
	follow := m.viewport.AtBottom() // check before SetContent: more content means not at bottom
	m.viewport.SetContent(strings.Join(m.wrapped, "\n"))
	if follow {
		m.viewport.GotoBottom()
	}
}

func (m *ProgressModel) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if key.Matches(msg, progKeys.ToggleLogs) {
			m.showFullLogs = !m.showFullLogs
			if m.showFullLogs {
				m.viewport.GotoBottom()
			}
			return nil
		}
		if m.showFullLogs {
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return cmd
		}
		return nil

	case progress.FrameMsg:
		progressModel, cmd := m.progress.Update(msg)
		m.progress = progressModel.(progress.Model)
		return cmd

	case msgLog:
		m.logs = append(m.logs, msg.text)
		if drop := len(m.logs) - 1000; drop > 0 {
			m.logs = m.logs[drop:]
			m.wrapped = m.wrapped[min(drop, len(m.wrapped)):]
		}
		m.refreshLogView()
		return waitForUpdate(m.updates)

	case msgWorkerUpdate:
		for len(m.workers) <= msg.workerID {
			m.workers = append(m.workers, "Idle")
		}
		m.workers[msg.workerID] = msg.status
		return waitForUpdate(m.updates)

	case msgProgress:
		// Workers report concurrently, so counts may arrive out of order.
		m.convertedFiles = max(m.convertedFiles, msg.completed)
		m.totalFiles = msg.total
		pct := overallProgress(m.currentStep, m.convertedFiles, m.totalFiles)
		return tea.Batch(waitForUpdate(m.updates), m.progress.SetPercent(pct))

	case msgStepAdvance:
		m.currentStep = msg.step
		pct := overallProgress(m.currentStep, m.convertedFiles, m.totalFiles)
		return tea.Batch(waitForUpdate(m.updates), m.progress.SetPercent(pct))

	case msgInitWorkers:
		m.workers = make([]string, msg.count)
		for i := 0; i < msg.count; i++ {
			m.workers[i] = "Starting..."
		}
		return waitForUpdate(m.updates)

	case msgError:
		m.err = msg.err
		return waitForUpdate(m.updates)
	case msgConversionDone:
		m.currentStep = numPipelineSteps
		m.outputs = msg.outputs
		return waitForUpdate(m.updates)
	case msgCancelled:
		m.cancelled = true
		return waitForUpdate(m.updates)
	case msgUpdatesClosed:
		return nil
	}
	return nil
}

// ShortHelp implements help.KeyMap with the keys that work right now.
func (m *ProgressModel) ShortHelp() []key.Binding {
	logs := progKeys.ToggleLogs
	var b []key.Binding
	if m.showFullLogs {
		logs.SetHelp("l", "summary")
		scroll := m.viewport.KeyMap.Up
		scroll.SetHelp("↑/↓/pgup/pgdn", "scroll")
		b = append(b, scroll)
	}
	back := progKeys.Back
	back.SetEnabled(m.Finished())
	if m.succeeded() {
		back.SetHelp("enter", "convert another")
	}
	b = append(b, logs, back)
	return append(b, globalHelp(false)...)
}

func (m *ProgressModel) FullHelp() [][]key.Binding { return [][]key.Binding{m.ShortHelp()} }

func (m *ProgressModel) View() string {
	if m.showFullLogs {
		return lipgloss.JoinVertical(lipgloss.Left, logBoxStyle().Render(m.viewport.View()), "", m.help.View(m))
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
	} else if m.succeeded() && len(m.outputs) > 0 {
		logTitle = bold.Foreground(successColor).Render("DONE")
		body = m.outputLines(logRows, logInner)
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
	for _, block := range []string{split, logsStr, m.help.View(m)} {
		if roomy {
			parts = append(parts, "")
		}
		parts = append(parts, block)
	}
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// outputLines lists the written files for the finish screen, in at most rows lines.
func (m *ProgressModel) outputLines(rows, width int) []string {
	ok := lipgloss.NewStyle().Foreground(successColor).Render("✔ ")
	dim := lipgloss.NewStyle().Foreground(subtextColor)
	if len(m.outputs) == 1 {
		o := m.outputs[0]
		lines := []string{ok + truncateLeft(o.path, width-2), "  " + dim.Render(o.summary())}
		return lines[:min(len(lines), rows)]
	}
	var total int64
	for _, o := range m.outputs {
		total += o.size
	}
	lines := []string{fmt.Sprintf("%d files written · %s", len(m.outputs), humanSize(total))}
	for i, o := range m.outputs {
		if len(lines) == rows-1 && i < len(m.outputs)-1 {
			lines = append(lines, dim.Render(fmt.Sprintf("  +%d more (press 'l' for logs)", len(m.outputs)-i)))
			break
		}
		note := "  " + o.summary()
		name := truncateLeft(o.path, max(10, width-2-lipgloss.Width(note)))
		lines = append(lines, ok+name+dim.Render(note))
	}
	return lines[:min(len(lines), rows)]
}
