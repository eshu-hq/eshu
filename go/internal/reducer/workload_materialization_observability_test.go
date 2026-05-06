package reducer

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestWorkloadMaterializationHandlerLogsStageTiming(t *testing.T) {
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(previous)

	loader := &stubFactLoader{envelopes: []facts.Envelope{
		{
			FactID:   "fact-repo",
			FactKind: "repository",
			Payload: map[string]any{
				"graph_id": "repo-payments",
				"name":     "payments",
			},
			ObservedAt: time.Date(2026, time.April, 28, 12, 0, 0, 0, time.UTC),
		},
		{
			FactID:   "fact-k8s",
			FactKind: "file",
			Payload: map[string]any{
				"repo_id": "repo-payments",
				"parsed_file_data": map[string]any{
					"k8s_resources": []any{
						map[string]any{
							"name":      "payments",
							"kind":      "Deployment",
							"namespace": "production",
						},
					},
				},
			},
			ObservedAt: time.Date(2026, time.April, 28, 12, 0, 0, 0, time.UTC),
		},
	}}
	handler := WorkloadMaterializationHandler{
		FactLoader:   loader,
		Materializer: NewWorkloadMaterializer(&recordingCypherExecutor{}),
	}

	_, err := handler.Handle(context.Background(), Intent{
		IntentID:        "intent-wm-observe",
		ScopeID:         "scope-payments",
		GenerationID:    "gen-1",
		SourceSystem:    "git",
		Domain:          DomainWorkloadMaterialization,
		Cause:           "test",
		EntityKeys:      []string{"repo-payments"},
		RelatedScopeIDs: []string{"scope-payments"},
		EnqueuedAt:      time.Date(2026, time.April, 28, 12, 0, 0, 0, time.UTC),
		AvailableAt:     time.Date(2026, time.April, 28, 12, 0, 0, 0, time.UTC),
		Status:          IntentStatusPending,
	})
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}

	logText := logs.String()
	for _, want := range []string{
		`"msg":"workload materialization completed"`,
		`"candidate_count":1`,
		`"workload_row_count":1`,
		`"instance_row_count":1`,
		`"deployment_source_row_count":0`,
		`"runtime_platform_row_count":1`,
		`"load_inputs_duration_seconds":`,
		`"build_projection_duration_seconds":`,
		`"graph_write_duration_seconds":`,
		`"workload_graph_write_duration_seconds":`,
		`"instance_graph_write_duration_seconds":`,
		`"deployment_source_graph_write_duration_seconds":`,
		`"runtime_platform_graph_write_duration_seconds":`,
		`"dependency_reconcile_duration_seconds":`,
		`"dependency_retract_duration_seconds":`,
		`"dependency_write_duration_seconds":`,
		`"phase_publish_duration_seconds":`,
		`"total_duration_seconds":`,
	} {
		if !strings.Contains(logText, want) {
			t.Fatalf("logs missing %s:\n%s", want, logText)
		}
	}
}
