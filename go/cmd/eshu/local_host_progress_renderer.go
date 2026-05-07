package main

import (
	"io"
	"os"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mattn/go-isatty"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

type localHostProgressRenderer interface {
	Render(statuspkg.Report) error
	Close() error
}

type localHostPlainProgressRenderer struct {
	workspaceRoot string
	runtimeConfig localHostRuntimeConfig
	writer        io.Writer
}

func (r localHostPlainProgressRenderer) Render(report statuspkg.Report) error {
	_, err := io.WriteString(r.writer, renderLocalHostProgressSnapshot(r.workspaceRoot, r.runtimeConfig, report))
	return err
}

func (r localHostPlainProgressRenderer) Close() error {
	return nil
}

type localHostBubbleTeaProgressRenderer struct {
	program *tea.Program
	done    chan error
}

func newLocalHostProgressRenderer(
	workspaceRoot string,
	runtimeConfig localHostRuntimeConfig,
	mode string,
	writer io.Writer,
	isTerminal func(io.Writer) bool,
) localHostProgressRenderer {
	if writer == nil {
		writer = io.Discard
	}
	if mode == localHostProgressModeAuto && isTerminal != nil && isTerminal(writer) {
		program := tea.NewProgram(
			localHostProgressTUIModel{
				workspaceRoot: workspaceRoot,
				runtimeConfig: runtimeConfig,
			},
			tea.WithAltScreen(),
			tea.WithInput(nil),
			tea.WithOutput(writer),
			tea.WithoutSignalHandler(),
		)
		renderer := &localHostBubbleTeaProgressRenderer{
			program: program,
			done:    make(chan error, 1),
		}
		go func() {
			_, err := program.Run()
			renderer.done <- err
		}()
		return renderer
	}
	return localHostPlainProgressRenderer{
		workspaceRoot: workspaceRoot,
		runtimeConfig: runtimeConfig,
		writer:        writer,
	}
}

func (r *localHostBubbleTeaProgressRenderer) Render(report statuspkg.Report) error {
	r.program.Send(localHostProgressReportMsg{report: report})
	return nil
}

func (r *localHostBubbleTeaProgressRenderer) Close() error {
	r.program.Quit()
	return <-r.done
}

func localHostProgressWriterIsTerminal(writer io.Writer) bool {
	file, ok := writer.(*os.File)
	if !ok {
		return false
	}
	return isatty.IsTerminal(file.Fd())
}
