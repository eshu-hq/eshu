package main

import (
	"io"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/eshu-hq/eshu/go/internal/query"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

func TestLocalHostProgressTUIModelRendersKnownWorkTable(t *testing.T) {
	t.Parallel()

	model := localHostProgressTUIModel{
		workspaceRoot: "/workspace/repo",
		runtimeConfig: localHostRuntimeConfig{
			Profile:      query.ProfileLocalAuthoritative,
			GraphBackend: query.GraphBackendNornicDB,
		},
	}
	updated, cmd := model.Update(localHostProgressReportMsg{report: statuspkg.Report{
		AsOf: time.Date(2026, time.May, 7, 15, 0, 0, 0, time.UTC),
		Health: statuspkg.HealthSummary{
			State: "progressing",
		},
		GenerationHistory: statuspkg.GenerationHistorySnapshot{
			Active:    1,
			Pending:   2,
			Completed: 3,
		},
		StageSummaries: []statuspkg.StageSummary{
			{Stage: "projector", Pending: 1, Running: 1, Succeeded: 2},
		},
	}})
	if cmd == nil {
		t.Fatal("Update() cmd = nil, want animated progress command")
	}

	tuiModel, ok := updated.(localHostProgressTUIModel)
	if !ok {
		t.Fatalf("Update() model = %T, want localHostProgressTUIModel", updated)
	}
	view := tuiModel.View()
	for _, want := range []string{
		"Eshu local graph",
		"Indexing",
		"Stage      State       Progress",
		"Done   Active  Waiting  Failed  Unit",
		"Collector",
		"Projector",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() missing %q in:\n%s", want, view)
		}
	}
}

func TestLocalHostProgressTUIModelAlignsKnownWorkColumnsWithStyledBars(t *testing.T) {
	t.Parallel()

	model := localHostProgressTUIModel{
		runtimeConfig: localHostRuntimeConfig{
			Profile:      query.ProfileLocalAuthoritative,
			GraphBackend: query.GraphBackendNornicDB,
		},
	}
	updated, _ := model.Update(localHostProgressReportMsg{report: statuspkg.Report{
		AsOf:   time.Date(2026, time.May, 7, 17, 7, 26, 0, time.UTC),
		Health: statuspkg.HealthSummary{State: "healthy"},
		GenerationHistory: statuspkg.GenerationHistorySnapshot{
			Active: 1,
		},
		StageSummaries: []statuspkg.StageSummary{
			{Stage: "projector", Succeeded: 1},
			{Stage: "reducer", Succeeded: 8},
		},
	}})
	tuiModel, ok := updated.(localHostProgressTUIModel)
	if !ok {
		t.Fatalf("Update() model = %T, want localHostProgressTUIModel", updated)
	}
	view := tuiModel.View()
	headerLine := localHostProgressLineWithPrefix(view, "Stage")
	collectorLine := localHostProgressLineWithPrefix(view, "Collector")
	if got, want := visibleColumnBefore(collectorLine, "1/1"), visibleColumnBefore(headerLine, "Done"); got != want {
		t.Fatalf("collector Done column = %d, want header column %d in:\n%s", got, want, view)
	}

	runningRow := model.renderKnownWorkRow(localHostKnownWorkRow{
		stage:     "Collector",
		done:      0,
		active:    1,
		total:     1,
		workLabel: "generations",
	})
	if got, want := visibleColumnBefore(runningRow, "0/1"), 56; got != want {
		t.Fatalf("running row Done column = %d, want %d in %q", got, want, runningRow)
	}

	idleRow := model.renderKnownWorkRow(localHostKnownWorkRow{
		stage:     "Collector",
		workLabel: "generations",
	})
	if got, want := visibleColumnBefore(idleRow, "-"), 56; got != want {
		t.Fatalf("idle row Done column = %d, want %d in %q", got, want, idleRow)
	}
}

