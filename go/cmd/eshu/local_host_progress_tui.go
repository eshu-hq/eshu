package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

const (
	eshuColorGraphite   = "#0B0F14"
	eshuColorBone       = "#F3EBDD"
	eshuColorSignalTeal = "#14B8A6"
	eshuColorDeepTeal   = "#0F766E"
	eshuColorEmber      = "#FF8A00"
	eshuColorCoral      = "#FF5A4F"
)

var (
	localHostProgressTitleStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color(eshuColorBone)).
					Bold(true)
	localHostProgressMutedStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color(eshuColorDeepTeal))
	localHostProgressHeaderStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color(eshuColorEmber)).
					Bold(true)
	localHostProgressHealthyStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color(eshuColorSignalTeal)).
					Bold(true)
	localHostProgressWarningStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color(eshuColorCoral)).
					Bold(true)
)

type localHostProgressReportMsg struct {
	report statuspkg.Report
}

type localHostProgressTUIModel struct {
	workspaceRoot string
	runtimeConfig localHostRuntimeConfig
	report        statuspkg.Report
	ready         bool
	bars          map[string]progress.Model
}

type localHostProgressVerdict struct {
	label  string
	detail string
	style  lipgloss.Style
}

func (m localHostProgressTUIModel) Init() tea.Cmd {
	return nil
}

func (m localHostProgressTUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case localHostProgressReportMsg:
		m.report = msg.report
		m.ready = true
		return m, m.animateKnownWorkRows(localHostKnownWorkRows(msg.report))
	case progress.FrameMsg:
		return m.updateProgressBars(msg)
	default:
		return m, nil
	}
}

func (m localHostProgressTUIModel) View() string {
	if !m.ready {
		return localHostProgressTitleStyle.Render("Eshu local graph starting...") + "\n"
	}

	verdict := localHostProgressVerdictForReport(m.report)
	var builder strings.Builder
	fmt.Fprintf(
		&builder,
		"%s %s  %s\n",
		localHostProgressTitleStyle.Render("Eshu local graph"),
		localHostProgressMutedStyle.Render(m.report.AsOf.Format("15:04:05")),
		verdict.style.Render(verdict.label),
	)
	fmt.Fprintf(
		&builder,
		"Owner %s  Backend %s\n",
		localHostProgressMutedStyle.Render(string(m.runtimeConfig.Profile)),
		localHostProgressMutedStyle.Render(localHostProgressBackendLabel(m.runtimeConfig)),
	)
	fmt.Fprintf(&builder, "%s\n", localHostProgressMutedStyle.Render(verdict.detail))
	fmt.Fprintf(&builder, "Health %s\n", localHostProgressHealthStyle(m.report.Health.State).Render(m.report.Health.State))
	builder.WriteString(localHostProgressHeaderStyle.Render(localHostProgressTUITableLine(
		"Stage",
		"State",
		"Progress",
		"Done",
		"Active",
		"Waiting",
		"Failed",
		"Unit",
	)))
	builder.WriteString("\n")
	for _, row := range localHostKnownWorkRows(m.report) {
		builder.WriteString(m.renderKnownWorkRow(row))
		builder.WriteString("\n")
	}
	if m.report.GenerationHistory.Superseded > 0 {
		fmt.Fprintf(
			&builder,
			"%s %d\n",
			localHostProgressMutedStyle.Render("Superseded generations"),
			m.report.GenerationHistory.Superseded,
		)
	}
	fmt.Fprintf(
		&builder,
		"Queue pending=%d in_flight=%d retrying=%d dead_letter=%d failed=%d oldest=%s\n",
		m.report.Queue.Pending,
		m.report.Queue.InFlight,
		m.report.Queue.Retrying,
		m.report.Queue.DeadLetter,
		m.report.Queue.Failed,
		localHostProgressAge(m.report.Queue.OldestOutstandingAge),
	)
	if latestFailure := localHostProgressFailureText(m.report.LatestQueueFailure); latestFailure != "" {
		fmt.Fprintf(&builder, "%s %s\n", localHostProgressWarningStyle.Render("Latest failure"), latestFailure)
	}
	return strings.TrimRight(builder.String(), "\n")
}

func (m *localHostProgressTUIModel) animateKnownWorkRows(rows []localHostKnownWorkRow) tea.Cmd {
	if m.bars == nil {
		m.bars = make(map[string]progress.Model, len(rows))
	}
	cmds := make([]tea.Cmd, 0, len(rows))
	for _, row := range rows {
		bar := m.progressBar(row.stage)
		cmds = append(cmds, bar.SetPercent(localHostProgressFraction(row.done, row.total)))
		m.bars[row.stage] = bar
	}
	return tea.Batch(cmds...)
}

