package status_test

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/status"
)

func TestBuildReportIncludesGenerationTransitions(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
			GenerationTransitions: []status.GenerationTransitionSnapshot{
				{
					ScopeID:                   "scope-1",
					GenerationID:              "generation-a",
					Status:                    "superseded",
					TriggerKind:               "snapshot",
					FreshnessHint:             "changed files",
					ObservedAt:                time.Date(2026, 4, 12, 15, 30, 0, 0, time.UTC),
					ActivatedAt:               time.Date(2026, 4, 12, 15, 31, 0, 0, time.UTC),
					SupersededAt:              time.Date(2026, 4, 12, 15, 40, 0, 0, time.UTC),
					CurrentActiveGenerationID: "generation-b",
				},
			},
		},
		status.DefaultOptions(),
	)

	if got, want := len(report.GenerationTransitions), 1; got != want {
		t.Fatalf("BuildReport().GenerationTransitions len = %d, want %d", got, want)
	}
	if got, want := report.GenerationTransitions[0].CurrentActiveGenerationID, "generation-b"; got != want {
		t.Fatalf("BuildReport().GenerationTransitions[0].CurrentActiveGenerationID = %q, want %q", got, want)
	}
}

func TestRenderTextIncludesGenerationTransitions(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
			GenerationTransitions: []status.GenerationTransitionSnapshot{
				{
					ScopeID:                   "scope-1",
					GenerationID:              "generation-b",
					Status:                    "active",
					TriggerKind:               "snapshot",
					ObservedAt:                time.Date(2026, 4, 12, 15, 45, 0, 0, time.UTC),
					ActivatedAt:               time.Date(2026, 4, 12, 15, 46, 0, 0, time.UTC),
					FreshnessHint:             "fresh snapshot",
					CurrentActiveGenerationID: "generation-b",
				},
			},
		},
		status.DefaultOptions(),
	)

	rendered := status.RenderText(report)
	for _, want := range []string{
		"Generation transitions:",
		"scope=scope-1 generation=generation-b status=active",
		"current_active=generation-b",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("RenderText() missing %q in output:\n%s", want, rendered)
		}
	}
}

func TestRenderJSONIncludesGenerationTransitions(t *testing.T) {
	t.Parallel()

	report := status.BuildReport(
		status.RawSnapshot{
			AsOf: time.Date(2026, 4, 12, 16, 0, 0, 0, time.UTC),
			GenerationTransitions: []status.GenerationTransitionSnapshot{
				{
					ScopeID:                   "scope-1",
					GenerationID:              "generation-a",
					Status:                    "superseded",
					TriggerKind:               "snapshot",
					ObservedAt:                time.Date(2026, 4, 12, 15, 30, 0, 0, time.UTC),
					SupersededAt:              time.Date(2026, 4, 12, 15, 40, 0, 0, time.UTC),
					FreshnessHint:             "changed files",
					CurrentActiveGenerationID: "generation-b",
				},
			},
		},
		status.DefaultOptions(),
	)

	payload, err := status.RenderJSON(report)
	if err != nil {
		t.Fatalf("RenderJSON() error = %v, want nil", err)
	}
	for _, want := range []string{
		`"generation_transitions"`,
		`"scope_id": "scope-1"`,
		`"generation_id": "generation-a"`,
		`"current_active_generation_id"`,
	} {
		if !strings.Contains(string(payload), want) {
			t.Fatalf("RenderJSON() missing %q in payload:\n%s", want, payload)
		}
	}
}