func TestLocalHostProgressTUIModelShowsCompleteVerdict(t *testing.T) {
	t.Parallel()

	model := localHostProgressTUIModel{
		runtimeConfig: localHostRuntimeConfig{
			Profile:      query.ProfileLocalAuthoritative,
			GraphBackend: query.GraphBackendNornicDB,
		},
	}
	updated, _ := model.Update(localHostProgressReportMsg{report: statuspkg.Report{
		AsOf:   time.Date(2026, time.May, 7, 15, 0, 0, 0, time.UTC),
		Health: statuspkg.HealthSummary{State: "healthy"},
		GenerationHistory: statuspkg.GenerationHistorySnapshot{
			Completed: 1,
		},
		StageSummaries: []statuspkg.StageSummary{
			{Stage: "projector", Succeeded: 1},
			{Stage: "reducer", Succeeded: 8},
		},
	}})
	tuiModel, ok := updated.(localHostProgressTUIModel)
	if !ok {
		t.Fatalf("Update() model = %T, want localHostProgressTUIModel", updated)
	}
	view := tuiModel.View()
	if !strings.Contains(view, "Complete") {
		t.Fatalf("View() = %q, want Complete verdict", view)
	}
	if !strings.Contains(view, "all known work drained") {
		t.Fatalf("View() = %q, want completion explanation", view)
	}
}

func TestLocalHostProgressTUIModelShowsSharedProjectionBacklog(t *testing.T) {
	t.Parallel()

	model := localHostProgressTUIModel{
		runtimeConfig: localHostRuntimeConfig{
			Profile:      query.ProfileLocalAuthoritative,
			GraphBackend: query.GraphBackendNornicDB,
		},
	}
	updated, _ := model.Update(localHostProgressReportMsg{report: statuspkg.Report{
		AsOf:   time.Date(2026, time.May, 9, 12, 23, 53, 0, time.UTC),
		Health: statuspkg.HealthSummary{State: "progressing"},
		GenerationHistory: statuspkg.GenerationHistorySnapshot{
			Active: 1,
		},
		StageSummaries: []statuspkg.StageSummary{
			{Stage: "projector", Succeeded: 1},
			{Stage: "reducer", Succeeded: 8},
		},
		DomainBacklogs: []statuspkg.DomainBacklog{
			{
				Domain:      "code_calls",
				Outstanding: 622561,
				InFlight:    1,
				OldestAge:   10*time.Minute + 22*time.Second,
			},
		},
	}})
	tuiModel, ok := updated.(localHostProgressTUIModel)
	if !ok {
		t.Fatalf("Update() model = %T, want localHostProgressTUIModel", updated)
	}

	view := tuiModel.View()
	for _, want := range []string{
		"Settling",
		"shared projection work is becoming graph-visible",
		"Shared projections code_calls outstanding=622561 in_flight=1 retrying=0 dead_letter=0 failed=0 oldest=10m22s",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("View() missing %q in:\n%s", want, view)
		}
	}
}

func TestLocalHostProgressTUIModelKeepsCollectorPendingAsIndexing(t *testing.T) {
	t.Parallel()

	model := localHostProgressTUIModel{
		runtimeConfig: localHostRuntimeConfig{
			Profile:      query.ProfileLocalAuthoritative,
			GraphBackend: query.GraphBackendNornicDB,
		},
	}
	updated, _ := model.Update(localHostProgressReportMsg{report: statuspkg.Report{
		AsOf:   time.Date(2026, time.May, 7, 15, 0, 0, 0, time.UTC),
		Health: statuspkg.HealthSummary{State: "healthy"},
		GenerationHistory: statuspkg.GenerationHistorySnapshot{
			Pending: 1,
		},
		StageSummaries: []statuspkg.StageSummary{
			{Stage: "projector", Succeeded: 1},
			{Stage: "reducer", Succeeded: 8},
		},
	}})
	tuiModel, ok := updated.(localHostProgressTUIModel)
	if !ok {
		t.Fatalf("Update() model = %T, want localHostProgressTUIModel", updated)
	}
	view := tuiModel.View()
	if !strings.Contains(view, "Indexing") {
		t.Fatalf("View() = %q, want Indexing while collector has pending generation", view)
	}
	if strings.Contains(view, "Complete") {
		t.Fatalf("View() = %q, must not show Complete before collector finishes", view)
	}
}