func (m *localHostProgressTUIModel) updateProgressBars(msg progress.FrameMsg) (tea.Model, tea.Cmd) {
	cmds := make([]tea.Cmd, 0, len(m.bars))
	for stage, bar := range m.bars {
		updated, cmd := bar.Update(msg)
		if progressBar, ok := updated.(progress.Model); ok {
			m.bars[stage] = progressBar
		}
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return *m, tea.Batch(cmds...)
}

func (m *localHostProgressTUIModel) renderKnownWorkRow(row localHostKnownWorkRow) string {
	if row.total <= 0 {
		return localHostProgressTUITableLine(
			row.stage,
			localHostProgressRowState(row),
			localHostProgressMutedStyle.Render("idle"),
			"-",
			fmt.Sprint(row.active),
			fmt.Sprint(row.waiting),
			fmt.Sprint(row.failed),
			localHostIdleWorkLabel(row),
		)
	}
	return localHostProgressTUITableLine(
		row.stage,
		localHostProgressRowState(row),
		m.progressBar(row.stage).View(),
		localHostProgressDoneText(row.done, row.total),
		fmt.Sprint(row.active),
		fmt.Sprint(row.waiting),
		fmt.Sprint(row.failed),
		row.workLabel,
	)
}

func localHostProgressTUITableLine(
	stage string,
	state string,
	progressText string,
	done string,
	active string,
	waiting string,
	failed string,
	unit string,
) string {
	columns := []string{
		localHostProgressPadRight(stage, 10),
		localHostProgressPadRight(state, 11),
		localHostProgressPadRight(progressText, 32),
		localHostProgressPadRight(done, 6),
		localHostProgressPadRight(active, 7),
		localHostProgressPadRight(waiting, 8),
		localHostProgressPadRight(failed, 7),
		unit,
	}
	return strings.Join(columns, " ")
}

func localHostProgressPadRight(value string, width int) string {
	padding := width - lipgloss.Width(value)
	if padding <= 0 {
		return value
	}
	return value + strings.Repeat(" ", padding)
}

func (m *localHostProgressTUIModel) progressBar(stage string) progress.Model {
	if m.bars != nil {
		if bar, ok := m.bars[stage]; ok {
			return bar
		}
	}
	bar := progress.New(
		progress.WithWidth(28),
		progress.WithScaledGradient(eshuColorEmber, eshuColorSignalTeal),
		progress.WithoutPercentage(),
		progress.WithSpringOptions(18, 0.85),
	)
	bar.EmptyColor = eshuColorDeepTeal
	return bar
}

func localHostProgressHealthStyle(state string) lipgloss.Style {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "healthy", "progressing":
		return localHostProgressHealthyStyle
	case "degraded", "stalled":
		return localHostProgressWarningStyle
	default:
		return localHostProgressMutedStyle
	}
}

func localHostProgressVerdictForReport(report statuspkg.Report) localHostProgressVerdict {
	rows := localHostKnownWorkRows(report)
	if localHostProgressHasFailures(report, rows) {
		return localHostProgressVerdict{
			label:  "Attention",
			detail: "failures or dead-letter work need operator action",
			style:  localHostProgressWarningStyle,
		}
	}
	if localHostProgressHasCollectorWorkInProgress(report.GenerationHistory) {
		return localHostProgressVerdict{
			label:  "Indexing",
			detail: "collector generation is still being discovered or committed",
			style:  localHostProgressHealthyStyle,
		}
	}
	if localHostProgressHasActiveWork(rows) {
		return localHostProgressVerdict{
			label:  "Indexing",
			detail: "collector, projector, or reducer work is active",
			style:  localHostProgressHealthyStyle,
		}
	}
	if localHostProgressHasWaitingWork(report, rows) {
		return localHostProgressVerdict{
			label:  "Settling",
			detail: "known work remains queued or waiting for a worker",
			style:  localHostProgressHeaderStyle,
		}
	}
	if localHostProgressHasCompletedWork(rows) {
		return localHostProgressVerdict{
			label:  "Complete",
			detail: "all known work drained; owner is watching for changes",
			style:  localHostProgressHealthyStyle,
		}
	}
	return localHostProgressVerdict{
		label:  "Watching",
		detail: "no known work yet; owner is watching the workspace",
		style:  localHostProgressMutedStyle,
	}
}

func localHostProgressHasCollectorWorkInProgress(history statuspkg.GenerationHistorySnapshot) bool {
	return history.Pending > 0
}

func localHostProgressHasFailures(report statuspkg.Report, rows []localHostKnownWorkRow) bool {
	if report.LatestQueueFailure != nil || report.Queue.DeadLetter > 0 || report.Queue.Failed > 0 {
		return true
	}
	for _, row := range rows {
		if row.failed > 0 {
			return true
		}
	}
	return false
}

func localHostProgressHasActiveWork(rows []localHostKnownWorkRow) bool {
	for _, row := range rows {
		if row.active > 0 {
			return true
		}
	}
	return false
}

func localHostProgressHasWaitingWork(report statuspkg.Report, rows []localHostKnownWorkRow) bool {
	if report.Queue.Pending > 0 || report.Queue.InFlight > 0 || report.Queue.Retrying > 0 {
		return true
	}
	for _, row := range rows {
		if row.waiting > 0 {
			return true
		}
	}
	return false
}

func localHostProgressHasCompletedWork(rows []localHostKnownWorkRow) bool {
	for _, row := range rows {
		if row.done > 0 {
			return true
		}
	}
	return false
}