func TestLocalHostProgressTUIModelTreatsActiveCollectorGenerationAsCurrent(t *testing.T) {
	t.Parallel()

	model := localHostProgressTUIModel{
		runtimeConfig: localHostRuntimeConfig{
			Profile:      query.ProfileLocalAuthoritative,
			GraphBackend: query.GraphBackendNornicDB,
		},
	}
	updated, _ := model.Update(localHostProgressReportMsg{report: statuspkg.Report{
		AsOf:   time.Date(2026, time.May, 7, 17, 24, 43, 0, time.UTC),
		Health: statuspkg.HealthSummary{State: "healthy"},
		GenerationHistory: statuspkg.GenerationHistorySnapshot{
			Active: 1,
		},
		StageSummaries: []statuspkg.StageSummary{
			{Stage: "projector", Succeeded: 1},
			{Stage: "reducer", Succeeded: 8},
		},
	}})
	tuiModel, ok := updated.(localHostProgressTUIModel)
	if !ok {
		t.Fatalf("Update() model = %T, want localHostProgressTUIModel", updated)
	}
	view := tuiModel.View()
	if !strings.Contains(view, "Complete") {
		t.Fatalf("View() = %q, want Complete when current generation and downstream work drained", view)
	}
	if !strings.Contains(view, "Collector  complete") {
		t.Fatalf("View() = %q, want Collector complete for active current generation", view)
	}
	if !strings.Contains(view, "1/1    0       0") {
		t.Fatalf("View() = %q, want active current generation counted as done, not worker-active", view)
	}
}

func TestLocalHostProgressTUIModelInitializesBrandedAnimatedBars(t *testing.T) {
	t.Parallel()

	model := localHostProgressTUIModel{}
	updated, _ := model.Update(localHostProgressReportMsg{report: statuspkg.Report{
		GenerationHistory: statuspkg.GenerationHistorySnapshot{
			Completed: 1,
			Pending:   1,
		},
	}})
	tuiModel, ok := updated.(localHostProgressTUIModel)
	if !ok {
		t.Fatalf("Update() model = %T, want localHostProgressTUIModel", updated)
	}
	bar, ok := tuiModel.bars["Collector"]
	if !ok {
		t.Fatalf("bars missing Collector: %#v", tuiModel.bars)
	}
	if got := bar.EmptyColor; got != eshuColorDeepTeal {
		t.Fatalf("Collector bar EmptyColor = %q, want %q", got, eshuColorDeepTeal)
	}
	if got := bar.Percent(); got != 0.5 {
		t.Fatalf("Collector bar Percent() = %v, want 0.5", got)
	}
}

func TestNewLocalHostProgressRendererSelectsPlainForPlainMode(t *testing.T) {
	t.Parallel()

	renderer := newLocalHostProgressRenderer(
		"/workspace/repo",
		localHostRuntimeConfig{},
		localHostProgressModePlain,
		nil,
		func(io.Writer) bool { return true },
	)
	if _, ok := renderer.(localHostPlainProgressRenderer); !ok {
		t.Fatalf("renderer = %T, want localHostPlainProgressRenderer", renderer)
	}
}

var _ tea.Model = localHostProgressTUIModel{}

func visibleColumnBefore(line string, marker string) int {
	index := strings.Index(line, marker)
	if index < 0 {
		return -1
	}
	return lipgloss.Width(line[:index])
}

func localHostProgressLineWithPrefix(view string, prefix string) string {
	for _, line := range strings.Split(view, "\n") {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return ""
}
